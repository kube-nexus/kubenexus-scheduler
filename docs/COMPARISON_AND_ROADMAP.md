# KubeNexus vs YuniKorn vs Volcano - Feature Comparison & Roadmap

**Last Updated**: February 16, 2026

## 🎯 Executive Summary

**KubeNexus** is a lightweight alternative to YuniKorn/Volcano, focusing on **simplicity and essential features** rather than feature parity. Think of it as:
- **YuniKorn/Volcano**: The "Swiss Army Knife" - feature-rich, complex, heavy
- **KubeNexus**: The "Precision Tool" - lightweight, focused, production-ready

---

## 📊 Detailed Feature Comparison

### Core Scheduling Features

| Feature | KubeNexus (v1.0) | YuniKorn | Volcano | Priority |
|---------|------------------|----------|---------|----------|
| **Gang Scheduling** | ✅ Full | ✅ Full | ✅ Full | ✅ DONE |
| **Resource Reservation** | ✅ Dynamic | ✅ Queue-based | ⚠️ Limited | ✅ DONE |
| **FIFO Ordering** | ✅ Yes | ✅ Yes | ✅ Yes | ✅ DONE |
| **Priority Scheduling** | ✅ Yes | ✅ Yes | ✅ Yes | ✅ DONE |
| **Bin Packing** | ✅ Basic | ✅ Advanced | ✅ Advanced | 🟡 Enhance |
| **Fair Sharing** | ❌ No | ✅ Yes | ✅ Yes | 🔵 Future |
| **Queue Hierarchies** | ❌ No | ✅ Yes | ✅ Yes | 🔵 Future |
| **Preemption** | ❌ No | ✅ Yes | ✅ Yes | 🔴 HIGH |

### Topology & Resource Awareness

| Feature | KubeNexus (v1.0) | YuniKorn | Volcano | Priority |
|---------|------------------|----------|---------|----------|
| **Node Affinity** | ✅ Basic (K8s native) | ✅ Full | ✅ Full | ✅ DONE |
| **Topology-Aware Scheduling** | ❌ No | ✅ Yes | ✅ Yes | 🔴 HIGH |
| **Zone/Region Awareness** | ❌ No | ✅ Yes | ✅ Yes | 🔴 HIGH |
| **GPU Scheduling** | ❌ No | ✅ Yes | ✅ Yes | 🟡 Medium |
| **NUMA Awareness** | ❌ No | ⚠️ Limited | ✅ Yes | 🔵 Future |
| **Network Topology** | ❌ No | ❌ No | ⚠️ Limited | 🔵 Future |
| **Storage Locality** | ❌ No | ⚠️ Limited | ⚠️ Limited | 🔵 Future |

### Multi-Tenancy & Isolation

| Feature | KubeNexus (v1.0) | YuniKorn | Volcano | Priority |
|---------|------------------|----------|---------|----------|
| **Queue Management** | ❌ No | ✅ Full | ✅ Full | 🔴 HIGH |
| **Queue Hierarchies** | ❌ No | ✅ Yes | ✅ Yes | 🔴 HIGH |
| **Resource Quotas** | ⚠️ K8s native | ✅ Advanced | ✅ Advanced | 🟡 Medium |
| **Multi-Tenancy** | ⚠️ Basic | ✅ Full | ✅ Full | 🔴 HIGH |
| **ACLs/RBAC** | ✅ K8s RBAC | ✅ Advanced | ✅ Advanced | ✅ DONE |

### Workload Support

| Feature | KubeNexus (v1.0) | YuniKorn | Volcano | Priority |
|---------|------------------|----------|---------|----------|
| **Spark** | ✅ Optimized | ✅ Full | ✅ Full | ✅ DONE |
| **TensorFlow/PyTorch** | ✅ Gang support | ✅ Full | ✅ Full | ✅ DONE |
| **MPI/HPC** | ✅ Gang support | ✅ Full | ✅ Full | ✅ DONE |
| **Ray** | ⚠️ Basic | ✅ Yes | ✅ Yes | 🟡 Medium |
| **Flink** | ⚠️ Basic | ✅ Yes | ✅ Yes | 🟡 Medium |
| **Stateful Sets** | ✅ Basic | ✅ Yes | ✅ Yes | ✅ DONE |
| **Job Queues** | ❌ No | ✅ Yes | ✅ Yes | 🔴 HIGH |

### Operational Features

| Feature | KubeNexus (v1.0) | YuniKorn | Volcano | Priority |
|---------|------------------|----------|---------|----------|
| **High Availability** | ✅ Leader Election | ✅ HA | ✅ HA | ✅ DONE |
| **Prometheus Metrics** | ✅ Basic | ✅ Advanced | ✅ Advanced | 🟡 Enhance |
| **Grafana Dashboards** | ❌ No | ✅ Yes | ✅ Yes | 🟡 Medium |
| **REST API** | ❌ No | ✅ Yes | ✅ Yes | 🔴 HIGH |
| **Web UI** | ❌ No | ✅ Yes | ✅ Yes | 🔵 Future |
| **Auto-scaling** | ⚠️ K8s native | ✅ Advanced | ✅ Advanced | 🔵 Future |

