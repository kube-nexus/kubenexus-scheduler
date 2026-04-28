#!/usr/bin/env bash
# Copyright 2026 KubeNexus Authors.
# SPDX-License-Identifier: Apache-2.0
#
# End-to-end test infrastructure setup.
# Creates a Kind cluster with KWOK fake GPU nodes, DRA ResourceSlices,
# and deploys the KubeNexus scheduler with all plugins enabled.
#
# Usage:
#   ./hack/e2e-setup.sh          # Full setup (cluster + KWOK + scheduler)
#   ./hack/e2e-setup.sh teardown # Delete the Kind cluster
#
# Prerequisites:
#   - kind, kubectl, helm, docker
#   - Go toolchain (for cross-compilation)

set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-kubenexus-e2e}"
SCHEDULER_IMAGE="kubenexus-scheduler:e2e"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Detect architecture
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64)  GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  *) echo "Unsupported arch: ${ARCH}"; exit 1 ;;
esac

info() { echo "==> $*"; }
warn() { echo "WARNING: $*" >&2; }

teardown() {
  info "Deleting Kind cluster '${CLUSTER_NAME}'"
  kind delete cluster --name "${CLUSTER_NAME}" 2>/dev/null || true
}

if [[ "${1:-}" == "teardown" ]]; then
  teardown
  exit 0
fi

# ── Step 1: Kind cluster ──────────────────────────────────────────────────
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
  info "Kind cluster '${CLUSTER_NAME}' already exists, reusing"
  kind export kubeconfig --name "${CLUSTER_NAME}"
else
  info "Creating Kind cluster '${CLUSTER_NAME}'"
  kind create cluster --name "${CLUSTER_NAME}" \
    --config "${ROOT_DIR}/test/e2e/fixtures/kind-e2e.yaml" \
    --wait 120s
fi

kubectl cluster-info --context "kind-${CLUSTER_NAME}" >/dev/null 2>&1 || {
  echo "ERROR: Cannot reach cluster"; exit 1
}

# ── Step 2: KWOK ──────────────────────────────────────────────────────────
info "Installing KWOK"
helm repo add kwok https://kwok.sigs.k8s.io/charts 2>/dev/null || true
helm repo update kwok >/dev/null 2>&1

KWOK_VERSION="0.7.0"
KWOK_IMAGE="registry.k8s.io/kwok/kwok:v${KWOK_VERSION}"

# Pre-pull and load into Kind to avoid TLS issues inside Kind nodes
if ! docker image inspect "${KWOK_IMAGE}" >/dev/null 2>&1; then
  info "Pulling KWOK image"
  docker pull "${KWOK_IMAGE}"
fi
kind load docker-image "${KWOK_IMAGE}" --name "${CLUSTER_NAME}" 2>/dev/null || true

if helm status kwok -n kube-system >/dev/null 2>&1; then
  info "KWOK already installed, upgrading"
  helm upgrade kwok kwok/kwok --namespace kube-system \
    --set image.pullPolicy=Never --wait --timeout 60s >/dev/null
else
  info "Installing KWOK via Helm"
  helm install kwok kwok/kwok --namespace kube-system \
    --set image.pullPolicy=Never --wait --timeout 60s
fi

# ── Step 3: KWOK Stages ──────────────────────────────────────────────────
info "Applying KWOK stages (node-heartbeat, pod-ready, pod-delete)"
kubectl apply -f "${ROOT_DIR}/test/e2e/fixtures/kwok-stages.yaml"

# ── Step 4: Fake GPU nodes ───────────────────────────────────────────────
info "Creating fake GPU nodes"
kubectl apply -f "${ROOT_DIR}/test/e2e/fixtures/kwok-gpu-nodes.yaml"

# Wait for all fake nodes to be Ready
for i in $(seq 1 30); do
  READY=$(kubectl get nodes -l type=kwok --no-headers 2>/dev/null | grep -c " Ready" || true)
  if [[ "${READY}" -ge 8 ]]; then
    info "All 8 fake GPU nodes are Ready"
    break
  fi
  if [[ "$i" -eq 30 ]]; then
    echo "ERROR: Fake GPU nodes not ready after 30s"; exit 1
  fi
  sleep 1
done

# ── Step 5: DRA ResourceSlices ───────────────────────────────────────────
info "Creating DRA DeviceClass and ResourceSlices"
kubectl apply -f "${ROOT_DIR}/test/e2e/fixtures/dra-resourceslices.yaml" 2>/dev/null || \
  warn "DRA ResourceSlices not applied (API may not be available)"

# ── Step 6: Build scheduler ─────────────────────────────────────────────
info "Building scheduler for linux/${GOARCH}"
cd "${ROOT_DIR}"
GOOS=linux GOARCH="${GOARCH}" CGO_ENABLED=0 \
  go build -ldflags='-w -s' -o bin/kubenexus-scheduler-linux-${GOARCH} cmd/scheduler/main.go

info "Building Docker image"
cp "bin/kubenexus-scheduler-linux-${GOARCH}" kubenexus-scheduler
docker build --no-cache -f Dockerfile.simple -t "${SCHEDULER_IMAGE}" .
rm kubenexus-scheduler
kind load docker-image "${SCHEDULER_IMAGE}" --name "${CLUSTER_NAME}"

# ── Step 7: Deploy scheduler ────────────────────────────────────────────
info "Deploying KubeNexus scheduler"
sed "s|image: kubenexus-scheduler:latest|image: ${SCHEDULER_IMAGE}|" \
  "${ROOT_DIR}/deploy/kubenexus-scheduler.yaml" | kubectl apply -f -

kubectl rollout status deployment/kubenexus-scheduler \
  -n kubenexus-system --timeout=90s

# ── Step 8: Verify ──────────────────────────────────────────────────────
info "Verifying deployment"
kubectl get pods -n kubenexus-system -l app=kubenexus-scheduler
kubectl get nodes -l type=kwok --no-headers | wc -l | xargs -I{} echo "  {} fake GPU nodes"

echo ""
info "E2E infrastructure ready. Run tests with:"
echo "  go test ./test/e2e/ -v -count=1 -timeout 10m"
