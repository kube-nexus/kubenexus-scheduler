# Kubernetes Scheduler Comparison: KubeNexus vs Kueue vs YuniKorn

## Executive Summary

A comprehensive comparison of three advanced Kubernetes schedulers designed for batch workloads, GPU scheduling, and multi-tenancy. This analysis is particularly relevant for teams running ML/AI workloads, big data processing, and GPU-intensive applications.

---

## Quick Comparison Matrix

| Feature | **KubeNexus** | **Kueue** | **YuniKorn** |
|---------|---------------|-----------|--------------|
| **Primary Focus** | Hybrid scheduling (batch + service) | Job queueing & quota management | Multi-tenant resource scheduling |
| **Gang Scheduling** | ✅ Built-in | ✅ Via integration | ✅ Native support |
| **GPU Support** | ✅ Topology-aware | ✅ Via resource quotas | ✅ Advanced GPU sharing |
| **Preemption** | ✅ Policy-based | ✅ Queue priority | ✅ Fair-share preemption |
| **Resource Reservation** | ✅ Proactive | ⚠️ Via AdmissionCheck | ✅ Native reservations |
| **Workload Classification** | ✅ Automatic (batch/service) | ❌ Manual queue assignment | ⚠️ Application-based |
| **NUMA Awareness** | ✅ Built-in topology scoring | ❌ Limited | ✅ CPU topology |
| **Multi-tenancy** | ⚠️ Basic namespaces | ✅ Strong queue hierarchies | ✅ Strong hierarchies |
| **Maturity** | 🆕 Early stage | ✅ Production (Google) | ✅ Production (Apache) |
| **Adoption** | 🆕 New | 🔥 Growing (GKE) | 🔥 High (Cloudera, Apple, etc.) |

---

## 1. Kueue (Google/Kubernetes SIG)

### What is Kueue?
**Kueue** is a Kubernetes-native job queueing system that provides **quota management, job ordering, and resource fairness**. It's designed to work **on top of** the default Kubernetes scheduler, not replace it.

### Architecture
```
User → Submit Job → Kueue Admission Controller → Queue → Quota Check → 
Unsuspend Job → Default K8s Scheduler → Pod Placement
```

### Key Features
1. **Queue Management**
   - Multiple queues per namespace/tenant
   - Hierarchical queue structure (ClusterQueue → LocalQueue)
   - FIFO, Priority, StrictFIFO ordering

2. **Resource Quotas (ClusterQueue)**
   ```yaml
   apiVersion: kueue.x-k8s.io/v1beta1
   kind: ClusterQueue
   metadata:
     name: gpu-cluster-queue
   spec:
     namespaceSelector: {}
     resourceGroups:
     - coveredResources: ["cpu", "memory", "nvidia.com/gpu"]
       flavors:
       - name: a100-80gb
         resources:
         - name: "nvidia.com/gpu"
           nominalQuota: 8
           borrowingLimit: 4
   ```

3. **AdmissionChecks**
   - Pre-scheduling validation (GPU availability, licenses, etc.)
   - Integration with ProvisioningRequest (for node provisioning)

4. **Job Types Supported**
   - Batch/Job
   - Kubeflow (TFJob, PyTorchJob, MPIJob)
   - Ray Jobs
   - Spark (via spark-operator)

### When Kueue Shines ✨
- **Multi-tenant GPU clusters** with strict quotas
- **Fair sharing** across teams/projects
- **Job prioritization** in queues
- **Integration with GKE Autopilot/DWS** (Dynamic Workload Scheduler)
- **Cost optimization** (preemptible vs on-demand)

### Limitations ⚠️
- **Not a scheduler** - relies on default K8s scheduler for placement
- **No topology awareness** (NUMA, PCIe locality)
- **Limited gang scheduling** (requires integration)
- **Queue assignment is manual** (users must specify queue)
- **Preemption is coarse-grained** (entire jobs, not pods)

### Apple's Use Case (GPU Workloads)
Apple likely uses Kueue for:
- **ML training pipeline management** (queue hundreds of training jobs)
- **GPU quota enforcement** across ML teams
- **Fair-share GPU allocation** (team A gets 40%, team B gets 60%)
- **Integration with GKE/Autopilot** for auto-scaling GPU nodes

---

## 2. YuniKorn (Apache Incubator)

### What is YuniKorn?
**YuniKorn** is a **standalone scheduler** (replaces kube-scheduler) designed for **big data and ML workloads**. Originally developed by Cloudera for Hadoop-on-Kubernetes.