### Advanced Features

| Feature | KubeNexus (v1.0) | YuniKorn | Volcano | Priority |
|---------|------------------|----------|---------|----------|
| **Dynamic Resource Allocation** | ✅ Spark DRA | ✅ Full | ✅ Full | ✅ DONE |
| **Elastic Scheduling** | ❌ No | ✅ Yes | ✅ Yes | 🔵 Future |
| **Backfilling** | ❌ No | ✅ Yes | ✅ Yes | 🟡 Medium |
| **Cost Optimization** | ❌ No | ⚠️ Limited | ⚠️ Limited | 🔵 Future |
| **Multi-Cluster** | ❌ No | ✅ Yes | ❌ No | 🔵 Future |
| **Spot Instance Aware** | ❌ No | ⚠️ Limited | ⚠️ Limited | 🔵 Future |

---

## 🏗️ How to Make KubeNexus Topology-Aware

### What is Topology-Aware Scheduling?

**Topology-aware scheduling** means considering the physical/logical layout of your cluster when placing pods:

1. **Availability Zones/Regions**: Spread pods across zones for HA
2. **Node Topology**: Place pods on nodes in the same rack/datacenter for low latency
3. **GPU Topology**: Place GPU workloads on nodes with specific GPU types/counts
4. **NUMA Topology**: Optimize memory access patterns
5. **Network Topology**: Minimize network hops between communicating pods

### Implementation Plan

#### Phase 1: Zone/Region Awareness (HIGH PRIORITY)

**What it does**: Schedule pod groups across multiple availability zones for HA

**How to implement**:
```go
// pkg/topology/zone.go
package topology

type ZonePlugin struct {
    // Implements Score plugin
}

func (z *ZonePlugin) Score(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
    node := getNode(nodeName)
    
    // Get pod group
    podGroupName, _, _ := utils.GetPodGroupLabels(pod)
    if podGroupName == "" {
        return 0, nil // Not a gang, use default scoring
    }
    
    // Count pods in each zone
    zoneCounts := countPodsPerZone(podGroupName, pod.Namespace)
    currentZone := node.Labels["topology.kubernetes.io/zone"]
    
    // Score: Prefer zones with fewer pods (spread pods)
    score := 100 - (zoneCounts[currentZone] * 10)
    return score, nil
}
```

**Annotations**:
```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    topology.kubenexus.io/spread-zones: "true"  # Enable zone spreading
    topology.kubenexus.io/min-zones: "3"        # Require at least 3 zones
```

#### Phase 2: GPU Scheduling (MEDIUM PRIORITY)

**What it does**: Schedule ML workloads on nodes with specific GPU types/counts

**How to implement**:
```go
// pkg/topology/gpu.go
package topology

func (g *GPUPlugin) Filter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeInfo *nodeinfo.NodeInfo) *framework.Status {
    // Check GPU requirements
    gpuType := pod.Annotations["gpu.kubenexus.io/type"]  // e.g., "nvidia-a100"
    gpuCount := getGPURequest(pod)
    
    node := nodeInfo.Node()
    nodeGPUType := node.Labels["nvidia.com/gpu.product"]
    nodeGPUCount := getAvailableGPUs(node)
    
    // Filter out nodes without matching GPU type or insufficient GPUs
    if gpuType != "" && nodeGPUType != gpuType {
        return framework.NewStatus(framework.Unschedulable, "GPU type mismatch")
    }
    
    if nodeGPUCount < gpuCount {
        return framework.NewStatus(framework.Unschedulable, "Insufficient GPUs")
    }
    
    return framework.NewStatus(framework.Success, "")
}

func (g *GPUPlugin) Score(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
    // Score: Prefer nodes with exact GPU count needed (pack efficiently)
    node := getNode(nodeName)
    needed := getGPURequest(pod)
    available := getAvailableGPUs(node)
    
    if available == needed {
        return 100, nil  // Perfect fit
    } else if available > needed {
        return 100 - (available - needed) * 10, nil  // Penalize waste
    }
    
    return 0, nil
}
```

#### Phase 3: Rack/Datacenter Topology (MEDIUM PRIORITY)

**What it does**: Minimize latency by co-locating communicating pods

**How to implement**:
```go
// pkg/topology/rack.go
package topology

func (r *RackPlugin) Score(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
    // Get affinity preference
    affinityMode := pod.Annotations["topology.kubenexus.io/affinity"]  // "same-rack" or "spread-rack"
    
    node := getNode(nodeName)
    rack := node.Labels["topology.kubernetes.io/rack"]
    
    podGroupName, _, _ := utils.GetPodGroupLabels(pod)
    rackCounts := countPodsPerRack(podGroupName, pod.Namespace)
    
    if affinityMode == "same-rack" {
        // Prefer racks with more pods (co-locate)
        return int64(rackCounts[rack] * 10), nil
    } else {
        // Prefer racks with fewer pods (spread)
        return 100 - int64(rackCounts[rack] * 10), nil
    }
}
```

