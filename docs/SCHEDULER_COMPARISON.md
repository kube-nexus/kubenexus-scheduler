# Kubernetes Scheduler Comparison

A technical comparison of advanced Kubernetes schedulers for batch workloads, GPU scheduling, and multi-tenancy.

---

## Quick Comparison

| Feature | **KubeNexus** | **Kueue** | **YuniKorn** |
|---------|---------------|-----------|--------------|
| **Primary Focus** | Hybrid scheduling (batch + service) | Job queueing & quota management | Multi-tenant resource scheduling |
| **Gang Scheduling** | ✅ Built-in | ✅ Via integration | ✅ Native support |
| **GPU Topology** | ✅ NUMA + PCIe aware | ❌ Limited | ⚠️ CPU only |
| **Preemption** | ✅ Policy-based | ✅ Queue priority | ✅ Fair-share preemption |
| **Workload Classification** | ✅ Automatic | ❌ Manual queue assignment | ⚠️ Application-based |
| **Multi-tenancy** | ⚠️ Basic namespaces | ✅ Strong queue hierarchies | ✅ Strong hierarchies |
| **Deployment** | Plugin (easy) | Controller + CRDs | Replace scheduler |
| **Maturity** | 🆕 Beta (v0.1.x) | ✅ Production | ✅ Production |

---

## 1. Kueue (Kubernetes SIG)

### Overview
**Kueue** is a Kubernetes-native job queueing system for **quota management and fair sharing**. It works on top of the default Kubernetes scheduler.

### Architecture
```
Job → Kueue Admission → Queue → Quota Check → Unsuspend → K8s Scheduler
```

### Key Features
- **Hierarchical queues** (ClusterQueue → LocalQueue)
- **Resource quotas** per tenant/team
- **Job prioritization** within queues
- **AdmissionChecks** for pre-scheduling validation
- **Integration** with Kubeflow, Spark, Ray operators

### Best For
✅ Multi-tenant GPU clusters with strict quotas  
✅ Fair sharing across teams/projects  
✅ Cost optimization (preemptible vs on-demand)  
✅ GKE/Autopilot integration

### Limitations
❌ Not a scheduler (relies on default K8s scheduler)  
❌ No topology awareness (NUMA, PCIe locality)  
❌ Manual queue assignment required

---

## 2. YuniKorn (Apache)

### Overview
**YuniKorn** is a standalone scheduler (replaces kube-scheduler) designed for **big data and ML workloads** at scale.

### Architecture
```
Job → YuniKorn Scheduler → Gang Scheduling → Placement → Binding
```

### Key Features
- **Native gang scheduling** with placeholder pods
- **Hierarchical queues** with guaranteed/max resources
- **Fair-share scheduling** (DRF algorithm)
- **GPU scheduling** with topology and sharing support
- **Application-level tracking** (not just pods)

### Best For
✅ Spark on Kubernetes (best-in-class)  
✅ Large-scale clusters (100+ GPUs, 5000+ nodes)  
✅ Multi-tenant big data platforms  
✅ Fair-share with strong SLAs

### Limitations
❌ Replaces kube-scheduler (operational complexity)  
❌ Steep learning curve (Hadoop-style configs)  
❌ Overkill for simple use cases

---

## 3. KubeNexus

### Overview
**KubeNexus** is a lightweight scheduler plugin providing **automatic workload classification and topology-aware scheduling** for hybrid workloads.

### Architecture
```
Pod → Classification → Topology Scoring → Gang Scheduling → Binding
```

### Key Features
- **Automatic workload detection** (Spark, TensorFlow, PyTorch, Ray)
- **GPU topology awareness** (NUMA, PCIe, NVLink)
- **Hybrid scoring** (spread services, pack batch jobs)
- **Resource reservation** for driver pods
- **Gang scheduling** via coscheduling plugin

### Best For
✅ Mixed service + batch workloads  
✅ GPU topology optimization  
✅ Simple deployment (no CRDs)  
✅ Automatic workload handling

### Limitations
⚠️ Beta status (v0.1.x)  
❌ No built-in quota management  
❌ Basic multi-tenancy only  
❌ Not production-proven yet

