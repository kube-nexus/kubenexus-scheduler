# KubeNexus Scheduler - Agent Development Guide

KubeNexus Scheduler is a Kubernetes scheduler extender optimized for GPU-intensive AI/ML workloads at hyperscale. Built on the kube-scheduler framework (K8s 1.35+), it provides 14+ scheduling plugins for gang scheduling, GPU topology awareness, VRAM management, network fabric co-location, NUMA optimization, and multi-tenant resource isolation.

## Build/Lint/Test Commands

### Building
```bash
make build                    # Build scheduler + webhook binaries
make docker-build             # Build Docker images
go build -o bin/kubenexus-scheduler ./cmd/scheduler/
go build -o bin/kubenexus-webhook ./cmd/webhook/
```

### Linting
```bash
make lint                     # Run golangci-lint
make fmt                      # Format Go code
make vet                      # Run go vet
```

### Testing
```bash
make test                     # Run all unit tests
go test ./pkg/... -v          # Run all package tests
go test ./pkg/plugins/coscheduling/ -v  # Test single plugin
go test ./test/integration/ -v          # Integration tests
go test ./test/benchmark/ -bench=.      # Benchmarks

# Run specific test
go test -run TestCoscheduling ./pkg/plugins/coscheduling/

# E2E tests (requires kind cluster)
./hack/test-setup.sh          # Set up kind cluster
./hack/test-workloads.sh      # Deploy test workloads
go test ./test/e2e/ -v        # Run e2e suite
```

### Code Generation
```bash
./hack/update-codegen.sh      # Generate deepcopy, client code for CRDs
```

## Repository Structure

### Core Services (`/cmd/`)
- `scheduler/main.go` - Main entry point, registers all plugins with kube-scheduler framework
- `webhook/main.go` - Admission webhook for pod mutation (injects scheduler name, defaults)

### Scheduler Plugins (`/pkg/plugins/`) - Execution Order

**Phase 1: Classification (PreFilter)**
1. `profileclassifier` - Central hub: classifies tenant tier, workload type, gang status. Stores in CycleState for all other plugins.

**Phase 2: Gang Coordination (PreFilter, QueueSort, Permit, Reserve)**
2. `coscheduling` - Gang scheduling: FIFO ordering, starvation prevention, permit-wait-for-gang
3. `resourcereservation` - Palantir-style capacity booking via ResourceReservation CRD

**Phase 3: Filtering (Filter)**
4. `networkfabric` - NVLink clique filtering: hard-reject nodes in wrong NVLink partition for gang pods with `require-clique: true` or `co-locate: strict`
5. `numatopology` - NUMA alignment filter (single-numa-node policy for large pods)
6. `vramscheduler` - VRAM capacity filter (reject nodes with insufficient GPU memory)

**Phase 4: Scoring (Score)**
7. `workloadaware` - Bin-pack batch, spread services
8. `topologyspread` - Zone-aware HA spreading
9. `backfill` - Opportunistic scheduling for preemptible pods
10. `resourcefragmentation` - GPU island protection, pristine island preservation
11. `tenanthardware` - Match tenant tier to GPU tier (Gold→H100, Bronze→T4)
12. `vramscheduler` - VRAM utilization scoring with tenant-specific thresholds
13. `networkfabric` - Multi-level topology co-location for gang members (NVLink clique > fabric domain > rack > AZ)
14. `numatopology` - NUMA fit quality scoring

**Phase 5: Preemption (PostFilter)**
16. `preemption` - Gang-aware multi-victim preemption with tenant-tier protection

### Supporting Packages (`/pkg/`)
- `apis/scheduling/v1alpha1/` - ResourceReservation CRD types
- `scheduler/` - Shared types (SchedulingConfig, PodGroupStatus) and Prometheus metrics
- `utils/` - Pod group label parsing, PodGroupManager
- `workload/` - Pod workload classification (Service/Batch/Training/Inference)
- `webhook/` - Pod mutation webhook logic

### Configuration (`/config/`)
- `config.yaml` - Scheduler configuration
- `crd-resourcereservation.yaml` - ResourceReservation CRD manifest
- `crd-workload.yaml` - Workload CRD manifest

### Deployment (`/deploy/`)
- `kubenexus-scheduler.yaml` - Single-instance deployment
- `kubenexus-scheduler-ha.yaml` - HA deployment with leader election
- `webhook.yaml` - Admission webhook deployment

## Code Style Guidelines

### Import Organization
Organize imports in three groups separated by blank lines:
```go
import (
    // 1. Standard library
    "context"
    "fmt"
    "time"

    // 2. External dependencies (k8s, prometheus)
    v1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/runtime"
    framework "k8s.io/kube-scheduler/framework"

    // 3. Internal packages
    "github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/profileclassifier"
    schedulermetrics "github.com/kube-nexus/kubenexus-scheduler/pkg/scheduler"
)
```

### Naming Conventions
- **Files**: snake_case (`gang_preemption.go`, `pod_mutation.go`)
- **Plugin names**: PascalCase constants (`Name = "Coscheduling"`)
- **Labels/Annotations**: DNS-style (`scheduling.kubenexus.io/vram-request`)
- **Test files**: Same directory as source, `_test.go` suffix
- **Packages**: Lowercase, single word when possible