### Architecture
```
User → Submit Job → YuniKorn Scheduler → Resource Analysis → 
Gang Scheduling → Node Placement → Pod Binding
```

### Key Features
1. **Hierarchical Queues**
   ```yaml
   partitions:
     - name: default
       queues:
         - name: root
           queues:
             - name: production
               resources:
                 guaranteed: {memory: 500G, vcore: 200, nvidia.com/gpu: 10}
                 max: {memory: 800G, vcore: 300, nvidia.com/gpu: 16}
             - name: dev
               resources:
                 guaranteed: {memory: 200G, vcore: 50, nvidia.com/gpu: 4}
   ```

2. **Gang Scheduling**
   - Native support for all-or-nothing scheduling
   - Placeholder pods for resource reservation
   - Timeout and retry mechanisms

3. **Preemption & Fair Sharing**
   - DRF (Dominant Resource Fairness)
   - Fair-share preemption across queues
   - Priority-based preemption

4. **GPU Scheduling**
   - GPU topology awareness
   - GPU sharing (via time-slicing or MIG)
   - GPU affinity (keep pods on same GPU node)

5. **Application-centric Scheduling**
   - Tracks entire application lifecycle (not just pods)
   - Application-level gang scheduling
   - Task groups within applications

### When YuniKorn Shines ✨
- **Spark on Kubernetes** (best-in-class support)
- **Large-scale GPU clusters** (100+ GPUs)
- **Multi-tenant big data platforms**
- **Hierarchical org structures** (business unit → team → project)
- **Fair-share scheduling** with strong SLAs
- **Gang scheduling for distributed training** (Horovod, DeepSpeed)

### Limitations ⚠️
- **Replaces kube-scheduler** (operational complexity)
- **Steep learning curve** (Hadoop-style queue configs)
- **No automatic workload classification**
- **Overkill for simple use cases**
- **Community adoption still growing**

### Apple's Use Case (GPU Workloads)
Apple likely uses YuniKorn for:
- **Large-scale distributed training** (GPT-style models)
- **Fair-share GPU allocation** with strong guarantees
- **Spark-based data pipelines** feeding ML training
- **Gang scheduling for multi-GPU training jobs**

---

## 3. KubeNexus (This Project)

### What is KubeNexus?
**KubeNexus** is a **lightweight scheduler plugin** that provides **intelligent workload classification, topology-aware scoring, and hybrid scheduling** for mixed service + batch workloads.

### Architecture
```
User → Submit Pod → KubeNexus Plugins → 
Workload Classification (batch/service) → 
Scoring (topology/hybrid) → Node Selection → Pod Binding
```

### Key Features
1. **Automatic Workload Classification**
   ```go
   // Automatically detects Spark, TensorFlow, PyTorch, Ray, etc.
   workloadType := classification.ClassifyPod(pod)
   if workloadType == workload.TypeBatch {
       // Apply batch-optimized scheduling
   }
   ```

2. **Topology-Aware GPU Scheduling**
   - NUMA locality scoring
   - PCIe topology awareness
   - GPU-CPU affinity
   - NVLink/NVSwitch detection

3. **Hybrid Scoring Algorithm**
   ```
   Score = 0.4 × ResourceBalance + 0.3 × Topology + 0.3 × Workload
   ```
   - Batch workloads: favor consolidation (bin packing)
   - Service workloads: favor spreading (high availability)

4. **Resource Reservation**
   - Proactive reservation for driver pods (Spark)
   - Prevents starvation in mixed workloads

5. **Gang Scheduling (Coscheduling Plugin)**
   - All-or-nothing scheduling for distributed jobs
   - Compatible with pod-group annotations

### When KubeNexus Shines ✨
- **Mixed workloads** (APIs + ML training on same cluster)
- **Automatic optimization** (no manual queue assignment)
- **GPU topology optimization** (multi-GPU nodes)
- **Lightweight deployment** (scheduler plugins, not full scheduler)
- **Flexible scoring** (easy to customize)

### Limitations ⚠️
- **Early stage project** (not production-proven)
- **No built-in quota management** (relies on K8s ResourceQuotas)
- **No queue management** (relies on K8s Priorities)
- **Limited multi-tenancy** (namespace-based only)
- **No autoscaling integration** (yet)

---

## Detailed Feature Comparison

### Gang Scheduling

| Scheduler | Implementation | Timeout Handling | Placeholder Pods |
|-----------|----------------|------------------|------------------|
| **Kueue** | Via JobSet/MPIJob integration | ✅ Configurable | ❌ No |
| **YuniKorn** | Native, application-level | ✅ Advanced retry | ✅ Yes |
| **KubeNexus** | Pod-group plugin | ⚠️ Basic | ❌ No |

