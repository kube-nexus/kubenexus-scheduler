# KubeNexus Competitive Advantage: Architecture to Beat Volcano & Native Stack

**Status**: Strategic Roadmap  
**Date**: February 2026  
**Goal**: Position KubeNexus as the intelligent, simple scheduler that beats Volcano on complexity while exceeding native K8s + Kueue on intelligence.

---

## The Context Gap

The native Kubernetes stack (Kueue + v1.35 scheduler plugins) is **stateless and reactive**—it examines a pod, looks at a node, and makes a Yes/No decision. It has no memory of past decisions, no awareness of upcoming demand, and no understanding of workload economics.

To provide more value than both Volcano and the native stack, **KubeNexus must be predictive and tenant-aware**.

---

## Competitive Positioning

| Dimension | Native K8s + Kueue | Volcano | **KubeNexus** |
|-----------|-------------------|---------|---------------|
| **Setup Complexity** | Medium (CRDs required) | High (Custom scheduler + APIs) | **Low (Plugin-based)** |
| **Operational Overhead** | Medium | Very High | **Minimal** |
| **Gang Scheduling** | Via adapters | Native but complex | **Native + Simple** |
| **Topology Awareness** | Basic (NodeResourceTopology) | None | **Full NUMA + PCIe** |
| **Predictive Placement** | ❌ Reactive only | ❌ Reactive | **✅ Proactive** |
| **Economic Efficiency** | ❌ No hardware tiering | ❌ No | **✅ Tenant-Hardware matching** |
| **Cluster-Wide Topology** | ❌ Per-node only | ❌ No | **✅ Network fabric aware** |
| **Kueue Integration** | Native | None | **✅ Direct API sync** |

---

## The 3 Architectural "Killers"

These three features will position KubeNexus ahead of both the native stack AND Volcano:

### 1. 🎯 De-fragmentation Score (Proactive Bin-packing)

**The Problem**:  
Native `NodeResourcesFit` only tries to fill nodes—it doesn't care which node it ruins. If a node has a perfectly clean 8-GPU NVLink island, the native scheduler will happily place a 1-GPU pod there if it's the "most allocated" node.

**The KubeNexus Solution**:  
Implement a **"Slot Preservation" algorithm** that gives massive penalties to placements that would fragment high-value resource islands.

#### Implementation Strategy

**Plugin**: New `ResourceFragmentationScore` plugin  
**Location**: `pkg/plugins/resourcefragmentation/`  
**Integration Point**: Score phase (runs AFTER Filter phase)

**Core Logic**:
```go
type GPUIsland struct {
    NodeName    string
    StartGPUID  int
    GPUCount    int
    NVLinkTier  string  // "NVSwitch", "NVLink", "PCIe"
    IsContiguous bool
    IsPristine  bool   // No allocations yet
}

func (rf *ResourceFragmentation) Score(ctx, state, pod, nodeInfo) (int64, *Status) {
    // 1. Detect GPU topology on node
    islands := rf.detectGPUIslands(nodeInfo)
    
    // 2. Calculate request size
    requestedGPUs := getGPURequest(pod)
    
    // 3. If pod requests <= 2 GPUs AND node has pristine 8-GPU island
    for _, island := range islands {
        if island.IsPristine && island.GPUCount >= 8 && requestedGPUs <= 2 {
            // MASSIVE penalty: Don't ruin the big island!
            return 0, framework.NewStatus(framework.Success)
        }
    }
    
    // 4. Prefer nodes where pod "completes" a partially filled island
    // or uses a small, already-fragmented node
    score := rf.calculateIslandFitScore(pod, islands)
    return score, framework.NewStatus(framework.Success)
}
```

**Value Add**:  
- Keeps "Pristine Islands" ready for big distributed jobs that Kueue is about to admit
- Native scheduler would have "peppered" those nodes with tiny pods
- Result: Better GPU utilization for multi-node training jobs

**Status**: 🟡 Not implemented yet  
**Priority**: **HIGH** - This is the biggest differentiator vs native stack

---

### 2. 💰 Tenant-Hardware Affinity Scoring (Economic Efficiency)

**The Problem**:  
In a heterogeneous cluster (H100s, A100s, L40s), the native scheduler treats "a GPU is a GPU." If a low-priority tenant's pod can fit on either an old A100 node OR a new H100 node, it might pick the H100.

**The KubeNexus Solution**:  
Implement **"Tiered-Quota Scoring"** that syncs with Kueue to see tenant priority and heavily penalizes premium hardware placement for low-priority tenants.

#### Implementation Strategy

**Plugin**: New `TenantHardwareAffinityScore` plugin  
**Location**: `pkg/plugins/tenanthardware/`  
**Integration Point**: Score phase + Kueue API watcher

