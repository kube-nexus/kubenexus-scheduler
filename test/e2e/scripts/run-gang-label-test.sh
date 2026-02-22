#!/bin/bash
# Test script for label-based gang scheduling

set -e

echo "=== Label-Based Gang Scheduling Test ==="
echo

# Apply the workload
echo "1. Creating namespace and gang pods..."
kubectl apply -f test/e2e/fixtures/gang-label-test.yaml

echo
echo "2. Waiting for pods to be scheduled (gang scheduling - all 3 or none)..."
sleep 5

echo
echo "3. Checking pod status:"
kubectl get pods -n gang-test -o wide

echo
echo "4. Checking scheduler logs for coscheduling activity:"
kubectl logs -n kubenexus-system deployment/kubenexus-scheduler --tail=50 | grep -E "coscheduling|gang|pod-group" || echo "Check full logs for gang scheduling activity"

echo
echo "5. Pod scheduling details:"
for pod in gang-worker-1 gang-worker-2 gang-worker-3; do
  echo "--- Pod: $pod ---"
  status=$(kubectl get pod $pod -n gang-test -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
  node=$(kubectl get pod $pod -n gang-test -o jsonpath='{.spec.nodeName}' 2>/dev/null || echo "none")
  echo "  Status: $status, Node: $node"
done

echo
echo "=== Test Complete ==="
echo "Expected: All 3 pods scheduled together (gang semantics) or all remain pending."
echo ""
echo "Clean up with: kubectl delete namespace gang-test"
