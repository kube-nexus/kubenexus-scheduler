# KubeNexus Scheduler - Complete User Guide

**Production-grade Kubernetes scheduling for ML/AI, HPC, and distributed workloads**

---

## 📖 Table of Contents

1. [Quick Start](#quick-start)
2. [NUMA-Aware Scheduling](#numa-aware-scheduling)
3. [Gang Scheduling](#gang-scheduling)
4. [Scheduler Comparison](#scheduler-comparison)
5. [Common Scenarios](#common-scenarios)
6. [Troubleshooting](#troubleshooting)

---

## Quick Start

### Installation

```bash
# Deploy KubeNexus scheduler
kubectl apply -f deploy/kubenexus-scheduler.yaml

# Verify deployment
kubectl get pods -n kube-system | grep kubenexus
```

### Basic Gang Scheduling Example

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: worker-0
  labels:
    pod-group.scheduling.kubenexus.io/name: "distributed-training"
    pod-group.scheduling.kubenexus.io/min-available: "4"
spec:
  schedulerName: kubenexus-scheduler
  containers:
  - name: worker
    image: pytorch/pytorch:latest
    resources:
      requests:
        cpu: "4"
        memory: "8Gi"
```

---

## NUMA-Aware Scheduling

### Overview

NUMA (Non-Uniform Memory Access) scheduling optimizes pod placement based on CPU, memory, and device topology for maximum performance.

**Use Cases:**
- ML/AI training with GPUs
- High-performance computing (HPC)
- Real-time processing
- Low-latency applications

### Quick Reference

#### Annotations

| Annotation | Values | Description |
|------------|--------|-------------|
| `numa.scheduling.kubenexus.io/policy` | `best-effort`, `restricted`, `single-numa`, `isolated` | NUMA allocation policy |
| `numa.scheduling.kubenexus.io/resources` | `cpu,memory,nvidia.com/gpu` | Resources to align |

#### Policies

**1. best-effort** (Default)
- Tries NUMA alignment, falls back to any node
- Good for: Development, testing

**2. restricted**
- Requires resources from minimal NUMA nodes
- Good for: Production workloads

**3. single-numa**
- ALL resources from single NUMA node
- Good for: High-performance workloads

**4. isolated**
- Dedicated NUMA node, no sharing
- Good for: Ultra-low latency, HPC

### Examples

#### ML Training with GPU

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ml-training
  annotations:
    numa.scheduling.kubenexus.io/policy: "single-numa"
    numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu"
spec:
  schedulerName: kubenexus-scheduler
  containers:
  - name: training
    image: nvcr.io/nvidia/pytorch:latest
    resources:
      requests:
        cpu: "16"
        memory: "64Gi"
        nvidia.com/gpu: "2"
      limits:
        nvidia.com/gpu: "2"
```

#### HPC Workload (Isolated)

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: hpc-simulation
  annotations:
    numa.scheduling.kubenexus.io/policy: "isolated"
    numa.scheduling.kubenexus.io/resources: "cpu,memory"
spec:
  schedulerName: kubenexus-scheduler
  containers:
  - name: simulation
    image: my-hpc-app:latest
    resources:
      requests:
        cpu: "32"
        memory: "128Gi"
```

### Node Labeling

Label nodes with NUMA topology for optimal scheduling:

```bash
# Automatic labeling (recommended)
kubectl apply -f deploy/numa-labeler-daemonset.yaml

# Manual labeling
kubectl label node worker-1 \
  numa.kubenexus.io/node-0-cpus="0-15,32-47" \
  numa.kubenexus.io/node-0-memory="64Gi" \
  numa.kubenexus.io/node-1-cpus="16-31,48-63" \
  numa.kubenexus.io/node-1-memory="64Gi"
```

---

## Gang Scheduling

### What is Gang Scheduling?

Gang scheduling ensures **ALL** pods in a group are scheduled together atomically, or none at all. Essential for distributed workloads like:
- Distributed ML training (PyTorch, TensorFlow)
- Apache Spark jobs
- MPI applications

### Configuration

Add labels to all pods in the gang:

```yaml
labels:
  pod-group.scheduling.kubenexus.io/name: "my-job"
  pod-group.scheduling.kubenexus.io/min-available: "8"
```

### Example: Distributed Training

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: distributed-training
spec:
  parallelism: 8
  completions: 8
  template:
    metadata:
      labels:
        pod-group.scheduling.kubenexus.io/name: "training-job"
        pod-group.scheduling.kubenexus.io/min-available: "8"
    spec:
      schedulerName: kubenexus-scheduler
      containers:
      - name: worker
        image: pytorch/pytorch:latest
        resources:
          requests:
            cpu: "4"
            memory: "16Gi"
            nvidia.com/gpu: "1"
```

---

## Scheduler Comparison

### KubeNexus vs Alternatives

| Feature | KubeNexus | Kueue | YuniKorn | Volcano |
|---------|-----------|-------|----------|---------|
| **Gang Scheduling** | ✅ Built-in | ✅ Via integration | ✅ Native | ✅ Native |
| **NUMA Awareness** | ✅ Full support | ❌ Limited | ⚠️ CPU only | ❌ Limited |
| **GPU Topology** | ✅ Yes | ❌ No | ✅ Yes | ⚠️ Basic |
| **Multi-tenancy** | ⚠️ Basic | ✅ Strong | ✅ Strong | ⚠️ Medium |
| **Complexity** | 🟢 Low | 🟡 Medium | 🔴 High | 🟡 Medium |
| **Maturity** | 🆕 New | ✅ Production | ✅ Production | ✅ Production |

### When to Use KubeNexus

**Choose KubeNexus if:**
- ✅ You need NUMA-aware scheduling for GPU workloads
- ✅ You want simple gang scheduling without complex CRDs
- ✅ You prefer lightweight, focused functionality
- ✅ You're running ML/AI or HPC workloads

**Consider alternatives if:**
- ❌ You need complex multi-tenancy (→ YuniKorn, Kueue)
- ❌ You need elaborate fair-share policies (→ YuniKorn)
- ❌ You need proven enterprise support (→ YuniKorn, Volcano)

---

## Common Scenarios

### Scenario 1: Multi-GPU ML Training

**Requirements:** 4 workers, 2 GPUs each, NUMA-aligned

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: multi-gpu-training
spec:
  parallelism: 4
  completions: 4
  template:
    metadata:
      labels:
        pod-group.scheduling.kubenexus.io/name: "gpu-training"
        pod-group.scheduling.kubenexus.io/min-available: "4"
      annotations:
        numa.scheduling.kubenexus.io/policy: "single-numa"
        numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu"
    spec:
      schedulerName: kubenexus-scheduler
      containers:
      - name: worker
        image: nvcr.io/nvidia/pytorch:latest
        resources:
          requests:
            cpu: "16"
            memory: "64Gi"
            nvidia.com/gpu: "2"
```

### Scenario 2: Apache Spark on Kubernetes

**Requirements:** 1 driver + 10 executors, gang scheduling

```yaml
# Spark driver
apiVersion: v1
kind: Pod
metadata:
  name: spark-driver
  labels:
    pod-group.scheduling.kubenexus.io/name: "spark-job-123"
    pod-group.scheduling.kubenexus.io/min-available: "11"
    spark-role: driver
spec:
  schedulerName: kubenexus-scheduler
  containers:
  - name: spark
    image: spark:3.5.0
    resources:
      requests:
        cpu: "2"
        memory: "4Gi"
---
# Spark executors (scaled to 10 replicas)
apiVersion: apps/v1
kind: Deployment
metadata:
  name: spark-executor
spec:
  replicas: 10
  selector:
    matchLabels:
      spark-role: executor
  template:
    metadata:
      labels:
        pod-group.scheduling.kubenexus.io/name: "spark-job-123"
        pod-group.scheduling.kubenexus.io/min-available: "11"
        spark-role: executor
    spec:
      schedulerName: kubenexus-scheduler
      containers:
      - name: executor
        image: spark:3.5.0
        resources:
          requests:
            cpu: "4"
            memory: "8Gi"
```

### Scenario 3: HPC with Exclusive NUMA

**Requirements:** Isolated NUMA node for simulation

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: hpc-simulation
  annotations:
    numa.scheduling.kubenexus.io/policy: "isolated"
    numa.scheduling.kubenexus.io/resources: "cpu,memory"
spec:
  schedulerName: kubenexus-scheduler
  containers:
  - name: simulation
    image: hpc-app:latest
    resources:
      requests:
        cpu: "32"
        memory: "128Gi"
      limits:
        cpu: "32"
        memory: "128Gi"
```

---

## Troubleshooting

### Pod Stuck in Pending

#### Issue: "waiting for pod group"

**Cause:** Gang scheduling - waiting for other pods

```bash
# Check pod group status
kubectl describe pod <pod-name> | grep -A 10 Events

# Check how many pods are ready
kubectl get pods -l pod-group.scheduling.kubenexus.io/name=<group-name>
```

**Solutions:**
1. Wait for all pods in gang to be scheduled
2. Check if enough cluster resources exist
3. Verify minAvailable matches actual pod count

#### Issue: "no nodes with NUMA topology"

**Cause:** Nodes missing NUMA labels

```bash
# Check node labels
kubectl get nodes -o json | jq '.items[].metadata.labels' | grep numa

# Label nodes
kubectl apply -f deploy/numa-labeler-daemonset.yaml
```

#### Issue: "no single NUMA node has sufficient capacity"

**Cause:** Resources exceed single NUMA node capacity

**Solutions:**
1. Use `restricted` policy instead of `single-numa`
2. Reduce resource requests
3. Use nodes with larger NUMA nodes

### Check Scheduler Logs

```bash
# View scheduler logs
kubectl logs -n kube-system deployment/kubenexus-scheduler -f

# Check for specific pod
kubectl logs -n kube-system deployment/kubenexus-scheduler | grep <pod-name>
```

### Common Commands

```bash
# List pods using KubeNexus scheduler
kubectl get pods --all-namespaces -o json | \
  jq -r '.items[] | select(.spec.schedulerName=="kubenexus-scheduler") | .metadata.name'

# Check pod scheduling events
kubectl get events --field-selector involvedObject.name=<pod-name>

# Verify NUMA labels on nodes
kubectl get nodes -L numa.kubenexus.io/node-count

# Force pod rescheduling
kubectl delete pod <pod-name>
```

### Performance Issues

#### High Latency

1. Check NUMA policy - use `single-numa` or `isolated`
2. Verify CPU pinning is enabled
3. Check memory is NUMA-local

#### GPU Underutilization

1. Ensure GPUs are on same NUMA node as CPU/memory
2. Use `numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu"`
3. Check PCIe topology with `nvidia-smi topo -m`

---

## Configuration Reference

### Scheduler Configuration

Edit `config/config.yaml`:

```yaml
apiVersion: kubescheduler.config.k8s.io/v1
kind: KubeSchedulerConfiguration
profiles:
  - schedulerName: kubenexus-scheduler
    plugins:
      queueSort:
        enabled:
          - name: Coscheduling
      preFilter:
        enabled:
          - name: Coscheduling
      permit:
        enabled:
          - name: Coscheduling
      reserve:
        enabled:
          - name: Coscheduling
      score:
        enabled:
          - name: NUMATopology
            weight: 10
```

### Supported Annotations

```yaml
# NUMA scheduling
numa.scheduling.kubenexus.io/policy: "best-effort|restricted|single-numa|isolated"
numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu,..."

# Gang scheduling (via labels)
labels:
  pod-group.scheduling.kubenexus.io/name: "<group-name>"
  pod-group.scheduling.kubenexus.io/min-available: "<count>"
```

---

## Best Practices

### 1. NUMA Scheduling

- ✅ Use `single-numa` for GPU workloads
- ✅ Use `isolated` for HPC/ultra-low latency
- ✅ Use `restricted` for production workloads
- ✅ Always label nodes with NUMA topology

### 2. Gang Scheduling

- ✅ Set `minAvailable` = total pod count
- ✅ Use consistent gang name across all pods
- ✅ Set appropriate timeouts for large gangs
- ❌ Don't mix gang and non-gang pods in same job

### 3. Resource Requests

- ✅ Set realistic CPU/memory requests
- ✅ Match requests to NUMA node capacity
- ✅ Use limits for critical workloads
- ❌ Don't over-request resources

### 4. Monitoring

- ✅ Monitor scheduler logs
- ✅ Track pod scheduling latency
- ✅ Alert on stuck pods
- ✅ Review NUMA node utilization

---

## Additional Resources

- **Main README:** [../README.md](../README.md) - Installation & quick start
- **NUMA Scheduling Guide:** [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md) - Deep dive into NUMA architecture
- **Scheduler Comparison:** [SCHEDULER_COMPARISON.md](SCHEDULER_COMPARISON.md) - Detailed comparison with alternatives
- **Design Decisions:** [DESIGN_DECISIONS.md](DESIGN_DECISIONS.md) - Architecture and API design rationale
- **Contributing:** [../CONTRIBUTING.md](../CONTRIBUTING.md) - Development guide

---

*Last Updated: February 2026*