---

## Feature Comparison

### Gang Scheduling

| Scheduler | Implementation | Timeout | Placeholders | Maturity |
|-----------|----------------|---------|--------------|----------|
| **Kueue** | Via JobSet/operators | ✅ Configurable | ❌ No | ✅ High |
| **YuniKorn** | Native, app-level | ✅ Advanced | ✅ Yes | ✅ High |
| **KubeNexus** | Coscheduling plugin | ⚠️ Basic | ❌ No | ⚠️ Beta |

**Winner**: YuniKorn (most mature)

### GPU Topology Awareness

| Feature | Kueue | YuniKorn | KubeNexus |
|---------|-------|----------|-----------|
| **NUMA locality** | ❌ | ⚠️ CPU only | ✅ GPU + CPU |
| **PCIe topology** | ❌ | ⚠️ Basic | ✅ Advanced |
| **NVLink detection** | ❌ | ❌ | ✅ Yes |
| **GPU sharing** | ⚠️ Time-slicing | ✅ MIG + time-slicing | ⚠️ External |

**Winner**: KubeNexus (topology), YuniKorn (sharing)

### Multi-Tenancy & Quotas

| Feature | Kueue | YuniKorn | KubeNexus |
|---------|-------|----------|-----------|
| **Hierarchical queues** | ✅ Yes | ✅ Yes | ➡️ Use Kueue |
| **Resource quotas** | ✅ Strong | ✅ Strong | ➡️ Use Kueue |
| **Fair sharing (DRF)** | ✅ Weighted | ✅ DRF | ➡️ Use Kueue |
| **Preemption** | ✅ Queue priority | ✅ Fair-share | ✅ Topology-aware |

**Winner**: Kueue & YuniKorn (admission control)  
**Note**: KubeNexus focuses on *placement*, not *admission*. Use Kueue for quotas/fair-share.

---

## Use Case Recommendations

### Large-Scale ML Platform (500+ GPUs, 10+ teams)
**Recommended**: **Kueue + KubeNexus** or **Kueue + YuniKorn**
- Kueue for quota management and fair-share (DRF)
- KubeNexus for GPU topology optimization and fragmentation prevention
- YuniKorn alternative if Spark-focused and need application-level tracking

### Spark on Kubernetes
**Recommended**: **YuniKorn**
- Best Spark integration
- Native gang scheduling for executors
- Application-level tracking

### Mixed Service + Batch Workloads
**Recommended**: **KubeNexus**
- Automatic workload classification
- Hybrid scoring (spread vs pack)
- Simple deployment

### Multi-GPU Topology Optimization
**Recommended**: **KubeNexus**
- NUMA + PCIe aware
- NVLink detection
- GPU-CPU affinity

### Enterprise Multi-Tenancy with GPU Topology Needs
**Recommended**: **Kueue (admission) + KubeNexus (placement)**
- Kueue: Strong quota management, hierarchical queues, DRF fair-share
- KubeNexus: GPU topology optimization, fragmentation prevention
- Best of both: admission control + intelligent placement

### Enterprise Multi-Tenancy (CPU/memory focus)
**Recommended**: **Kueue** or **YuniKorn**
- Strong quota management
- Hierarchical queues
- Fair-share scheduling

---

## Combining Schedulers

### Architecture: Kueue + KubeNexus
```
Job → Kueue (admission + quota) → KubeNexus (topology placement) → Pods
```

**Benefits**:
- Kueue handles multi-tenancy and quotas
- KubeNexus optimizes GPU topology
- Best of both worlds

**Configuration**:
```yaml
apiVersion: kueue.x-k8s.io/v1beta1
kind: LocalQueue
metadata:
  name: gpu-queue
spec:
  clusterQueue: gpu-cluster-queue
---
apiVersion: batch/v1
kind: Job
metadata:
  labels:
    kueue.x-k8s.io/queue-name: gpu-queue
spec:
  template:
    spec:
      schedulerName: kubenexus-scheduler  # Use KubeNexus for placement
      containers:
      - name: training
        resources:
          requests:
            nvidia.com/gpu: 4
```

---

## Technical Details