**Core Logic**:
```go
type HardwareTier struct {
    TierName   string  // "premium", "standard", "economy"
    GPUModels  []string // ["H100", "A100", "L40"]
    CostFactor float64  // Relative cost multiplier
}

type TenantPriority struct {
    Namespace      string
    PriorityClass  string
    QueueName      string  // From Kueue LocalQueue
    AllowedTiers   []string // ["premium", "standard"]
}

func (tha *TenantHardwareAffinity) Score(ctx, state, pod, nodeInfo) (int64, *Status) {
    // 1. Get tenant priority from Kueue
    tenant := tha.getTenantPriorityFromKueue(pod)
    
    // 2. Detect hardware tier of this node
    nodeTier := tha.detectHardwareTier(nodeInfo)
    
    // 3. Match tenant to hardware
    if tenant.PriorityClass == "low" && nodeTier.TierName == "premium" {
        // Heavy penalty: Don't waste H100s on low-priority work
        return 10, framework.NewStatus(framework.Success)  // Very low score
    }
    
    // 4. Prefer matching tier
    if contains(tenant.AllowedTiers, nodeTier.TierName) {
        return 100, framework.NewStatus(framework.Success)
    }
    
    return 50, framework.NewStatus(framework.Success)
}
```

**Kueue Integration**:
```go
// Watch Kueue LocalQueue status to see upcoming demand
func (tha *TenantHardwareAffinity) watchKueueQueues() {
    informer := tha.kueueClient.KueueV1beta1().LocalQueues("").Informer()
    informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
        AddFunc: func(obj interface{}) {
            lq := obj.(*kueuev1beta1.LocalQueue)
            // Track which tenants are in which queues
            tha.tenantCache.Update(lq.Namespace, lq.Name, lq.Spec.ClusterQueue)
        },
    })
}
```

**Value Add**:  
- Ensures "Premium" hardware is physically available for "Premium" tenants/jobs when they arrive
- Native K8s doesn't have this "economic" foresight
- Result: Better ROI on expensive hardware, happier high-paying customers

**Status**: 🟡 Not implemented yet  
**Priority**: **MEDIUM** - High value in commercial/enterprise scenarios

---

### 3. 🌐 Cross-Node Interconnect Topology (Cluster-Aware NUMA)

**The Problem**:  
`NodeResourceTopology` (native K8s v1.35) only looks **inside the box**—it doesn't know if Node A and Node B are on the same Leaf Switch or across different racks.

**The KubeNexus Solution**:  
Implement a **"Network Fabric" plugin** that calculates network distance between nodes for Gang scheduling.

#### Implementation Strategy

**Plugin**: Extend existing `NUMATopology` plugin  
**Location**: `pkg/plugins/numatopology/numatopology.go`  
**Integration Point**: Score phase for Gang members

**Core Logic**:
```go
type NetworkTopology struct {
    NodeName        string
    RackID          string
    ToRSwitch       string
    SpineSwitch     string
    NetworkTier     string  // "InfiniBand", "RoCE", "10GbE"
}

func (nt *NUMATopology) ScoreForGang(ctx, state, pod, nodeInfo) (int64, *Status) {
    // 1. Check if pod is part of a Gang
    gangName, gangSize := getGangInfo(pod)
    if gangName == "" {
        return nt.Score(ctx, state, pod, nodeInfo)  // Fall back to regular scoring
    }
    
    // 2. Get already-scheduled gang members
    gangNodes := nt.getGangMemberNodes(gangName, pod.Namespace)
    
    // 3. If this is the first gang member, prefer nodes in dense racks
    if len(gangNodes) == 0 {
        score := nt.scoreDenseRackPreference(nodeInfo)
        return score, framework.NewStatus(framework.Success)
    }
    
    // 4. For subsequent members, calculate network distance
    networkScore := nt.calculateNetworkProximityScore(nodeInfo, gangNodes)
    
    // 5. Prefer nodes that share ToR switch with existing gang members
    return networkScore, framework.NewStatus(framework.Success)
}

func (nt *NUMATopology) calculateNetworkProximityScore(nodeInfo, gangNodes) int64 {
    currentTopo := nt.getNetworkTopology(nodeInfo.Node())
    
    sameToR := 0
    sameRack := 0
    
    for _, gangNode := range gangNodes {
        gangTopo := nt.getNetworkTopology(gangNode)
        
        if currentTopo.ToRSwitch == gangTopo.ToRSwitch {
            sameToR++
        } else if currentTopo.RackID == gangTopo.RackID {
            sameRack++
        }
    }
    
    // Prefer same ToR > same Rack > different rack
    score := (sameToR * 100) + (sameRack * 50)
    return int64(score)
}
```

**Node Labeling** (Required by admins):
```yaml
metadata:
  labels:
    topology.kubernetes.io/rack: "rack-1"
    topology.kubernetes.io/switch: "tor-switch-1"
    network.kubenexus.io/fabric: "infiniband"
    network.kubenexus.io/bandwidth: "200Gbps"
```