**Winner**: YuniKorn (most mature)

---

### GPU Scheduling & Topology

| Feature | Kueue | YuniKorn | KubeNexus |
|---------|-------|----------|-----------|
| **NUMA Awareness** | ❌ | ⚠️ CPU only | ✅ GPU + CPU |
| **PCIe Locality** | ❌ | ⚠️ Basic | ✅ Advanced |
| **NVLink Detection** | ❌ | ❌ | ✅ Yes |
| **GPU Sharing** | ⚠️ Via time-slicing | ✅ MIG + time-slicing | ⚠️ External (MIG) |
| **Multi-Instance GPU (MIG)** | ❌ | ✅ | ⚠️ Planned |

**Winner**: KubeNexus (for topology), YuniKorn (for sharing)

---

### Multi-Tenancy & Quotas

| Feature | Kueue | YuniKorn | KubeNexus |
|---------|-------|----------|-----------|
| **Hierarchical Queues** | ✅ ClusterQueue → LocalQueue | ✅ Multi-level | ❌ |
| **Resource Quotas** | ✅ Strong (per flavor) | ✅ Strong (per queue) | ⚠️ K8s ResourceQuota only |
| **Fair Sharing** | ✅ Weighted sharing | ✅ DRF algorithm | ❌ |
| **Preemption** | ✅ Queue priority | ✅ Fair-share + priority | ✅ Basic priority |
| **Borrowing/Bursting** | ✅ Yes | ✅ Yes | ❌ |

**Winner**: Kueue & YuniKorn (tie - both excellent)

---

### Operational Complexity

| Aspect | Kueue | YuniKorn | KubeNexus |
|--------|-------|----------|-----------|
| **Deployment** | Easy (CRDs + controller) | Medium (replace scheduler) | Easy (scheduler plugins) |
| **Configuration** | Medium (queue setup) | Complex (Hadoop-style) | Simple (plugin config) |
| **Learning Curve** | Low-Medium | High | Low |
| **Observability** | ✅ Metrics + events | ✅ Rich metrics | ⚠️ Basic |
| **Troubleshooting** | Easy (standard K8s) | Medium (custom scheduler) | Easy (standard K8s) |

**Winner**: KubeNexus (simplest), Kueue (easiest multi-tenancy)

---

## Real-World Use Cases

### Use Case 1: ML Training Platform (Apple-style)
**Scenario**: 1000+ ML engineers, 500 GPUs, multiple teams

**Best Choice**: **Kueue + YuniKorn**
- **Kueue** for queue management, quotas, job ordering
- **YuniKorn** for gang scheduling, fair-share, GPU topology
- **Why**: Strong multi-tenancy, proven at scale

**KubeNexus Alternative**: Not ready for this scale yet

---

### Use Case 2: Spark + GPU Analytics
**Scenario**: Big data pipelines + ML inference

**Best Choice**: **YuniKorn**
- Native Spark support
- Gang scheduling for Spark executors
- GPU scheduling for inference pods
- **Why**: Best Spark integration

**KubeNexus Alternative**: Good for smaller Spark workloads

---

### Use Case 3: Mixed Service + Batch Workloads
**Scenario**: APIs (service) + nightly batch jobs on same cluster

**Best Choice**: **KubeNexus**
- Automatic workload classification
- Hybrid scoring (spread services, pack batch)
- Simple deployment
- **Why**: Optimized for mixed workloads

**Kueue Alternative**: Would need separate queues + manual classification

---

### Use Case 4: GPU Node Optimization
**Scenario**: Multi-GPU nodes (8x A100 with NVLink)

**Best Choice**: **KubeNexus**
- NUMA awareness
- NVLink topology scoring
- PCIe locality optimization
- **Why**: Best GPU topology awareness

**YuniKorn Alternative**: Good GPU support but less topology-aware

---

## Interview Talking Points (Apple K8s Team)

### 1. Understanding Their Current Setup
Ask about:
- "Which scheduler are you using primarily - Kueue, YuniKorn, or both?"
- "What GPU types do you manage? (A100, H100, custom Apple Silicon?)"
- "How do you handle gang scheduling for distributed training?"
- "What's your approach to multi-tenancy and quota management?"

### 2. Highlight Complementary Strengths
- **If they use Kueue**: "KubeNexus could complement Kueue by providing better GPU topology awareness and automatic workload classification"
- **If they use YuniKorn**: "I've studied YuniKorn's gang scheduling implementation - very elegant. KubeNexus takes a lighter approach as a plugin"

