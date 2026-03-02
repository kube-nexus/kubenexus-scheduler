# KubeNexus Features

## Economic Multi-Tenant GPU Scheduling

### Problem: Manual Tenant-to-Hardware Mapping

In native Kubernetes, you need manual configuration:

```yaml
# Gold tenant pods
spec:
  nodeSelector:
    gpu-type: h100
  
# Silver tenant pods  
spec:
  nodeSelector:
    gpu-type: a100
    
# Bronze tenant pods
spec:
  nodeSelector:
    gpu-type: l40
```

**Issues:**
- Manual pod spec configuration per tenant
- No automatic routing
- Easy to misconfigure (Bronze gets H100 = waste)

### KubeNexus Solution: Automatic Routing

```yaml
# Gold Tenant - Automatically routed to H100
apiVersion: v1
kind: Pod
metadata:
  namespace: gold-team  # Namespace labeled tenant.kubenexus.io/tier: gold
spec:
  schedulerName: kubenexus-scheduler
  containers:
  - name: training
    resources:
      requests:
        nvidia.com/gpu: 1
# TenantHardware scores: H100=100, A100=70, L40=20
# Result: Lands on H100 automatically
```

### VRAM-Aware Scheduling

Matches GPU memory requirements to available VRAM:

```yaml
# Gold tenant: 70B model (80GB VRAM) → Perfect fit on H100
apiVersion: v1
kind: Pod
metadata:
  labels:
    vram.scheduling.kubenexus.io/required: "80Gi"
spec:
  schedulerName: kubenexus-scheduler
# VRAMScheduler: 80GB/80GB = 100% utilization = Score 100
# Filters nodes with <80GB VRAM (A100-40GB, L40-48GB)
# Only H100-80GB or A100-80GB qualify
```

**Benefits:**
- Prevents OOM errors (filters insufficient VRAM)
- Maximizes GPU utilization (matches VRAM needs)
- Economic efficiency (right-sized GPU for workload)

### DRA (Dynamic Resource Allocation) Support

KubeNexus reads GPU topology from DRA ResourceSlices:

```yaml
apiVersion: resource.k8s.io/v1alpha3
kind: ResourceSlice
metadata:
  name: node1-gpus
spec:
  nodeName: node1
  devices:
  - name: gpu-0
    basic:
      attributes:
        memory: 80Gi
        compute: 142TFLOPS
```

VRAMScheduler queries ResourceSlices for accurate GPU VRAM capacity.

## Automatic Workload-Aware Placement

### Native Kubernetes Problem

```yaml
# Option 1: LeastAllocated (spread) for ALL workloads
priorityPolicy:
  - weight: 1
    name: LeastAllocated
# ❌ Training: GPUs spread across racks = slow network
# ✅ Services: Distributed for HA

# Option 2: MostAllocated (bin pack) for ALL workloads
priorityPolicy:
  - weight: 1
    name: MostAllocated  
# ✅ Training: GPUs on same node = fast NVLink
# ❌ Services: All replicas on same node = no HA
```

**You can't have both.** Pick ONE strategy for ALL pods.

### KubeNexus Solution: Adaptive Strategy

```yaml
# Training: Automatic bin-packing
apiVersion: v1
kind: Pod
metadata:
  labels:
    workload.kubenexus.io/type: training
spec:
  schedulerName: kubenexus-scheduler
# WorkloadAware → BinPackingScore (consolidate for GPU locality)
# NetworkFabric → Prefer same NVSwitch fabric

---

# Service: Automatic spreading
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    workload.kubenexus.io/type: service
spec:
  replicas: 10
  template:
    spec:
      schedulerName: kubenexus-scheduler
# WorkloadAware → SpreadScore (distribute for HA)
# TopologySpread → Zone spread across availability zones
```

**One scheduler. Automatic adaptation. No manual configuration.**

### Workload Type Detection

**Automatic detection from:**
1. Labels: `workload.kubenexus.io/type`
2. Operators: PyTorchJob → training, Deployment → service
3. Resource patterns: GPU requests → likely training
4. Default: Service workload

## Gang Scheduling (Coscheduling)

### Problem: Partial Scheduling Deadlock

```
Gang needs 8 pods
6 pods schedule successfully
2 pods stuck pending (no resources)
6 pods consuming resources but waiting forever
→ Deadlock: Can't proceed, can't release resources
```

### KubeNexus Solution: All-or-Nothing

```yaml
apiVersion: v1
kind: Pod
metadata:
  labels:
    pod-group.scheduling.kubenexus.io/name: "training-job"
    pod-group.scheduling.kubenexus.io/min-available: "8"
spec:
  schedulerName: kubenexus-scheduler
```

**Behavior:**
- Pods enter Permit phase (held by Coscheduling plugin)
- Wait until 8/8 pods are feasible
- All 8 pods bind atomically
- If timeout (10s), all reject and retry

**Operator Integration:**

Works with any operator that creates pods:
- Kubeflow Training Operator (PyTorchJob, TFJob, MPIJob)
- Spark Operator (SparkApplication)
- Ray Operator
- Custom operators

Add labels to pod template, KubeNexus handles the rest.

### Starvation Prevention

**Problem:** Large gang (64 pods) starves small gang (4 pods)

**Solution:** Age-based priority boost
```
Small gang waiting >60s → Priority +100
Now higher priority than just-arrived large gang
Small gang schedules first
```

### Gang Preemption

**Problem:** Gang needs 8 GPUs, cluster has 10 GPUs with 8 used by various pods

**Native K8s:** Preempts pods one-by-one → Partial gang still can't run