**Value Add**:  
- Solves the "InfiniBand/RoCE" bottleneck that native K8s ignored
- Effectively does "Cluster-Wide NUMA" topology awareness
- Result: Faster distributed training, better network utilization

**Status**: 🟢 Partially implemented (NUMA exists, needs Gang + Network extension)  
**Priority**: **HIGH** - Critical for large-scale distributed training

---

## How This Beats Volcano

| Volcano Weakness | KubeNexus Advantage |
|------------------|---------------------|
| **Complex Custom Scheduler** | Simple plugin-based, uses native framework |
| **Custom CRDs (VolcanoJob, Queue)** | Works with standard Pod labels + optional Kueue |
| **No Kueue integration** | Direct Kueue API integration for predictive placement |
| **No topology awareness** | Full NUMA + PCIe + Network fabric |
| **Steep learning curve** | Familiar Kubernetes patterns |

---

## Current Progress Assessment

### ✅ What KubeNexus Already Has

1. **Gang Scheduling**: Implemented via `Coscheduling` plugin ✅
2. **NUMA Topology Awareness**: Advanced NUMA plugin with GPU support ✅
3. **Workload Classification**: Automatic batch vs service detection ✅
4. **Filter vs Score**: Proper plugin architecture ✅
5. **Simple Deployment**: Plugin-based, no scheduler replacement ✅

### 🟡 What Needs Implementation (3 Killers)

1. **De-fragmentation Score**: NOT implemented yet
2. **Tenant-Hardware Affinity**: NOT implemented yet
3. **Network Fabric Topology**: Partially in NUMA, needs Gang extension

### 📋 Implementation Roadmap

#### Phase 1: Foundation (Next 2 weeks)
- [ ] Create Kueue API client/watcher infrastructure
- [ ] Add GPU island detection to existing NUMA plugin
- [ ] Create basic fragmentation scoring

#### Phase 2: Killer #1 - De-fragmentation (Weeks 3-4)
- [ ] Implement `ResourceFragmentationScore` plugin
- [ ] Add GPU topology detection (NVLink, PCIe)
- [ ] Create "Pristine Island" penalty logic
- [ ] Integration tests with multi-GPU nodes

#### Phase 3: Killer #2 - Tenant-Hardware (Weeks 5-6)
- [ ] Implement `TenantHardwareAffinityScore` plugin
- [ ] Kueue LocalQueue status watcher
- [ ] Hardware tier detection (labels + auto-detect)
- [ ] Priority class to tier mapping

#### Phase 4: Killer #3 - Network Fabric (Weeks 7-8)
- [ ] Extend NUMA plugin with Gang network awareness
- [ ] Network topology label schema
- [ ] Cross-node proximity scoring
- [ ] Multi-node Gang placement tests

#### Phase 5: Integration & Documentation (Weeks 9-10)
- [ ] End-to-end testing with Kueue
- [ ] Performance benchmarks vs native + Volcano
- [ ] Documentation and examples
- [ ] Blog post: "How KubeNexus Beats Volcano"

---

## Next Steps

### Immediate Actions (This Week)

1. **Add Kueue Client Dependency**:
   ```bash
   go get sigs.k8s.io/kueue@v0.10.0
   ```

2. **Create Plugin Skeletons**:
   - `pkg/plugins/resourcefragmentation/`
   - `pkg/plugins/tenanthardware/`

3. **Design GPU Island Detection**:
   - Read existing NUMA topology code
   - Extend to detect NVLink vs PCIe connections
   - Create island representation data structures

### Questions to Answer

1. **Do you want to start with Killer #1 (De-fragmentation)?**
   - This is the most impactful for GPU clusters
   - Can deliver quick wins vs native scheduler

2. **Should we create a Kueue integration plugin first?**
   - Shared infrastructure for Killer #2 and future features
   - Watches LocalQueue, ClusterQueue, Workload CRDs

3. **What's your cluster's network topology?**
   - Do you have InfiniBand, RoCE, or standard Ethernet?
   - Are nodes labeled with rack/switch info?
   - Will inform priority of Killer #3

---

## Conclusion

KubeNexus is **75% of the way there**. You have:
- ✅ Gang scheduling (better than native)
- ✅ NUMA awareness (better than Volcano)
- ✅ Simple deployment (better than both)

To complete the picture and **truly beat both competitors**, you need:
- 🎯 Proactive placement (De-fragmentation)
- 💰 Economic efficiency (Tenant-Hardware matching)
- 🌐 Cluster-wide topology (Network fabric)

**Would you like me to generate the Go code for the "Slot Preservation" scoring function (Killer #1) right now?** This would be the fastest way to demonstrate superiority over the native stack.

Alternatively, if you'd prefer to start with the Kueue integration infrastructure, I can create that foundation first.