### 3. Demonstrate Deep Knowledge
- "Kueue's AdmissionCheck mechanism is brilliant for pre-scheduling validation"
- "YuniKorn's DRF algorithm ensures true fair-share across multiple resource types"
- "One challenge with Kueue is it doesn't handle NUMA/PCIe topology - that's where custom scoring helps"

### 4. Show Innovation
- "In KubeNexus, I implemented automatic workload classification - no need for users to specify queues"
- "The hybrid scoring algorithm adapts to workload type - services spread for HA, batch packs for efficiency"
- "Built-in NVLink detection for optimal multi-GPU placement"

### 5. Acknowledge Trade-offs
- "Kueue's queue management is more mature than anything I've built"
- "YuniKorn's hierarchical quotas are essential for large orgs - KubeNexus doesn't have that"
- "For Apple's scale, production-proven tools like Kueue/YuniKorn are the right choice"

---

## Technical Deep Dives

### How Kueue Would Handle a GPU Training Job

```yaml
# 1. Define ClusterQueue with GPU quota
apiVersion: kueue.x-k8s.io/v1beta1
kind: ClusterQueue
metadata:
  name: ml-training-queue
spec:
  namespaceSelector:
    matchLabels:
      team: ml-research
  resourceGroups:
  - coveredResources: ["nvidia.com/gpu"]
    flavors:
    - name: a100-80gb
      resources:
      - name: "nvidia.com/gpu"
        nominalQuota: 16
        borrowingLimit: 8
---
# 2. Create LocalQueue per namespace
apiVersion: kueue.x-k8s.io/v1beta1
kind: LocalQueue
metadata:
  namespace: ml-research
  name: training-queue
spec:
  clusterQueue: ml-training-queue
---
# 3. Submit PyTorchJob
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: distributed-training
  labels:
    kueue.x-k8s.io/queue-name: training-queue  # ← Manual assignment
spec:
  pytorchReplicaSpecs:
    Master:
      replicas: 1
      template:
        spec:
          containers:
          - name: pytorch
            resources:
              requests:
                nvidia.com/gpu: 1
    Worker:
      replicas: 7
      template:
        spec:
          containers:
          - name: pytorch
            resources:
              requests:
                nvidia.com/gpu: 1
```

**Flow**:
1. Job submitted → Kueue admission controller suspends it
2. Check quota in `ml-training-queue` (16 GPUs available?)
3. If yes → Unsuspend job → Default scheduler places pods
4. If no → Job waits in queue until resources available

**Limitation**: Default scheduler doesn't know about GPU topology (NUMA, NVLink)

---

### How YuniKorn Would Handle the Same Job

```yaml
# 1. Configure queue in yunikorn-configs
apiVersion: v1
kind: ConfigMap
metadata:
  name: yunikorn-configs
data:
  queues.yaml: |
    partitions:
      - name: default
        queues:
          - name: root
            queues:
              - name: ml-research
                resources:
                  guaranteed:
                    nvidia.com/gpu: 16
                  max:
                    nvidia.com/gpu: 24
                properties:
                  application.sort.policy: fifo
                  preemption.policy: fence
---
# 2. Submit PyTorchJob with YuniKorn annotations
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: distributed-training
  labels:
    applicationId: pytorch-training-001
    queue: root.ml-research  # ← Queue assignment
spec:
  pytorchReplicaSpecs:
    Master:
      replicas: 1
      template:
        metadata:
          labels:
            applicationId: pytorch-training-001
            task-group: master
        spec:
          schedulerName: yunikorn  # ← Use YuniKorn
          containers:
          - name: pytorch
            resources:
              requests:
                nvidia.com/gpu: 1
    Worker:
      replicas: 7
      template:
        metadata:
          labels:
            applicationId: pytorch-training-001
            task-group: worker
        spec:
          schedulerName: yunikorn
          containers:
          - name: pytorch
            resources:
              requests:
                nvidia.com/gpu: 1
```

**Flow**:
1. Job submitted → YuniKorn detects application-level gang scheduling
2. Create placeholders for all 8 pods (1 master + 7 workers)
3. Find nodes with 8 GPUs available (gang constraint)
4. Bind all pods atomically
5. Track application lifecycle (not just pods)

**Advantage**: True gang scheduling, application-level tracking

---

### How KubeNexus Would Handle the Same Job