### Plugin Pattern
Every plugin follows this structure:
```go
type MyPlugin struct {
    handle framework.Handle
    // listers, state
}

var _ framework.ScorePlugin = &MyPlugin{}  // Compile-time interface check

const Name = "MyPlugin"

func (p *MyPlugin) Name() string { return Name }

func New(ctx context.Context, obj runtime.Object, handle framework.Handle) (framework.Plugin, error) {
    // Initialize listers from handle.SharedInformerFactory()
    return &MyPlugin{handle: handle}, nil
}
```

### ProfileClassifier Integration
All plugins should integrate with ProfileClassifier for tenant/workload awareness:
```go
// Get profile from CycleState (set by ProfileClassifier in PreFilter)
profile, err := profileclassifier.GetProfile(&state)
if err == nil && profile != nil {
    // Use profile.TenantTier, profile.WorkloadType, profile.IsGang, etc.
}
// Always have fallback logic if profile unavailable
```

### Metrics
Use the shared metrics package for all observability:
```go
import schedulermetrics "github.com/kube-nexus/kubenexus-scheduler/pkg/scheduler"

// Track plugin latency
start := time.Now()
defer func() {
    schedulermetrics.SchedulingLatency.WithLabelValues("MyPlugin", "Score").Observe(time.Since(start).Seconds())
}()
```

### Node Labels Convention
- GPU topology: `gpu.kubenexus.io/` prefix
- Network fabric: `network.kubenexus.io/` prefix
- NVLink partitions: `nvidia.com/gpu.clique` (set by NVIDIA DRA driver)
- GPU health: `gpu.kubenexus.io/health-*`
- Hardware tier: `hardware.kubenexus.io/` prefix
- NUMA: `topology.kubenexus.io/` prefix

### Logging
```go
klog.V(3).InfoS("Plugin decision made", "pod", pod.Name, "node", node.Name, "score", score)
klog.V(4).InfoS("Detailed scoring", "factors", factors)  // Debug detail
klog.V(5).InfoS("Trace-level", "rawData", data)           // Trace
klog.ErrorS(err, "Operation failed", "pod", pod.Name)     // Errors
```

### Comments
- Apache 2.0 copyright headers on all files
- GoDoc-style for exported functions/types
- Package-level comments explaining THE PROBLEM, THE SOLUTION, NODE LABELS, POD ANNOTATIONS
- Avoid obvious comments; explain "why" not "what"

## Key Design Decisions

### Plugin Execution Order
ProfileClassifier MUST run first in PreFilter. All other plugins depend on the SchedulingProfile stored in CycleState. If ProfileClassifier fails, plugins use local fallback classification.

### Gang Scheduling Pipeline
1. ProfileClassifier.PreFilter → classifies workload
2. Coscheduling.PreFilter → validates gang minAvailable
3. ResourceReservation.PreFilter → creates CRD reservations
4. All Filter/Score plugins → standard evaluation
5. Coscheduling.Permit → blocks until full gang ready (10s timeout)
6. ResourceReservation.PostBind → cleanup reservations

### VRAM Discovery Chain (graceful degradation)
1. DRA ResourceSlices (K8s 1.26+) → full GPU topology
2. NFD auto-discovery (K8s 1.18+) → PCI device ID mapping
3. Manual node labels (any K8s) → operator-provided

### Tenant Tier System
- **Gold**: Premium GPUs, strict health requirements, tight VRAM thresholds
- **Silver**: Standard GPUs, standard thresholds (default)
- **Bronze**: Economy GPUs, loose thresholds, backfill-eligible

## Target Environments

### Anthropic-style Giga Clusters (130K+ nodes)
- Emphasis on: Gang scheduling at scale, GPU island fragmentation prevention, NUMA topology, NVSwitch fabric co-location
- Key plugins: coscheduling, resourcefragmentation, networkfabric, numatopology

### OpenAI-style Multi-Cluster (smaller clusters, physically co-located)
- Emphasis on: Node health monitoring, NVLINK failure handling, IMEX/MIG partitioning, multi-layer scheduling
- Key plugins: nodehealthmonitor, vramscheduler (MIG support), backfill, tenanthardware
- Pattern: Global fleet scheduler → Cluster scheduler → Node-level decisions

### ComputeDomain Integration (NVIDIA GB200 NVL72)
- ComputeDomains = dynamic IMEX domains managed via NVIDIA DRA driver
- `networkfabric` plugin reads `nvidia.com/gpu.clique` labels for NVLink partition placement
- Filter phase hard-rejects cross-clique nodes when `require-clique: true` or `co-locate: strict`
- Score phase gives highest locality bonus (+40) for same-clique placement
- ComputeDomains handle IMEX lifecycle; scheduler handles workload-to-partition matching

## General Rules
- Use `git mv` when moving files to preserve history
- DO NOT add obvious comments that duplicate the code
- Keep comments short, explicit and concise
- Run `make lint && make test` after changes
- Test files MUST be in the same directory as the code they test
- All hardcoded thresholds should eventually migrate to ConfigMap-based configuration

## Dependencies
- Go 1.25+, Kubernetes 1.35.1
- kube-scheduler framework (k8s.io/kube-scheduler)
- Prometheus client for metrics
- Optional: DRA (K8s 1.26+), NFD, Kueue
