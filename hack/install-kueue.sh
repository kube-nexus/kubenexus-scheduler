#!/bin/bash
# Setup script for Kueue - Required for Workload API support

set -e

CLUSTER_NAME=${1:-kubenexus-test}

echo "=== Kueue Installation ==="
echo

# Check if already installed
if kubectl get deployment -n kueue-system kueue-controller-manager &>/dev/null; then
    echo "✓ Kueue is already installed"
    kubectl get pods -n kueue-system
    exit 0
fi

echo "1. Pre-loading Kueue images into Kind cluster..."
./hack/load-kueue-images.sh ${CLUSTER_NAME}

echo
echo "2. Installing Kueue v0.10.0..."
kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/v0.10.0/manifests.yaml

echo
echo "2. Waiting for Kueue controller to be ready (may take up to 5 minutes)..."
kubectl wait --for=condition=available --timeout=300s deployment/kueue-controller-manager -n kueue-system 2>&1 || {
    echo "⚠ Timeout waiting for Kueue, checking pod status..."
    kubectl get pods -n kueue-system
    kubectl describe pod -n kueue-system -l control-plane=controller-manager | tail -20
}

echo
echo "3. Verifying Kueue installation..."
kubectl get pods -n kueue-system

echo
echo "4. Checking Workload API availability..."
if kubectl api-resources | grep -q "workloads.*scheduling.k8s.io"; then
    echo "✓ Workload API is now available!"
    kubectl api-resources | grep workload
else
    echo "⚠ Workload API not yet available, may need a moment..."
fi

echo
echo "=== Kueue Installation Complete ==="
echo
echo "You can now use the Workload API for gang scheduling:"
echo "  kubectl apply -f test/e2e/fixtures/workload-api-test.yaml"
echo
echo "Or run the test:"
echo "  ./test/e2e/scripts/run-workload-api-test.sh"
