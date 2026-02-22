#!/bin/bash
# Test script for K8s 1.35 Workload API gang scheduling
# NOTE: Requires Kueue to be installed

set -e

echo "=== K8s 1.35 Workload API Gang Scheduling Test ==="
echo

# Check if Kueue is installed
if ! kubectl get deployment -n kueue-system kueue-controller-manager &>/dev/null; then
    echo "ERROR: Kueue is not installed!"
    echo "The Workload API requires Kueue for full functionality."
    echo ""
    echo "Install Kueue with:"
    echo "  kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/v0.10.0/manifests.yaml"
    echo ""
    echo "For testing without Kueue, use the label-based approach:"
    echo "  ./test/e2e/scripts/run-gang-label-test.sh"
    echo ""
    exit 1
fi

echo "✓ Kueue detected"
echo

# Apply the workload
echo "1. Creating namespace and Workload..."
kubectl apply -f test/e2e/fixtures/workload-api-test.yaml

echo
echo "2. Waiting for pods to be scheduled (gang scheduling - all or none)..."
sleep 3

echo
echo "3. Checking pod status:"
kubectl get pods -n workload-test -o wide

echo
echo "4. Checking Workload status:"
kubectl get workload -n workload-test -o yaml

echo
echo "5. Checking scheduler logs for coscheduling activity:"
kubectl logs -n kubenexus-system deployment/kubenexus-scheduler --tail=50 | grep -i "workload\|coscheduling\|gang" || echo "No specific gang scheduling logs found"

echo
echo "6. Pod scheduling details:"
for pod in worker-1 worker-2 worker-3; do
  echo "--- Pod: $pod ---"
  kubectl get pod $pod -n workload-test -o jsonpath='{.status.conditions[?(@.type=="PodScheduled")].status}' && echo " - Scheduled"
  kubectl get pod $pod -n workload-test -o jsonpath='{.spec.nodeName}' && echo
done

echo
echo "=== Test Complete ==="
echo "All 3 pods should be scheduled together (gang semantics) or remain pending together."
