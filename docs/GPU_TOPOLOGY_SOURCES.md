# GPU Topology Information Sources for KubeNexus

**Date:** February 2026  
**Status:** Architecture Decision & Implementation Roadmap

---

## The Four Missing Pieces

KubeNexus NUMA plugin documentation mentions these capabilities, but they're **not yet implemented**:

1. ❌ **GPU-to-NUMA affinity** - Which GPU is on which NUMA node?
2. ❌ **NVLink topology** - Which GPUs have direct NVLink connections?
3. ❌ **PCIe bus locality** - PCIe switch topology and bandwidth?
4. ⚠️ **VRAM-aware NUMA** - Combine VRAM + NUMA decisions?

**This document defines WHERE this information should come from.**

---

## TL;DR: DRA is the Answer

**Recommended Source:** DRA (Dynamic Resource Allocation) `ResourceSlice` API

**Why:**
- ✅ Standard Kubernetes API (v1, stable in K8s 1.35+)
- ✅ Per-device granularity (not node-level)
- ✅ Auto-populated by DRA drivers (no manual labeling)
- ✅ Native support for arbitrary attributes (topology, capabilities)
- ✅ Dynamic updates (live topology changes)

**Alternative (Current):** Manual node labels - This is a **temporary workaround**, not the long-term solution.

---

## Architecture: How Information Flows

```
┌─────────────────────────────────────────────────────────────────┐
│  Physical Hardware (GPU Nodes)                                  │
│  - NUMA topology (/sys/devices/system/node/)                   │
│  - GPU devices (/dev/nvidia0, /dev/dri/card0)                  │
│  - PCIe topology (/sys/bus/pci/devices/)                       │
│  - NVLink topology (nvidia-smi topo -m)                        │
└────────────────────┬────────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────────┐
│  DRA Driver (nvidia-dra-driver, amd-dra-driver)                │
│  - Discovers GPU devices via NVML/ROCm                         │
│  - Reads topology from sysfs + vendor APIs                     │
│  - Populates ResourceSlice with device attributes              │
└────────────────────┬────────────────────────────────────────────┘
                     │ Creates/Updates
                     ▼
┌─────────────────────────────────────────────────────────────────┐
│  Kubernetes API: ResourceSlice (resource.k8s.io/v1)            │
│                                                                 │
│  spec:                                                          │
│    nodeName: gpu-node-1                                        │
│    driver: gpu.nvidia.com                                      │
│    devices:                                                     │
│    - name: gpu-0                                               │
│      attributes:                                                │
│        numa-node: {int: "0"}          # GPU on NUMA 0         │
│        nvlink-peers: {string: "1,2,3"} # Connected to GPU 1-3│
│        pcie-bus-id: {string: "0000:17:00.0"}                  │
│        pcie-switch: {string: "pex8747-0"}                     │
│        nvlink-domain: {int: "0"}       # NVLink island ID     │
│      capacity:                                                  │
│        memory: {value: "80Gi"}         # Already supported    │
└────────────────────┬────────────────────────────────────────────┘
                     │ Read via K8s API
                     ▼
┌─────────────────────────────────────────────────────────────────┐
│  KubeNexus Scheduler Plugins                                   │
│                                                                 │
│  VRAMScheduler:                                                │
│    ✅ Already reads ResourceSlice for VRAM capacity           │
│    → Extend to read numa-node attribute                       │
│                                                                 │
│  NUMATopology:                                                 │
│    → Read numa-node attribute per GPU                         │
│    → Prefer nodes where pod's GPUs fit in single NUMA         │
│                                                                 │
│  GPUTopology (NEW):                                            │
│    → Read nvlink-peers for NVLink island detection            │
│    → Read pcie-switch for PCIe locality                       │
│    → Score nodes by GPU interconnect quality                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## DRA ResourceSlice: The Full Picture

### Current Support (K8s 1.35)

```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceSlice
metadata:
  name: gpu-node-1-gpus
  ownerReferences:
  - apiVersion: v1
    kind: Node
    name: gpu-node-1