### Kueue Gang Scheduling
Uses JobSet or operator integration:
```yaml
apiVersion: jobset.x-k8s.io/v1alpha2
kind: JobSet
metadata:
  name: distributed-training
spec:
  successPolicy:
    operator: All  # All jobs must succeed
  replicatedJobs:
  - name: workers
    replicas: 8
```

### YuniKorn Gang Scheduling
Native with placeholders:
```yaml
apiVersion: v1
kind: Pod
metadata:
  labels:
    applicationId: spark-app-001
    task-group.kubenexus.io/name: executors
    task-group.kubenexus.io/minMember: "8"
```

### KubeNexus Gang Scheduling
Pod-group labels:
```yaml
apiVersion: v1
kind: Pod
metadata:
  labels:
    pod-group.scheduling.kubenexus.io/name: training-001
    pod-group.scheduling.kubenexus.io/min-available: "8"
spec:
  schedulerName: kubenexus-scheduler
```

---

## Performance Considerations

### Scheduling Latency

| Scheduler | Single Pod | Gang (8 pods) | Notes |
|-----------|------------|---------------|-------|
| **Kueue** | ~5-10ms overhead | Depends on operator | Admission overhead |
| **YuniKorn** | ~5-15ms | ~50-100ms | Placeholder creation |
| **KubeNexus** | ~5-10ms | ~50ms (target) | Plugin overhead |

### Resource Overhead

| Scheduler | Memory | CPU | Storage |
|-----------|--------|-----|---------|
| **Kueue** | ~100MB | <0.1 core | CRD state |
| **YuniKorn** | ~200MB | <0.5 core | In-memory |
| **KubeNexus** | ~50MB | <0.1 core | In-memory |

---

## Migration Path

### From Default Scheduler to KubeNexus
1. Deploy KubeNexus alongside default scheduler
2. Test with specific workloads (`schedulerName: kubenexus-scheduler`)
3. Gradually migrate workloads
4. No CRD migration needed

### From Kueue to Kueue + KubeNexus
1. Keep Kueue for admission control
2. Add KubeNexus for placement
3. Update pod templates to use `kubenexus-scheduler`
4. Kueue continues managing quotas

### From Default to YuniKorn
1. Deploy YuniKorn
2. Configure queues
3. Update workloads to use `schedulerName: yunikorn`
4. Requires complete scheduler replacement

---

## Recommendations by Cluster Size

### Small (< 50 nodes, < 20 GPUs)
**Recommended**: **KubeNexus** or **Default + ResourceQuotas**
- Low complexity
- Simple topology optimization
- No need for complex multi-tenancy

### Medium (50-500 nodes, 20-100 GPUs)
**Recommended**: **Kueue + KubeNexus**
- Kueue for quota management
- KubeNexus for topology
- Balanced complexity vs features

### Large (500+ nodes, 100+ GPUs)
**Recommended**: **Kueue + YuniKorn** or **YuniKorn only**
- Proven at scale
- Strong multi-tenancy
- Mature gang scheduling

---

## References

- [Kueue Documentation](https://kueue.sigs.k8s.io/)
- [YuniKorn Documentation](https://yunikorn.apache.org/)
- [Kubernetes Scheduler Framework](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/)
- [Gang Scheduling in Kubernetes](https://github.com/kubernetes-sigs/scheduler-plugins/tree/master/pkg/coscheduling)

---

## Summary

| Scenario | Best Choice | Reason |
|----------|-------------|--------|
| **Production-scale multi-tenancy** | Kueue or YuniKorn | Proven quota management |
| **Spark workloads** | YuniKorn | Best Spark integration |
| **GPU topology optimization** | KubeNexus | Advanced NUMA/PCIe awareness |
| **Mixed service + batch** | KubeNexus | Automatic workload classification |
| **Large-scale (1000+ nodes)** | YuniKorn | Proven scalability |
| **Simple deployment** | KubeNexus | Lightweight plugin |

**KubeNexus Status**: Beta (v0.1.x) - Suitable for testing and early adoption. For production-critical workloads, consider mature alternatives like Kueue or YuniKorn until KubeNexus reaches v1.0.
