# KubeNexus NUMA Scheduling - Quick Reference

> One-page cheat sheet for NUMA-aware scheduling

---

## 🚀 Quick Start

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-ml-pod
  annotations:
    scheduling.kubenexus.io/numa-policy: "single-numa-node"
    scheduling.kubenexus.io/memory-intensive: "true"
spec:
  schedulerName: kubenexus-scheduler
  containers:
  - name: app
    resources:
      requests:
        cpu: "8"
        memory: "32Gi"
```

---

## 📋 Pod Annotations

### Basic NUMA Policy
```yaml
# Strict: MUST fit in single NUMA node (default for batch/ML)
scheduling.kubenexus.io/numa-policy: "single-numa-node"

# Best-effort: Prefer single NUMA, allow cross-NUMA if needed
scheduling.kubenexus.io/numa-policy: "best-effort"

# Disable NUMA awareness (default for services)
scheduling.kubenexus.io/numa-policy: "none"
```

### NUMA Affinity
```yaml
# Prefer NUMA nodes 0,1 (20% score boost)
scheduling.kubenexus.io/numa-affinity-node-id: "0,1"

# Avoid NUMA nodes 2,3 (filtered out)
scheduling.kubenexus.io/numa-anti-affinity-node-id: "2,3"
```

### Memory Optimization
```yaml
# Optimize memory bandwidth (auto-detected if >16GB and >4GB/core)
scheduling.kubenexus.io/memory-intensive: "true"
```

### NUMA Distance
```yaml
# Weight for inter-NUMA latency (0-100, higher = more emphasis)
scheduling.kubenexus.io/numa-distance-weight: "80"
```

### Gang Scheduling with NUMA
```yaml
# Gang group identifier
scheduling.kubenexus.io/gang-group: "ml-job-123"

# Gang NUMA spread policy
scheduling.kubenexus.io/gang-numa-spread: "packed"    # Co-locate (low latency)
scheduling.kubenexus.io/gang-numa-spread: "balanced"  # Distribute (high bandwidth)
scheduling.kubenexus.io/gang-numa-spread: "isolated"  # Dedicated NUMA per member
```

---

## 🏷️ Node Labels

### Required Labels
```bash
# Basic NUMA topology
kubectl label node worker-1 \
  numa.kubenexus.io/node-count=2 \
  numa.kubenexus.io/node-0-cpus="0-15,32-47" \
  numa.kubenexus.io/node-0-memory="68719476736" \
  numa.kubenexus.io/node-1-cpus="16-31,48-63" \
  numa.kubenexus.io/node-1-memory="68719476736"
```

### Optional Labels (Advanced Features)
```bash
# Memory bandwidth (MB/s)
numa.kubenexus.io/node-0-bandwidth="102400"
numa.kubenexus.io/node-1-bandwidth="102400"

# NUMA distance matrix (10=local, 20=remote)
numa.kubenexus.io/node-0-distance-0="10"
numa.kubenexus.io/node-0-distance-1="20"
numa.kubenexus.io/node-1-distance-0="20"
numa.kubenexus.io/node-1-distance-1="10"
```

---

## 🎯 Common Use Cases

### ML Training (Single Pod)
```yaml
annotations:
  scheduling.kubenexus.io/numa-policy: "single-numa-node"
  scheduling.kubenexus.io/memory-intensive: "true"
resources:
  requests: { cpu: "12", memory: "96Gi" }
```

### Distributed ML (Gang)
```yaml
annotations:
  scheduling.kubenexus.io/gang-group: "ml-job"
  scheduling.kubenexus.io/gang-numa-spread: "packed"
  scheduling.kubenexus.io/numa-policy: "single-numa-node"
  scheduling.kubenexus.io/memory-intensive: "true"
resources:
  requests: { cpu: "8", memory: "32Gi" }
```

### HPC Simulation (Isolated)
```yaml
annotations:
  scheduling.kubenexus.io/gang-group: "hpc-sim"
  scheduling.kubenexus.io/gang-numa-spread: "isolated"
  scheduling.kubenexus.io/numa-policy: "single-numa-node"
resources:
  requests: { cpu: "16", memory: "64Gi" }
```

### In-Memory Database
```yaml
annotations:
  scheduling.kubenexus.io/numa-policy: "single-numa-node"
  scheduling.kubenexus.io/numa-affinity-node-id: "0"
  scheduling.kubenexus.io/memory-intensive: "true"
  scheduling.kubenexus.io/numa-distance-weight: "90"
resources:
  requests: { cpu: "8", memory: "64Gi" }
```

---

## 📊 Scoring Algorithm

```
Total Score = (NUMA Fit × 40%) + 
              (Memory Bandwidth × 25%) + 
              (NUMA Distance × 20%) + 
              (Gang Affinity × 15%)
```

| Component | Weight | Optimizes For |
|-----------|--------|---------------|
| NUMA Fit | 40% | Efficient resource usage |
| Memory Bandwidth | 25% | Memory-intensive workloads |
| NUMA Distance | 20% | Inter-NUMA latency |
| Gang Affinity | 15% | Gang member co-location |

---

## 🔍 Verification Commands

```bash
# Check node labels
kubectl get node worker-1 --show-labels | grep numa

# Check pod placement
kubectl get pod my-pod -o wide

# Inside pod: verify NUMA
kubectl exec my-pod -- numactl --hardware
kubectl exec my-pod -- numastat

# Scheduler logs
kubectl logs -n kube-system kubenexus-scheduler | grep NUMA
```

---

## ⚠️ Troubleshooting

### Pod Pending
```bash
# Check events
kubectl describe pod my-pod

# Common: "no single NUMA node has sufficient capacity"
# Solution: Reduce pod size or use "best-effort" policy
```

### Cross-NUMA Traffic
```bash
# Enable kubelet Topology Manager
# /var/lib/kubelet/config.yaml:
topologyManagerPolicy: single-numa-node
cpuManagerPolicy: static
```

### Missing Node Labels
```bash
# Verify labels exist
kubectl get node worker-1 -o json | jq '.metadata.labels | with_entries(select(.key | contains("numa")))'

# Re-label if needed
./label-numa-node.sh worker-1
```

---

## 📚 Full Documentation

- **Complete Guide:** [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md)
- **Node Labeling:** [NUMA_NODE_LABELING.md](NUMA_NODE_LABELING.md)
- **Examples:** [examples/advanced-numa-examples.yaml](examples/advanced-numa-examples.yaml)
- **Index:** [README.md](README.md)

---

## 🎯 Performance Expected

| Workload | Improvement | Condition |
|----------|-------------|-----------|
| ML Training | 30-50% | Single NUMA placement |
| Distributed ML | 37-39% | Packed gang policy |
| HPC Simulation | 52-57% | Isolated gang policy |
| Memory Latency | 2-3x lower | Single NUMA vs cross-NUMA |

---

## ✨ Feature Comparison

| Feature | KubeNexus | Others |
|---------|-----------|--------|
| NUMA Filter | ✅ | ⚠️/❌ |
| Multi-factor Score | ✅ | ❌ |
| NUMA Affinity | ✅ | ❌ |
| Memory BW | ✅ | ❌ |
| NUMA Distance | ✅ | ❌ |
| Gang + NUMA | ✅ 3 policies | ❌ |

---

**Version:** 2.0 | **Updated:** Feb 2026 | **K8s:** 1.28+
