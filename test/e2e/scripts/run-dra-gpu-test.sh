#!/usr/bin/env bash

# Test DRA GPU allocation with kubenexus-scheduler

set -e

CLUSTER_NAME="${1:-kubenexus-test}"

echo "=========================================="
echo "Testing DRA GPU Allocation"
echo "=========================================="
echo ""

# Check prerequisites
if ! kubectl get namespace dra-example-driver &> /dev/null; then
  echo "Error: DRA driver not installed"
  echo "Please run: ./hack/install-dra-driver.sh"
  exit 1
fi

if ! kubectl get deployment kubenexus-scheduler -n kubenexus-system &> /dev/null; then
  echo "Error: kubenexus-scheduler not deployed"
  echo "Please run: kubectl apply -f deploy/kubenexus-scheduler.yaml"
  exit 1
fi

echo "✓ Prerequisites met"
echo ""

# Preload busybox image to avoid TLS certificate issues
echo "Preloading busybox:1.36 image..."
if ! docker images | grep -q "busybox.*1.36"; then
  docker pull busybox:1.36
fi
kind load docker-image busybox:1.36 --name "$CLUSTER_NAME" 2>/dev/null || true
echo "✓ Image preloaded"
echo ""

# Show available GPU resources
echo "Available GPU ResourceSlices:"
kubectl get resourceslice
echo ""

# Deploy test pods
echo "Deploying test pods..."
kubectl apply -f test/e2e/fixtures/dra-gpu-test.yaml

echo "✓ Test pods created"
echo ""

# Wait for pods to start
echo "Waiting for pods to be scheduled..."
sleep 5

# Check pod status
echo "Pod status:"
kubectl get pods -n dra-gpu-test -o wide
echo ""

# Check ResourceClaims
echo "ResourceClaims:"
kubectl get resourceclaim -n dra-gpu-test
echo ""

# Wait for single GPU pod
echo "Waiting for single-gpu-pod..."
kubectl wait --for=condition=Ready pod/single-gpu-pod -n dra-gpu-test --timeout=60s || true

# Wait for dual GPU pod
echo "Waiting for dual-gpu-pod..."
kubectl wait --for=condition=Ready pod/dual-gpu-pod -n dra-gpu-test --timeout=60s || true

# Wait for gang pods
echo "Waiting for gang GPU pods..."
for i in 0 1 2; do
  kubectl wait --for=condition=Ready pod/gang-gpu-pod-$i -n dra-gpu-test --timeout=60s || true
done

echo ""

# Show logs from single GPU pod
echo "=========================================="
echo "Single GPU Pod Logs:"
echo "=========================================="
kubectl logs single-gpu-pod -n dra-gpu-test || echo "Pod not ready yet"
echo ""

# Show logs from dual GPU pod
echo "=========================================="
echo "Dual GPU Pod Logs:"
echo "=========================================="
kubectl logs dual-gpu-pod -n dra-gpu-test || echo "Pod not ready yet"
echo ""

# Show logs from gang GPU pods
echo "=========================================="
echo "Gang GPU Pods Logs:"
echo "=========================================="
for i in 0 1 2; do
  echo "--- gang-gpu-pod-$i ---"
  kubectl logs gang-gpu-pod-$i -n dra-gpu-test || echo "Pod not ready yet"
  echo ""
done

# Check scheduler logs for gang scheduling
echo "=========================================="
echo "Scheduler Logs (Gang Scheduling):"
echo "=========================================="
kubectl logs -n kubenexus-system deployment/kubenexus-scheduler --tail=50 | \
  grep -E "gpu-gang|Coscheduling|PreFilter|Permit" || \
  echo "No gang scheduling logs found yet"
echo ""

# Show final status
echo "=========================================="
echo "Final Status:"
echo "=========================================="
kubectl get pods -n dra-gpu-test -o wide
echo ""

echo "ResourceClaim allocation:"
kubectl get resourceclaim -n dra-gpu-test -o wide
echo ""

echo "=========================================="
echo "Test Complete!"
echo "=========================================="
echo ""
echo "To see detailed GPU allocations:"
echo "  kubectl logs single-gpu-pod -n dra-gpu-test"
echo "  kubectl logs dual-gpu-pod -n dra-gpu-test"
echo ""
echo "To verify gang scheduling worked:"
echo "  kubectl logs -n kubenexus-system deployment/kubenexus-scheduler | grep 'gpu-gang'"
echo ""
echo "Cleanup:"
echo "  kubectl delete -f test/e2e/fixtures/dra-gpu-test.yaml"
echo ""
