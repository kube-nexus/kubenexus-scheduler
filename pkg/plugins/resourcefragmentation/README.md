# Resource Fragmentation Score Plugin

## Overview

The **ResourceFragmentationScore** plugin implements proactive bin-packing to prevent resource fragmentation in GPU clusters. This is KubeNexus's first "killer feature" that beats both Volcano and the native Kubernetes scheduler on intelligence.

## The Problem

Native Kubernetes `NodeResourcesFit` only tries to "fill" nodes using simple bin-packing—it doesn't understand the value of different resource topologies. Example scenario:

**Cluster State:**
- Node A: 8x H100 GPUs, **pristine** (all connected via NVSwitch)
- Node B: 8x H100 GPUs, 6 allocated, 2 free
- Node C: 4x L40 GPUs, pristine

**Request:** 1 GPU pod

**Native Scheduler Decision:**
- Picks Node B (most allocated = standard bin-packing)
- ✅ Correct choice

**Request:** 1 GPU pod (when Node B is full)

**Native Scheduler Decision:**
- Picks Node A (only one available)
- ❌ **Fragments the pristine 8-GPU island!**
- Result: Large 8-GPU distributed training jobs can no longer use Node A

## The KubeNexus Solution

KubeNexus implements **"Island Preservation"** scoring:

1. **Detects high-value resource "islands"** (e.g., 8 GPUs connected via NVLink)
2. **Gives massive penalties** to placements that would fragment pristine islands
3. **Prefers nodes** where the pod "completes" a partially-filled island
4. **Keeps premium topology ready** for large distributed jobs

## How It Works

### GPU Island Detection

The plugin detects GPU topology using node labels:

```yaml
metadata:
  labels:
    gpu.kubenexus.io/topology: "nvswitch"      # nvswitch | nvlink | pcie
    gpu.kubenexus.io/count: "8"                # Total GPU count
    gpu.kubenexus.io/model: "H100"             # GPU model
    gpu.kubenexus.io/is-pristine: "true"       # No allocations (optional)
    gpu.kubenexus.io/interconnect: "nvlink-bandwidth-900GBps"
```

### Island Quality Scores

Different topologies have different values:

| Topology | Quality Score | Examples | Use Case |
|----------|--------------|----------|----------|
| **NVSwitch** | 100 | H100, H200 (8-GPU full mesh) | Large-scale distributed training |
| **NVLink** | 80 | A100 (2-8 GPU pairs) | Multi-GPU training |
| **PCIe** | 50 | L40, T4 (PCIe Gen4/5) | Inference, small training |
| **Unknown** | 30 | No labels | Generic GPU workloads |

### Scoring Logic

```
IF pristine_island AND island_size >= 4 AND request_size <= 2:
    RETURN 0  // DON'T fragment pristine large islands!

ELSE IF available_gpus == requested_gpus:
    RETURN 90  // Perfect fit bonus

ELSE IF island_partially_filled:
    RETURN 100 - (available - requested)  // Completion bonus

ELSE:
    RETURN (allocated / total) * 100  // Standard bin-packing
```

## Configuration

### Enable in Scheduler Config

Add to `config/config.yaml`:

```yaml
apiVersion: kubescheduler.config.k8s.io/v1
kind: KubeSchedulerConfiguration
profiles:
  - schedulerName: kubenexus-scheduler
    plugins:
      score:
        enabled:
          - name: ResourceFragmentationScore
            weight: 5  # Adjust based on importance
          - name: NUMATopology
            weight: 10
          - name: WorkloadAwareScoring
            weight: 5
```

### Weight Guidelines

- **Weight 1-3**: Gentle preference, easily overridden by other plugins
- **Weight 5-7**: Strong preference (recommended)
- **Weight 10+**: Very strong preference, dominates other factors

### Label Your Nodes

**Automatic (Recommended):**
Use GPU Feature Discovery (GFD) + Node Feature Discovery (NFD):

```bash
# Install NFD
kubectl apply -f https://kubernetes-sigs.github.io/node-feature-discovery/master/nfd-daemonset-combined.yaml

# Install GFD
kubectl apply -f https://raw.githubusercontent.com/NVIDIA/gpu-feature-discovery/main/deployments/static/gpu-feature-discovery-daemonset.yaml
```

**Manual:**
```bash
kubectl label node gpu-node-01 \
  gpu.kubenexus.io/topology=nvswitch \
  gpu.kubenexus.io/count=8 \
  gpu.kubenexus.io/model=H100 \
  gpu.kubenexus.io/is-pristine=true
```

## Example Scenarios

### Scenario 1: Preserve Pristine 8-GPU Island

