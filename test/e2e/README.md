# End-to-End Tests

This directory contains end-to-end tests for KubeNexus Scheduler.

## Directory Structure

```
test/e2e/
├── e2e_test.go              # Go-based E2E tests
├── fixtures/                # YAML test fixtures
│   ├── gang-label-test.yaml          # Label-based gang scheduling test
│   ├── workload-api-test.yaml        # Workload API test (requires Kueue)
│   └── kind-config.yaml              # Kind cluster configuration
└── scripts/                 # Test scripts
    ├── run-gang-label-test.sh        # Run gang scheduling test
    └── run-workload-api-test.sh      # Run Workload API test
```

## Running Tests

### Prerequisites

- Kind cluster running (K8s 1.35+ recommended)
- KubeNexus Scheduler deployed
- kubectl configured

### Label-Based Gang Scheduling Test (Recommended)

This test uses label-based gang scheduling which works out of the box:

```bash
./test/e2e/scripts/run-gang-label-test.sh
```

**Expected behavior**: All 3 pods in the gang are scheduled together atomically. The scheduler waits until all pods are ready before binding any of them.

**Labels used**:
- `pod-group.scheduling.sigs.k8s.io/name`: Gang name
- `pod-group.scheduling.sigs.k8s.io/min-available`: Minimum pods required

### Workload API Test (Requires Kueue)

This test uses the Kubernetes native Workload API. 

#### Install Kueue

Use our setup script:

```bash
./hack/install-kueue.sh
```

Or install manually:

```bash
# Install Kueue first
kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/v0.10.0/manifests.yaml

# Wait for it to be ready
kubectl wait --for=condition=available --timeout=300s deployment/kueue-controller-manager -n kueue-system
```

#### Run the test

```bash
./test/e2e/scripts/run-workload-api-test.sh
```

### Go-Based Tests

```bash
cd test/e2e
go test -v ./...
```

## Test Fixtures

All YAML test fixtures are in the `fixtures/` directory:

- **gang-label-test.yaml**: Complete gang scheduling example with 3 pods
- **workload-api-test.yaml**: Workload API example (requires Kueue)
- **kind-config.yaml**: Kind cluster configuration for local testing

## Clean Up

```bash
# Clean up gang test
kubectl delete namespace gang-test

# Clean up workload test  
kubectl delete namespace workload-test
```

## Verification

Check scheduler logs to see gang scheduling in action:

```bash
kubectl logs -n kubenexus-system deployment/kubenexus-scheduler --tail=100 | grep -E "coscheduling|gang|permit"
```

You should see logs like:
```
PreFilter: podGroup gang-test/test-gang has 3 pods, needs 3
Permit: podGroup gang-test/test-gang waiting for more pods (2/3)
Permit: podGroup gang-test/test-gang ready to schedule (3/3)
Permit: allowing pod gang-test/gang-worker-1
```

## Adding New Tests

1. Create YAML fixture in `fixtures/`
2. Create test script in `scripts/` if needed
3. Add Go test in `e2e_test.go` for automated testing
4. Update this README

## Notes

- **Label-based approach is production-ready** and fully tested
- **Workload API approach requires Kueue** and is for advanced use cases
- All tests should run from the repository root directory
- Use absolute or relative paths from root (e.g., `test/e2e/fixtures/...`)
