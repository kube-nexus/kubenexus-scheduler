# End-to-End Tests

E2E tests for KubeNexus Scheduler using Kind + KWOK fake GPU nodes.

## Quick Start

```bash
# Setup cluster with fake GPU nodes + deploy scheduler
make e2e-setup      # or: ./hack/e2e-setup.sh

# Run e2e tests
make e2e-test       # or: go test ./test/e2e/ -v

# Teardown
make e2e-teardown   # or: ./hack/e2e-setup.sh teardown

# Full pipeline (setup + test)
make e2e
```

## Architecture

```
Kind cluster (control-plane only)
├── KWOK controller (manages fake node heartbeats + pod lifecycle)
├── KubeNexus Scheduler (deployed as pod)
├── 8 KWOK fake GPU nodes:
│   ├── Rack A (NVSwitch, H100 Gold, clique-0 + clique-1): gpu-node-01..04
│   ├── Rack B (InfiniBand, A100 Silver): gpu-node-05..06
│   └── Rack C (Ethernet, T4 Bronze, us-east-1b): gpu-node-07..08
└── DRA ResourceSlice objects (device-level GPU attributes)
```

## Directory Structure

```
test/e2e/
├── e2e_test.go                    # Go e2e tests (8 tests)
├── README.md
├── fixtures/
│   ├── kind-e2e.yaml              # Kind config (control-plane only, DRA enabled)
│   ├── kwok-gpu-nodes.yaml        # 8 fake GPU nodes with topology labels
│   ├── kwok-stages.yaml           # KWOK stage configs for pod lifecycle
│   ├── dra-resourceslices.yaml    # DRA ResourceSlice objects
│   ├── dra-gpu-test.yaml          # DRA DeviceClass + ResourceClaimTemplates
│   └── ...
└── scripts/
    └── ...
```

## Tests

| Test | What it verifies |
|------|-----------------|
| `TestTrainingPodGetsGoldGPU` | Training pods land on Gold/Silver tier, not Bronze |
| `TestServicePodsSpread` | Service pods spread across multiple nodes |
| `TestGangCliqueCoLocation` | Gang pods with require-clique stay in same NVLink partition |
| `TestTenantHardwareMatching` | Gold tenant → H100, Bronze tenant → T4 |
| `TestNetworkFabricPreference` | Training pods prefer NVSwitch/InfiniBand over Ethernet |
| `TestTopologySpreadAcrossAZs` | Service pods distribute across availability zones |
| `TestDRAResourceSlicesExist` | DRA API is available and scheduler has RBAC |
| `TestUnschedulableExcessGPU` | Pod requesting 16 GPUs stays unschedulable |

## Prerequisites

- Docker running
- `kind` v0.31+
- `kubectl`
- `helm` v3+

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