```yaml
# 1. Submit PyTorchJob (no queue annotation needed!)
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: distributed-training
  labels:
    # KubeNexus auto-detects this as batch workload
    pytorch-job-name: distributed-training
spec:
  pytorchReplicaSpecs:
    Master:
      replicas: 1
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: pytorch-training-001
            pod-group.scheduling.kubenexus.io/min-available: "8"
        spec:
          schedulerName: kubenexus-scheduler
          containers:
          - name: pytorch
            resources:
              requests:
                nvidia.com/gpu: 1
    Worker:
      replicas: 7
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: pytorch-training-001
            pod-group.scheduling.kubenexus.io/min-available: "8"
        spec:
          schedulerName: kubenexus-scheduler
          containers:
          - name: pytorch
            resources:
              requests:
                nvidia.com/gpu: 1
```

**Flow**:
1. Job submitted → KubeNexus classifies as "batch" (PyTorch label detected)
2. Coscheduling plugin detects pod-group annotations (gang scheduling)
3. Topology scoring evaluates GPU placement:
   - Prefer nodes with NVLink between GPUs
   - Prefer NUMA-local CPU-GPU pairs
   - Prefer consolidating pods on fewer nodes
4. Bind all 8 pods with topology awareness

**Advantage**: Automatic classification + topology optimization

---

## Combining Multiple Schedulers

### Architecture: Kueue + KubeNexus
```
User → Job → Kueue (queue + quota) → Unsuspend → 
KubeNexus Scheduler (topology-aware placement) → Pods
```

**Benefits**:
- Kueue handles multi-tenancy, quotas, fairness
- KubeNexus handles GPU topology optimization
- Best of both worlds!

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
      schedulerName: kubenexus-scheduler  # ← Use KubeNexus for placement
      containers:
      - name: training
        resources:
          requests:
            nvidia.com/gpu: 4
```

---

## Recommendations by Scenario

### For Apple's K8s Team (GPU Workloads)

**Current State** (likely):
- Primary: **Kueue** (queue management, quotas)
- Secondary: **YuniKorn** (gang scheduling, fair-share)
- Scale: 100+ GPUs, 10+ teams

**Recommendations**:

1. **Keep Kueue as primary** (best multi-tenancy)
2. **Add topology awareness** via custom scheduler plugins (like KubeNexus approach)
3. **Use YuniKorn for Spark workloads** specifically
4. **Implement** automatic workload classification (reduce user burden)

**What to Pitch**:
- "I see you're using Kueue - great choice for multi-tenancy"
- "One gap is GPU topology optimization - I've built plugins that score nodes based on NVLink, NUMA locality"
- "Could integrate with Kueue - let Kueue handle admission, custom plugins handle placement"

---

## Key Takeaways for Interview

### What Kueue Does Best
✅ Multi-tenant quota management  
✅ Job queueing and prioritization  
✅ Integration with GKE/Autopilot  
✅ Simple to deploy and operate  

### What YuniKorn Does Best
✅ Gang scheduling (most mature)  
✅ Fair-share scheduling (DRF)  
✅ Spark-on-Kubernetes support  
✅ Hierarchical org structure  

### What KubeNexus Does Best
✅ Automatic workload classification  
✅ GPU topology awareness (NUMA, NVLink)  
✅ Hybrid scoring (batch vs service)  
✅ Lightweight (plugins, not full scheduler)  

### The Ideal Setup
**Kueue** (admission/quotas) + **Custom Plugins** (topology) + **YuniKorn** (gang scheduling)

---

## Questions to Ask Apple

1. **Architecture**:
   - "How do you integrate Kueue with your GPU provisioning system?"
   - "Do you use YuniKorn's gang scheduling, or handle it differently?"

2. **Scale**:
   - "What's your largest training job? (GPUs, duration)"
   - "How do you handle multi-GPU node optimization?"

3. **Challenges**:
   - "What's the biggest pain point with current schedulers?"
   - "Any issues with GPU fragmentation or underutilization?"

4. **Innovation**:
   - "Have you explored custom scoring plugins for topology?"
   - "Interest in automatic workload classification?"

---

## Further Reading

### Kueue
- Docs: https://kueue.sigs.k8s.io/
- GitHub: https://github.com/kubernetes-sigs/kueue
- KEP: https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/3521-kueue

### YuniKorn
- Docs: https://yunikorn.apache.org/
- GitHub: https://github.com/apache/yunikorn-k8shim
- Design: https://yunikorn.apache.org/docs/design/architecture/

### KubeNexus
- This repository!
- Focus on hybrid scheduling and topology awareness

---

**Good luck with your Apple interview! 🍎**

*Remember: Show deep technical knowledge, acknowledge trade-offs, and demonstrate how you think about real-world scale challenges.*