---

## 🚀 What's Missing? Priority Roadmap

### 🔴 **HIGH PRIORITY** (Next 3-6 months)

#### 1. **Preemption** (CRITICAL)
**Why**: Handle resource contention, allow high-priority jobs to preempt low-priority ones
**Effort**: Medium
**Impact**: High

```go
// Implement PreemptPlugin
type PreemptPlugin struct{}

func (p *PreemptPlugin) SelectVictimsOnNode(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) ([]*v1.Pod, *framework.Status) {
    // Find lowest priority pods that can be evicted
    // to make room for higher priority pod
}
```

#### 2. **Queue Management** (CRITICAL)
**Why**: Multi-tenancy, resource isolation, fair sharing
**Effort**: High
**Impact**: High

```go
// Introduce Queue CRD
type Queue struct {
    Name string
    Capacity ResourceList  // Max resources
    Guaranteed ResourceList  // Min resources
    Weight int  // For fair sharing
}
```

#### 3. **Topology-Aware Scheduling** (CRITICAL)
**Why**: HA, latency optimization, GPU scheduling
**Effort**: Medium
**Impact**: High
**Details**: See Phase 1-3 above

#### 4. **REST API** (HIGH)
**Why**: Programmatic access, debugging, observability
**Effort**: Medium
**Impact**: Medium

```go
// Expose REST API for:
// - List pod groups
// - Queue status
// - Metrics
// - Manual intervention (force schedule, evict, etc.)
```

### 🟡 **MEDIUM PRIORITY** (6-12 months)

#### 5. **Enhanced Bin Packing**
**Why**: Better resource utilization
**Effort**: Medium
**Impact**: Medium

#### 6. **Backfilling**
**Why**: Fill gaps with small jobs while large jobs wait
**Effort**: Medium
**Impact**: Medium

#### 7. **Advanced Metrics & Grafana Dashboards**
**Why**: Better observability
**Effort**: Low
**Impact**: Medium

#### 8. **Job Queue System**
**Why**: Better batch job management
**Effort**: High
**Impact**: High

### 🔵 **FUTURE** (12+ months)

- Fair Sharing
- Elastic Scheduling
- Cost Optimization
- Multi-Cluster Scheduling
- Web UI
- Auto-scaling Integration

---

## 🎯 YuniKorn vs Volcano: Which is Better?

### YuniKorn Strengths
✅ **Apache Project**: Strong governance, community
✅ **Multi-Cluster**: Supports scheduling across clusters
✅ **Mature**: Used in production by Alibaba, LinkedIn
✅ **Queue Management**: Best-in-class queue hierarchies
✅ **REST API**: Comprehensive

### Volcano Strengths
✅ **CNCF Project**: Strong Kubernetes community ties
✅ **Job Management**: Rich job queue and lifecycle management
✅ **HPC Focus**: Better for scientific computing
✅ **Plugin Ecosystem**: More scheduling plugins

### Which is Better?
**It depends**:
- **For Cloud-Native Batch (Spark, ML)**: YuniKorn
- **For HPC/Scientific Computing**: Volcano
- **For Lightweight, Simple Gang Scheduling**: **KubeNexus** 😊

---

## 💡 KubeNexus Positioning

### When to Choose KubeNexus
✅ You need gang scheduling without the complexity
✅ You want minimal resource overhead
✅ You primarily run Spark/ML batch jobs
✅ You don't need advanced multi-tenancy (or use K8s namespaces)
✅ You value simplicity and maintainability

### When to Choose YuniKorn
✅ You need complex queue hierarchies
✅ You need multi-cluster scheduling
✅ You have strict multi-tenancy requirements
✅ You're willing to manage a heavier system

### When to Choose Volcano
✅ You run HPC/MPI workloads
✅ You need rich job lifecycle management
✅ You need advanced job dependencies

---

## 📈 Next Steps for KubeNexus

### Immediate Actions (This Week)
1. ✅ Update dependencies to K8s 1.30
2. ⬜ Add topology awareness plugin (Phase 1: Zones)
3. ⬜ Add preemption support
4. ⬜ Create queue management design doc

### Short Term (1-3 Months)
1. Implement queue CRD
2. Add GPU scheduling support
3. Build REST API
4. Create Grafana dashboards

### Medium Term (3-6 Months)
1. Add fair sharing
2. Implement backfilling
3. Add rack/datacenter topology
4. Performance benchmarks vs YuniKorn/Volcano

---

## 📚 References

- [YuniKorn Architecture](https://yunikorn.apache.org/docs/)
- [Volcano Architecture](https://volcano.sh/en/docs/)
- [Kubernetes Scheduler Framework](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/)
- [Topology-Aware Scheduling](https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/)

