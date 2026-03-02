# KubeNexus Scheduler

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://golang.org/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.28+-326CE5?logo=kubernetes)](https://kubernetes.io/)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)
[![Status](https://img.shields.io/badge/Status-Beta-yellow.svg)]()

**Multi-Tenant Heterogeneous Workload Scheduler for Kubernetes**

Stop manually configuring pod specs. KubeNexus automatically routes Gold→H100, Bronze→L40, bin-packs training jobs, and spreads services—all through intelligent 3-axis placement optimization.

> **⚠️ Beta Status**: Ready for testing in dev/staging. Production use should be carefully evaluated.

---

## Positioning

**KubeNexus focuses on last-mile placement quality**: topology locality, fragmentation prevention, and workload-intent strategies.

- **Standalone**: Provides workload-aware placement + topology/interference control for mixed tenants and mixed intents using native Kubernetes primitives (PriorityClasses, ResourceQuotas, namespaces)
- **With Kueue**: Kueue provides admission/fairness/flavors; KubeNexus provides placement optimization within admitted intent

For multi-tenant fairness and admission control, KubeNexus can be layered with [Kueue](https://kueue.sigs.k8s.io/), while remaining fully usable standalone.

---

## The Problem

Modern AI/ML infrastructure requires:
- **Multiple Teams** (Gold/Silver/Bronze tiers)
- **Multiple Workload Types** (Training/Inference/Service/Batch)
- **Multiple Hardware Tiers** (H100/A100/L40 GPUs)

**Economic Waste**: Bronze teams land on expensive H100s. Gold teams find no H100 capacity. Training jobs spread across zones. Service workloads bin-pack on one node. $960k/year wasted on $2.4M GPU infrastructure through poor placement.

**Manual Complexity**: Multiple scheduler profiles, complex pod specs, per-team configuration.

## KubeNexus Solution

**Automatic 3-Axis Placement:**

✅ **WHO** (Tenant Tier): Gold→H100, Silver→A100, Bronze→L40  
✅ **WHAT** (Workload Type): Training→bin pack, Service→spread  
✅ **WHERE** (Hardware): NUMA, NVSwitch, GPU topology optimization

**One scheduler. Zero manual configuration.**

---

## Quick Example

### Before (Manual Configuration)
```yaml
# Every team needs custom pod specs
spec:
  nodeSelector:
    gpu-type: h100          # Manual per-team
  schedulerName: training-scheduler  # Multiple profiles
```

### After (Automatic)
```yaml
# Just use namespace + scheduler name
metadata:
  namespace: gold-team      # Auto-detects tier
spec:
  schedulerName: kubenexus-scheduler
# Automatically: Gold→H100, Training→bin-pack, NUMA-aligned
```

---

## Key Features

### 💰 Economic Multi-Tenant GPU Scheduling

**TenantHardware + VRAMScheduler** route teams to appropriate GPU tiers and match VRAM requirements.

```yaml
# Gold tenant with 70B model (80GB VRAM)
metadata:
  namespace: gold-team
  labels:
    vram.scheduling.kubenexus.io/required: "80Gi"
spec:
  schedulerName: kubenexus-scheduler
# Result: Routes to H100-80GB, filters A100-40GB
```

**Value**: $960k/year savings on $2.4M infrastructure through optimal placement.

📖 [Details](docs/FEATURES.md#economic-multi-tenant-gpu-scheduling)

### 🔄 Workload-Aware Placement

Native K8s: Pick ONE strategy (spread OR bin-pack) for ALL pods.  
**KubeNexus**: Adapts per workload automatically.

```yaml
# Training → Bin pack (GPU locality)
workload.kubenexus.io/type: training

# Service → Spread (high availability)
workload.kubenexus.io/type: service
```

**Value**: Optimal placement without multiple scheduler profiles.

📖 [Details](docs/FEATURES.md#workload-aware-placement)

### 🎯 Gang Scheduling

All-or-nothing scheduling with cross-plugin awareness.

```yaml
metadata:
  labels:
    pod-group.scheduling.sigs.k8s.io/name: distributed-training
    pod-group.scheduling.sigs.k8s.io/min-available: "64"
# Gang of 64 GPUs schedules atomically or waits
# Works with: ResourceReservation, BackfillScoring, WorkloadAware
```

**Value**: Prevents partial gang placement and deadlock.

📖 [Kubeflow Integration](docs/KUBEFLOW_INTEGRATION.md) | [Spark Integration](docs/SPARK_OPERATOR_INTEGRATION.md) | [Details](docs/FEATURES.md#gang-scheduling)

### 🧠 NUMA-Aware Scheduling

2-3x faster GPU training through CPU/Memory/GPU topology alignment.

```yaml
annotations:
  numa.scheduling.kubenexus.io/policy: "single-numa"
  numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu"
```

**Policies**: single-numa, restricted, best-effort, none

📖 [NUMA Guide](docs/NUMA_SCHEDULING_GUIDE.md) | [Quick Reference](docs/NUMA_QUICK_REFERENCE.md)

### 🌐 Network Fabric-Aware

Keeps distributed training within NVSwitch/NVLink domains (100 score) vs Ethernet (50 score).

📖 [Details](docs/FEATURES.md#network-fabric-aware-scheduling)

### ⚖️ Multi-Tenant Placement Quality

**Standalone capabilities** (no admission controller needed):

- **Tenant-aware placement**: Gold→premium GPUs, Bronze→economy GPUs
- **Fragmentation prevention**: Blocks interference (Bronze jobs don't fragment Gold's 8-GPU pools)
- **Preemption hierarchy**: Gold can preempt Silver/Bronze
- **Starvation prevention**: Age-based priority boost after 60s
- **Backfill placement**: Bronze uses idle Gold capacity (preempted when Gold returns)

**With Kueue integration** (adds admission control):

- **Quotas & fairness**: ResourceQuotas, cohort borrowing, weighted fair share
- **Queue management**: Prevents cluster flooding, prioritizes admission
- **Kueue FlavorFungibility**: Kueue admits, KubeNexus optimizes node placement within flavor

```yaml
# Kueue admits pod (quota check) → KubeNexus schedules (topology optimization)
apiVersion: kueue.x-k8s.io/v1beta1
kind: LocalQueue
metadata:
  annotations:
    scheduling.kubenexus.io/tier: "gold"
```

📖 [Details](docs/FEATURES.md#multi-tenant-placement-quality) | [Architecture](docs/ARCHITECTURE.md)

---

## Installation

```bash
# 1. Install CRDs
kubectl apply -f config/crd-workload.yaml
kubectl apply -f config/crd-resourcereservation.yaml

# 2. Deploy KubeNexus Scheduler
kubectl apply -f deploy/kubenexus-scheduler.yaml

# 3. Label namespaces with tenant tiers
kubectl label namespace gold-team scheduling.kubenexus.io/tier=gold
kubectl label namespace bronze-team scheduling.kubenexus.io/tier=bronze

# 4. Use in pods
apiVersion: v1
kind: Pod
metadata:
  namespace: gold-team
spec:
  schedulerName: kubenexus-scheduler
  containers:
  - name: training
    resources:
      requests:
        nvidia.com/gpu: 8
```

📖 [Complete Installation Guide](docs/USER_GUIDE.md) | [GPU Cluster Guide](docs/GPU_CLUSTER_GUIDE.md)

---

## Architecture

### ProfileClassifier: Tenant + Workload Identity

Every pod gets classified into a **SchedulingProfile** (WHO + WHAT):

```go
type SchedulingProfile struct {
    TenantTier    TenantTier   // gold / silver / bronze
    WorkloadType  WorkloadType // training / service / batch
    // ... more fields
}
```

All plugins read this profile for intelligent decisions.

### Plugin Pipeline

```
PreFilter → ProfileClassifier (classify WHO + WHAT)
  ↓
Filter → ResourceReservation, NUMATopology (feasibility)
  ↓
Score → TenantHardware, WorkloadAware, VRAMScheduler, NetworkFabric (optimization)
  ↓
Permit → Coscheduling (gang coordination)
  ↓
PostFilter → GangPreemption (atomic preemption)
```

📖 [Full Architecture](docs/ARCHITECTURE.md) | [Design Decisions](docs/DESIGN_DECISIONS.md)

---

## Integrations

### Kueue Integration

**Architecture**: Kueue (admission control) + KubeNexus (placement optimization)

```yaml
apiVersion: kueue.x-k8s.io/v1beta1
kind: LocalQueue
metadata:
  annotations:
    scheduling.kubenexus.io/tier: "gold"
```

**Flow**: 
1. Kueue checks quota → Admits pod to cluster
2. KubeNexus optimizes node placement (topology, fragmentation, NUMA)

📖 [Kueue Integration Guide](docs/ARCHITECTURE.md#kueue-integration)

### Operator Support

- **Kubeflow Training/MPI Operators**: Gang scheduling + intelligent placement
- **Spark Operator**: Driver/executor anti-affinity
- **Ray Operator**: Head/worker placement strategies
- **PyTorch/TensorFlow Operators**: Distributed training optimization

📖 [Kubeflow Integration](docs/KUBEFLOW_INTEGRATION.md) | [Spark Integration](docs/SPARK_OPERATOR_INTEGRATION.md) | [Operator Support](docs/OPERATOR_CRD_SUPPORT.md)

---

## Comparison

| Feature | **KubeNexus** | Volcano | YuniKorn | Kueue | Native K8s |
|---------|---------------|---------|----------|-------|------------|
| **Multi-Tenant GPU Routing** | ✅ Automatic | ❌ Manual nodeSelector | ❌ Manual | ❌ (FlavorFungibility only) | ❌ Manual |
| **Workload-Aware Placement** | ✅ Auto per-pod | ❌ Global policy | ❌ Global | ❌ | ❌ |
| **NUMA Topology** | ✅ CPU+Mem+GPU | Basic | ❌ | ❌ | ❌ |
| **GPU Fragmentation Prevention** | ✅ Tenant-aware | ❌ | ❌ | ❌ | ❌ |
| **VRAM Scheduling** | ✅ Utilization-based | ❌ | ❌ | ❌ | ❌ |
| **Gang Scheduling** | ✅ Cross-plugin | ✅ Basic | ✅ Basic | ✅ | ❌ |
| **Admission Control** | ➕ Via Kueue | ✅ Built-in | ✅ Built-in | ✅ Core feature | ResourceQuota |
| **Best For** | Multi-tenant heterogeneous GPU | Batch jobs | Large multi-tenant | Quota management | Simple workloads |

📖 [Detailed Comparison](docs/SCHEDULER_COMPARISON.md) | [vs Upstream](docs/UPSTREAM_COMPARISON.md) | [Competitive Advantage](docs/COMPETITIVE_ADVANTAGE.md)

---

## Documentation

- **User Guide**: [docs/USER_GUIDE.md](docs/USER_GUIDE.md)
- **GPU Cluster Setup**: [docs/GPU_CLUSTER_GUIDE.md](docs/GPU_CLUSTER_GUIDE.md)
- **NUMA Scheduling**: [docs/NUMA_SCHEDULING_GUIDE.md](docs/NUMA_SCHEDULING_GUIDE.md)
- **Features Deep Dive**: [docs/FEATURES.md](docs/FEATURES.md)
- **Architecture**: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- **Testing Guide**: [docs/TESTING_GUIDE.md](docs/TESTING_GUIDE.md)
- **Kubeflow Integration**: [docs/KUBEFLOW_INTEGRATION.md](docs/KUBEFLOW_INTEGRATION.md)
- **Spark Integration**: [docs/SPARK_OPERATOR_INTEGRATION.md](docs/SPARK_OPERATOR_INTEGRATION.md)

---

## Roadmap

**v0.2** (Current):
- ✅ ProfileClassifier (tenant + workload classification)
- ✅ Gang scheduling with cross-plugin awareness
- ✅ NUMA topology scheduling
- ✅ Network fabric-aware placement
- ✅ Kueue integration

**v0.3** (Next):
- ⏳ DRA (Dynamic Resource Allocation) for GPU pools
- ⏳ Enhanced preemption (checkpoint/restore)
- ⏳ Multi-cluster scheduling
- ⏳ Advanced metrics & observability

**v0.4+**:
- Dominant Resource Fairness (DRF)
- Weighted fair share
- GPU time-slicing support

---

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

Issues, PRs, and feedback: [github.com/kubenexus/scheduler](https://github.com/example-org/kubenexus-scheduler)

---

## Community & Support

- **Documentation**: [docs/](docs/)
- **Discussions**: GitHub Discussions
- **Issues**: GitHub Issues
- **Security**: [SECURITY.md](SECURITY.md)

---

## License

Apache License 2.0 - See [LICENSE](LICENSE) for details.