spec:
  nodeName: gpu-node-1
  driver: gpu.nvidia.com
  pool:
    name: gpu-pool
    resourceSliceCount: 1
  devices:
  - name: gpu-0
    # Arbitrary key-value attributes - THIS IS THE KEY!
    attributes:
      model: {string: "H100"}
      architecture: {string: "hopper"}
      compute-capability: {string: "9.0"}
      cuda-cores: {int: "16896"}
      
      # ** TOPOLOGY ATTRIBUTES (DRA driver must add these) **
      numa-node: {int: "0"}                    # Which NUMA node?
      pcie-bus-id: {string: "0000:17:00.0"}   # PCIe address
      pcie-link-speed: {string: "32GT/s"}     # PCIe Gen5 x16
      pcie-link-width: {int: "16"}            # 16 lanes
      pcie-switch-id: {string: "pex8747-0"}   # PCIe switch identifier
      
      # NVLink topology (NVIDIA-specific)
      nvlink-version: {int: "4"}              # NVLink 4.0
      nvlink-peers: {string: "gpu-1,gpu-2,gpu-3"}  # Direct connections
      nvlink-domain: {int: "0"}               # NVLink island/domain ID
      
      # AMD-specific (for MI300X)
      xgmi-hops: {string: "0:0,1:1,2:1,3:2"}  # XGMI distance to other GPUs
      
    # Resource capacity (already standard)
    capacity:
      memory: {value: "80Gi"}                 # Already used by VRAMScheduler
```

### What DRA Provides Out-of-Box

✅ **Already Available:**
- Device name/ID
- Resource capacity (memory, compute)
- Arbitrary string/int/bool attributes
- Per-device granularity

❌ **Not Provided (Driver Must Add):**
- Topology attributes (numa-node, pcie-bus-id, etc.)
- NVLink/XGMI connectivity
- PCIe switch mapping

---

## Implementation Phases

### Phase 1: DRA Driver Enhancement (External)

**Who implements:** NVIDIA/AMD/Intel DRA driver maintainers (or custom fork)

**What:** Enhance `nvidia-dra-driver` to populate topology attributes

**Example Enhancement:**
```go
// In nvidia-dra-driver device discovery
func discoverDevice(gpuIndex int) Device {
    device := Device{
        Name: fmt.Sprintf("gpu-%d", gpuIndex),
        Attributes: map[string]DeviceAttribute{
            "model": {String: &DeviceAttributeString{String_: getGPUModel(gpuIndex)}},
            
            // Add topology attributes
            "numa-node": {
                Int: &DeviceAttributeInt{
                    Int_: int64(getNUMANode(gpuIndex)), // From NVML
                },
            },
            "nvlink-peers": {
                String: &DeviceAttributeString{
                    String_: strings.Join(getNVLinkPeers(gpuIndex), ","),
                },
            },
            "pcie-bus-id": {
                String: &DeviceAttributeString{
                    String_: getPCIeBusID(gpuIndex),
                },
            },
        },
        Capacity: map[DeviceCapacityName]DeviceCapacity{
            "memory": {Value: getVRAMCapacity(gpuIndex)},
        },
    }
    return device
}

func getNUMANode(gpuIndex int) int {
    // Query via NVML
    device, _ := nvml.DeviceGetHandleByIndex(gpuIndex)
    numaNode, _ := device.GetNUMANode()
    return numaNode
}