**Setup:**
- Node A: 8x H100 (pristine)
- Node B: 4x L40 (pristine)

**Request:** 1 GPU

**Native Scheduler:** Picks Node A (first available)  
**KubeNexus:** Picks Node B (preserves H100 island for large jobs)

### Scenario 2: Perfect Fit

**Setup:**
- Node A: 8x A100 (6 allocated, 2 free)
- Node B: 8x A100 (pristine)

**Request:** 2 GPUs

**Native Scheduler:** Might pick either  
**KubeNexus:** Picks Node A (perfect fit, completes the node)

### Scenario 3: Large Distributed Training

**Setup:**
- Node A: 8x H100 (pristine)
- Node B: 8x H100 (1 allocated, 7 free)

**Request:** 8 GPUs

**Native Scheduler:** Rejects Node B (insufficient), picks Node A  
**KubeNexus:** Same (large requests CAN use pristine islands)

## Comparison with Native Scheduler

| Feature | Native K8s | KubeNexus |
|---------|-----------|-----------|
| **Basic Bin-packing** | ✅ Yes | ✅ Yes |
| **Topology Awareness** | ❌ No | ✅ Yes (NVSwitch, NVLink, PCIe) |
| **Island Preservation** | ❌ No | ✅ Yes |
| **Fragmentation Prevention** | ❌ No | ✅ Yes |
| **Value-Based Scoring** | ❌ No | ✅ Yes (island quality) |
| **Perfect Fit Bonus** | ⚠️ Implicit | ✅ Explicit |

## Integration with Other Plugins

### Works With

- **✅ NUMATopology**: Fragmentation scoring AFTER NUMA filtering
- **✅ WorkloadAwareScoring**: Combined scoring (batch = bin-pack + preserve islands)
- **✅ Coscheduling**: Gang members get consistent fragmentation treatment
- **✅ Kueue**: Can watch Kueue queues for upcoming demand (future)

### Execution Order

```
1. Filter phase: NUMATopology filters out incompatible nodes
2. Score phase:
   a. ResourceFragmentationScore (island preservation)
   b. NUMATopology (NUMA alignment)
   c. WorkloadAwareScoring (workload type)
3. Combined score: Weighted sum of all plugins
```

## Future Enhancements

### Phase 2: Kueue Integration (Planned)

Watch Kueue's LocalQueue to see **upcoming demand**:

```go
// If Kueue queue has pending 8-GPU job, proactively preserve 8-GPU islands
func (rf *ResourceFragmentationScore) watchKueueQueues() {
    // Watch LocalQueue.status.pendingWorkloads
    // Adjust island preservation based on queued jobs
}
```

### Phase 3: Dynamic Island Updates

Monitor GPU allocations and update `is-pristine` labels in real-time:

```go
// Controller that watches pod allocations and updates node labels
func (c *IslandController) updatePristineLabels() {
    // If all GPUs become free, mark node as pristine again
}
```

### Phase 4: Multi-Node Gang Topology

Extend to consider **cross-node** topology for distributed training:

```go
// For 16-GPU gang (2x 8-GPU nodes), prefer nodes on same rack/switch
func (rf *ResourceFragmentationScore) scoreGangTopology(pod, nodes) {
    // Calculate network distance between gang members
}
```

## Testing

Run tests:

```bash
go test ./pkg/plugins/resourcefragmentation/... -v
```

Expected output:
```
=== RUN   TestPristineIslandPreservation
--- PASS: TestPristineIslandPreservation (0.00s)
=== RUN   TestPerfectFitBonus
--- PASS: TestPerfectFitBonus (0.00s)
=== RUN   TestLargeRequestOnPristine
--- PASS: TestLargeRequestOnPristine (0.00s)
...
PASS
```

## Troubleshooting

### Issue: All nodes getting same score

**Cause:** Nodes not labeled with GPU topology  
**Solution:** Add labels using NFD/GFD or manually

### Issue: Small pods not scheduling

**Cause:** Weight too high, all large islands penalized  
**Solution:** Reduce weight or add more small-GPU nodes

### Issue: Pristine label not updating

**Cause:** No controller updating labels  
**Solution:** Manually update or implement dynamic controller (Phase 3)

## Contributing

This plugin is part of KubeNexus's competitive advantage. Contributions welcome:

1. Additional topology types (ROCm, TPU)
2. Better island detection algorithms
3. Integration with hardware monitoring
4. Performance optimizations

## References

- [NUMA Scheduling Guide](../NUMA_SCHEDULING_GUIDE.md)
- [Design Decisions](../DESIGN_DECISIONS.md)
- [Competitive Advantage](../COMPETITIVE_ADVANTAGE.md)
- [K8s NodeResourcesFit Plugin](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/)