**KubeNexus GangPreemption:** 
- Finds set of victim pods that frees 8 GPUs
- Preempts entire set atomically
- Gang schedules immediately

## NUMA-Aware Scheduling

### Problem: Cross-NUMA Penalties

```
Node with 2 NUMA domains:
- NUMA 0: CPUs 0-63, Memory 0-256GB, GPUs 0-3
- NUMA 1: CPUs 64-127, Memory 256-512GB, GPUs 4-7

Pod gets:
- CPUs from NUMA 0
- Memory from NUMA 1
- GPUs from NUMA 1

Result: Cross-NUMA memory access = 2-3x slower
```

### KubeNexus NUMA Policies

**1. Single NUMA Node**
```yaml
annotations:
  numa.scheduling.kubenexus.io/policy: "single-numa"
  numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu"
# Allocates ALL resources from same NUMA node
# Use for: GPU training, HPC workloads
```

**2. Restricted**
```yaml
annotations:
  numa.scheduling.kubenexus.io/policy: "restricted"
  numa.scheduling.kubenexus.io/resources: "nvidia.com/gpu"
# GPUs from same NUMA, CPU/Memory can span
# Use for: Multi-GPU training
```

**3. Best Effort**
```yaml
annotations:
  numa.scheduling.kubenexus.io/policy: "best-effort"
# Prefers NUMA alignment, allows fallback
# Use for: Most workloads (default)
```

**4. None**
```yaml
annotations:
  numa.scheduling.kubenexus.io/policy: "none"
# Ignores NUMA topology
# Use for: Network-bound workloads
```

### NUMA Performance Impact

**GPU Training Example:**
- Cross-NUMA: 100 images/sec
- Single-NUMA: 280 images/sec
- **2.8x speedup** from NUMA-aware scheduling

## Network Fabric-Aware Scheduling

### Problem: GPU Communication Latency

```
8-GPU distributed training:
- 4 GPUs on Node A (NVSwitch fabric-0)
- 4 GPUs on Node B (Ethernet fabric-1)

Result: Cross-fabric communication = 10x slower than NVSwitch
```

### KubeNexus NetworkFabric Plugin

**Fabric Hierarchy (scores):**
1. NVSwitch: 100 (highest priority)
2. NVLink: 90
3. InfiniBand: 80
4. RoCE: 70
5. Ethernet: 50

**Bonus scores:**
- Same NVSwitch domain: +20
- Same rack: +10
- Same availability zone: +5

**Result:** Training pods land on nodes with fastest interconnect.

### GPU Island Awareness

```yaml
# Node labels
metadata:
  labels:
    fabric.kubenexus.io/type: "nvswitch"
    fabric.kubenexus.io/domain: "superpod-1"  # 64-GPU island
    fabric.kubenexus.io/rack: "rack-42"
```

NetworkFabric plugin scores nodes in same domain higher → Keeps training job within GPU island.

## Backfill Scheduling

### Problem: Stranded Idle Capacity

```
Cluster: 100 GPUs
Usage: 50 GPUs by prod workloads
Reserved: 30 GPUs for incoming ML job (SLA guarantee)
Idle: 20 GPUs sitting empty

Waste: 20 GPUs × $10/hr = $200/hr wasted
```

### KubeNexus Solution: Opportunistic Backfill

```yaml
# Low-priority batch job
apiVersion: v1
kind: Pod
metadata:
  labels:
    backfill.scheduling.kubenexus.io/eligible: "true"
spec:
  priorityClassName: low-priority  # Can be preempted
  schedulerName: kubenexus-scheduler
```

**Behavior:**
- BackfillScoring places low-priority pods on idle capacity
- High-priority job arrives → GangPreemption evicts backfill pods
- No capacity wasted, no SLA violation

## Multi-Tenant Fairness

### Quota vs. Fair Share

**Kubernetes ResourceQuota:**
```yaml
apiVersion: v1
kind: ResourceQuota
spec:
  hard:
    requests.nvidia.com/gpu: "10"
```

**Problem:** Hard limit. Gold tenant with 10 GPU quota can't use 11th GPU even if cluster has 50 idle GPUs.

**KubeNexus Approach:** Soft priorities + preemption
- Gold tenant can use ALL idle capacity
- Silver tenant gets preempted when Gold needs resources
- No hard quotas → Better utilization

### Integration with Kueue

Kueue provides admission control + quotas. KubeNexus provides intelligent placement:

```
Kueue: Admits pod (within quota)
  ↓
KubeNexus: Schedules to optimal node (tenant tier + workload type + topology)
```

**Complementary, not competitive.**

## Resource Reservation

### Problem: Gang Fragmentation

```
Gang needs 8 GPUs
- Pod 1-6 schedule immediately
- Pod 7-8 pending
- Other pods arrive, consume last 2 GPUs
- Gang 1-6 holding resources but can't complete
→ Fragmentation
```

### ResourceReservation Plugin

```yaml
# Automatic reservation
apiVersion: scheduling.kubenexus.io/v1alpha1
kind: ResourceReservation
metadata:
  name: gang-reservation
spec:
  reservations:
    member-0: {cpu: 4, memory: 16Gi, nvidia.com/gpu: 1, node: ""}
    member-1: {cpu: 4, memory: 16Gi, nvidia.com/gpu: 1, node: ""}
    # ... 6 more members
```

**How it works:**
1. First gang pod → Creates ResourceReservation
2. Reserve capacity for all 8 members
3. Other pods filtered out (reserved capacity not available)
4. Gang completes → Reservation deleted

**Result:** Prevents gang fragmentation.