func getNVLinkPeers(gpuIndex int) []string {
    // Parse nvidia-smi topo output
    // Or use NVML NvLink API
    device, _ := nvml.DeviceGetHandleByIndex(gpuIndex)
    var peers []string
    for i := 0; i < 6; i++ { // H100 has 18 NVLink, check each
        state, _ := device.GetNvLinkState(i)
        if state == nvml.FEATURE_ENABLED {
            remoteDevice, _ := device.GetNvLinkRemotePciInfo(i)
            peers = append(peers, fmt.Sprintf("gpu-%d", remoteDevice.BusId))
        }
    }
    return peers
}
```

**Status:** 
- 🔴 Not implemented in official nvidia-dra-driver
- 🟡 Can be implemented in custom fork
- ✅ DRA API supports it (attributes are arbitrary)

**Timeline:** 
- Upstream request: Q2 2026
- Custom fork: 2-3 weeks development

---

### Phase 2: Scheduler Plugin Updates

#### 2.1 VRAMScheduler Enhancement

**Current:** Reads `capacity.memory` from ResourceSlice ✅  
**Enhancement:** Also read `attributes.numa-node`

```go
// pkg/plugins/vramscheduler/vramscheduler.go
func (v *VRAMScheduler) getNodeGPUVRAM(ctx context.Context, node *v1.Node) (int64, int) {
    // ... existing ResourceSlice query code ...
    
    for _, device := range slice.Spec.Devices {
        // Existing: Get VRAM
        vramBytes := device.Capacity["memory"].Value.Value()
        
        // NEW: Get NUMA node
        numaNode := -1
        if numaAttr, exists := device.Attributes["numa-node"]; exists {
            if numaAttr.Int != nil {
                numaNode = int(numaAttr.Int.Int_)
            }
        }
        
        // Store GPU -> NUMA mapping
        v.gpuNUMAMap[device.Name] = numaNode
        
        klog.V(5).InfoS("Discovered GPU topology",
            "gpu", device.Name,
            "vram", formatBytes(vramBytes),
            "numaNode", numaNode)
    }
}
```

#### 2.2 NUMATopology Integration

**Enhancement:** Read GPU-NUMA mapping from DRA

```go
// pkg/plugins/numatopology/numatopology.go
func (n *NUMATopology) Score(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfo framework.NodeInfo) (int64, *framework.Status) {
    // ... existing NUMA scoring ...
    
    // NEW: If pod requests GPUs, check GPU-NUMA locality
    gpuCount := getGPURequest(pod)
    if gpuCount > 0 {
        // Query ResourceSlices for this node
        gpuDevices := n.getGPUDevicesFromDRA(ctx, nodeInfo.Node())
        
        // Check if requested GPUs can all be on same NUMA node
        numaGPUMap := make(map[int][]string)
        for _, gpu := range gpuDevices {
            if numaAttr, exists := gpu.Attributes["numa-node"]; exists {
                numaNode := int(numaAttr.Int.Int_)
                numaGPUMap[numaNode] = append(numaGPUMap[numaNode], gpu.Name)
            }
        }
        
        // Boost score if enough GPUs on single NUMA
        for numaID, gpus := range numaGPUMap {
            if len(gpus) >= gpuCount {
                klog.V(4).InfoS("Node has sufficient GPUs on single NUMA",
                    "node", nodeInfo.Node().Name,
                    "numaNode", numaID,
                    "availableGPUs", len(gpus),
                    "requiredGPUs", gpuCount)
                score += 30 // Boost for GPU-NUMA locality
                break
            }
        }
    }
    
    return score, framework.NewStatus(framework.Success)
}
```

#### 2.3 New Plugin: GPUTopology

**Purpose:** Score based on NVLink/PCIe topology

```go
// pkg/plugins/gputopology/gputopology.go (NEW)
package gputopology

type GPUTopology struct {
    handle framework.Handle
    vramScheduler *vramscheduler.VRAMScheduler
}

func (gt *GPUTopology) Score(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfo framework.NodeInfo) (int64, *framework.Status) {
    gpuCount := getGPURequest(pod)
    if gpuCount <= 1 {
        return framework.MaxNodeScore / 2, framework.NewStatus(framework.Success) // Neutral
    }
    
    // Get GPU devices from DRA
    devices := gt.getGPUDevicesFromDRA(ctx, nodeInfo.Node())
    
    // Detect NVLink islands (groups of GPUs with full NVLink connectivity)
    islands := gt.detectNVLinkIslands(devices)
    
    // Score: High if node has NVLink island >= pod's GPU request
    for _, island := range islands {
        if len(island.GPUs) >= gpuCount {
            klog.V(4).InfoS("Found NVLink island for pod",
                "node", nodeInfo.Node().Name,
                "islandSize", len(island.GPUs),
                "requiredGPUs", gpuCount)
            return 100, framework.NewStatus(framework.Success) // Perfect score
        }
    }
    
    // Fallback: Check PCIe switch locality
    pcieSwitchGroups := gt.groupByPCIeSwitch(devices)
    for _, group := range pcieSwitchGroups {
        if len(group.GPUs) >= gpuCount {
            klog.V(4).InfoS("Found PCIe switch group for pod",
                "node", nodeInfo.Node().Name,
                "groupSize", len(group.GPUs))
            return 70, framework.NewStatus(framework.Success) // Good score
        }
    }
    
    // Poor score: GPUs are spread across switches
    return 30, framework.NewStatus(framework.Success)
}

func (gt *GPUTopology) detectNVLinkIslands(devices []Device) []NVLinkIsland {
    // Parse nvlink-peers attributes to build connectivity graph
    graph := make(map[string][]string)
    for _, device := range devices {
        if peersAttr, exists := device.Attributes["nvlink-peers"]; exists {
            peers := strings.Split(peersAttr.String.String_, ",")
            graph[device.Name] = peers
        }
    }
    
    // Find fully-connected subgraphs (cliques)
    return findCliques(graph)
}
```

---

### Phase 3: Fallback & Migration

**Dual-mode support:**
1. Try DRA ResourceSlices first (preferred)
2. Fall back to node labels if DRA unavailable
3. Log warnings if topology info missing

```go
func (n *NUMATopology) getGPUNUMAMapping(ctx context.Context, node *v1.Node) map[string]int {
    // Try DRA first
    draMapping := n.getGPUNUMAFromDRA(ctx, node)
    if len(draMapping) > 0 {
        klog.V(4).InfoS("Using DRA for GPU-NUMA mapping", "node", node.Name)
        return draMapping
    }
    
    // Fallback to node labels
    labelMapping := n.getGPUNUMAFromLabels(node)
    if len(labelMapping) > 0 {
        klog.V(3).InfoS("Falling back to node labels for GPU-NUMA mapping (DRA unavailable)", "node", node.Name)
        return labelMapping
    }
    
    klog.Warningf("No GPU-NUMA mapping available for node %s (neither DRA nor labels)", node.Name)
    return make(map[string]int)
}
```

---

## Migration Path

### Today (Feb 2026)
- ❌ Manual node labels only
- ⚠️ NUMA plugin ignores GPU topology
- ✅ VRAMScheduler reads DRA for VRAM only

### Q2 2026 (Phase 1)
- 🟡 Fork nvidia-dra-driver, add topology attributes
- 🟡 Test on internal clusters
- 🟡 Upstream PR to nvidia-dra-driver

### Q3 2026 (Phase 2)
- ✅ Update VRAMScheduler to read numa-node attribute
- ✅ Update NUMATopology to use GPU-NUMA mapping from DRA
- ✅ Implement GPUTopology plugin for NVLink/PCIe scoring

### Q4 2026 (Phase 3)
- ✅ Deprecate node label approach
- ✅ Documentation updates
- ✅ Production rollout

---

## Summary

**Question:** Where should GPU topology information come from?

**Answer:** **DRA ResourceSlice attributes** (populated by enhanced DRA drivers)

**Why:**
- Standard K8s API
- Per-device granularity
- Auto-discovered, not manual
- Extensible (arbitrary attributes)
- Works with existing VRAMScheduler DRA integration

**Current Status:** VRAMScheduler already uses DRA for VRAM ✅  
**Next Step:** Enhance DRA drivers to add topology attributes  
**Timeline:** 6-9 months for full implementation

**Interim Solution:** Manual node labels (current approach)  
**Long-term Solution:** DRA attributes (this document's recommendation)
