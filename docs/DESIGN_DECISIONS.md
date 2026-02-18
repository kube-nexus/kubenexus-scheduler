# KubeNexus Scheduler - Design Decisions

## Overview

This document explains key design decisions for KubeNexus Scheduler, particularly around API design, Kubeflow integration, and configuration mechanisms.

---

## 1. Configuration Mechanism: Labels vs Annotations vs CRDs

### Current Approach

**Gang Scheduling**: Uses **Labels**
```yaml
labels:
  pod-group.scheduling.kubenexus.io/name: "training-job"
  pod-group.scheduling.kubenexus.io/min-available: "8"
```

**NUMA Scheduling**: Uses **Annotations**
```yaml
annotations:
  numa.scheduling.kubenexus.io/policy: "single-numa"
  numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu"
```

### Rationale

#### Why Labels for Gang Scheduling?

✅ **Advantages:**
1. **Queryable**: Can select pods by group name (`kubectl get pods -l pod-group.scheduling.kubenexus.io/name=my-job`)
2. **Native Kubernetes**: Works with ReplicaSets, Deployments, Jobs without extra tooling
3. **Simple**: No CRDs to install, no extra API objects
4. **Compatible**: Works with existing tools (Argo, Kubeflow, Spark Operator)

❌ **Disadvantages:**
1. Limited to string values
2. No validation (except via admission webhook)
3. Can't express complex policies

#### Why Annotations for NUMA Scheduling?

✅ **Advantages:**
1. **Non-identifying metadata**: NUMA policy doesn't need to be in pod identity
2. **Larger size limit**: Annotations support up to 256KB (vs 63 chars for label values)
3. **Tooling agnostic**: Won't interfere with label-based selectors
4. **Rich data**: Can store structured configuration (e.g., comma-separated resource list)

❌ **Disadvantages:**
1. Not queryable via label selectors
2. Harder to monitor/debug (can't easily list "all NUMA-aware pods")

#### Alternative: CRDs (Not Chosen)

We considered a CRD-based approach like Volcano's PodGroup:

```yaml
apiVersion: scheduling.kubenexus.io/v1alpha1
kind: PodGroup
metadata:
  name: training-job
spec:
  minMember: 8
  numaPolicy: single-numa
  resources: ["cpu", "memory", "nvidia.com/gpu"]
```

**Why we didn't choose this:**
- ❌ Requires CRD installation (operational overhead)
- ❌ Extra API object to manage (lifecycle complexity)
- ❌ Less compatible with existing tools
- ❌ More complex for users (2 objects instead of 1)
- ❌ Harder to integrate with Kubeflow/Argo/Spark operators

**When CRDs make sense:**
- Complex scheduling policies (e.g., custom fairness rules)
- Advanced features like gang hierarchies
- Need for status reporting and lifecycle management
- Multi-version API evolution

### Recommendation: Hybrid Approach (Current)

**Use labels when:**
- Need to select/query pods
- Part of pod identity
- Simple key-value pairs
- Need compatibility with K8s controllers

**Use annotations when:**
- Non-identifying metadata
- Richer/longer values
- Configuration that doesn't need querying
- Policy-based settings

**Consider CRDs when:**
- Need validation and admission control
- Complex nested structures
- Lifecycle management beyond pods
- Status reporting required

---

## 2. Kubeflow Integration

### Supported Kubeflow CRDs

KubeNexus automatically supports Kubeflow Training Operator CRDs:

1. **PyTorchJob** (`pytorch.kubeflow.org/v1`)
2. **TFJob** (`tensorflow.kubeflow.org/v1`)
3. **MPIJob** (`kubeflow.org/v2beta1`)
4. **PaddleJob** (`kubeflow.org/v1`)
5. **XGBoostJob** (`kubeflow.org/v1`)
6. **MXJob** (`kubeflow.org/v1`)

### How It Works

Kubeflow operators create pods with labels like:
```yaml
metadata:
  labels:
    training.kubeflow.org/job-name: "mnist-training"
    training.kubeflow.org/replica-type: "worker"
    training.kubeflow.org/replica-index: "0"
```

**KubeNexus detects these automatically** and applies gang scheduling if configured.

### Configuration Options

#### Option 1: Auto-Detection (Planned v1.5)

KubeNexus can auto-detect Kubeflow jobs and apply gang scheduling:

```yaml
# In scheduler config
pluginConfig:
  - name: Coscheduling
    args:
      kubeflowAutoGang: true  # Auto-enable gang for Kubeflow jobs
```

Pros: Zero configuration for users
Cons: Less explicit, assumes all Kubeflow jobs need gang scheduling

#### Option 2: Explicit Labels (Current, Recommended)

Users add KubeNexus labels to Kubeflow CRDs:

```yaml
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: mnist-training
spec:
  pytorchReplicaSpecs:
    Master:
      replicas: 1
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "mnist-training"
            pod-group.scheduling.kubenexus.io/min-available: "9"
    Worker:
      replicas: 8
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "mnist-training"
            pod-group.scheduling.kubenexus.io/min-available: "9"
        spec:
          schedulerName: kubenexus-scheduler
```

Pros: Explicit, flexible, works today
Cons: Requires user to set labels

#### Option 3: Mutating Webhook (Planned v1.5)

A webhook automatically injects KubeNexus labels into Kubeflow pods:

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: kubenexus-kubeflow-injector
webhooks:
  - name: inject-gang-labels.kubenexus.io
    rules:
      - apiGroups: ["kubeflow.org"]
        apiVersions: ["v1"]
        operations: ["CREATE"]
        resources: ["pytorchjobs", "tfjobs", "mpijobs"]
```

Pros: Automatic, user-friendly
Cons: Requires webhook infrastructure

### Recommendation

**Current (v1.0)**: Use Option 2 (Explicit Labels)
- Document in Kubeflow examples
- Provide templates/helm values

**Future (v1.5)**: Add Option 3 (Webhook)
- Optional feature for better UX
- Auto-calculate minAvailable from replica counts

---

## 3. Label Compatibility

### Multiple Label Support

KubeNexus supports multiple label formats for compatibility:

```go
// Priority order (first match wins):
const (
    // KubeNexus native (preferred)
    PodGroupNameLabel = "pod-group.scheduling.kubenexus.io/name"
    PodGroupMinAvailableLabel = "pod-group.scheduling.kubenexus.io/min-available"
    
    // Kubernetes SIG scheduler-plugins compatible
    PodGroupSigLabel = "pod-group.scheduling.sigs.k8s.io/name"
    PodGroupMinMemberLabel = "pod-group.scheduling.sigs.k8s.io/min-member"
    
    // Volcano compatible (future)
    VolcanoJobLabel = "volcano.sh/job-name"
)
```

This allows users to migrate from other schedulers without relabeling all workloads.

---

## 4. Design Principles

### Simplicity First
- Prefer labels over CRDs when possible
- Single pod annotation over separate config objects
- Works with standard Kubernetes primitives

### Compatibility
- Support standard Kubernetes SIG labels
- Work with existing operators (Kubeflow, Spark, Argo)
- Allow migration from other schedulers

### Explicit Over Implicit
- Users opt-in to features via labels/annotations
- No "magic" auto-detection by default
- Clear failure modes and error messages

### Performance
- Label-based selection is fast (indexed in etcd)
- No extra API calls for CRD lookups
- Minimal scheduler overhead

---

## 5. Future Considerations

### When to Add CRDs

Consider adding a `PodGroup` CRD if we need:

1. **Advanced Features:**
   - Gang hierarchies (parent-child relationships)
   - Queue management with priorities
   - Fair-share policies across groups
   - Resource borrowing/lending

2. **Status Reporting:**
   - PodGroup phase (Pending, Running, Completed)
   - Resource allocation tracking
   - Historical metrics

3. **Validation:**
   - Admission control for gang size limits
   - NUMA policy validation against node topology
   - Resource request validation

4. **Multi-Scheduler Coordination:**
   - Gang scheduling across multiple schedulers
   - Cluster-wide gang tracking
   - Cross-namespace coordination

### Graduated Approach

**v1.0 (Current)**: Labels + Annotations only
**v1.5**: Optional CRD for advanced features (backward compatible)
**v2.0**: CRD becomes recommended, labels still supported

---

## 6. Examples

### Kubeflow PyTorchJob with Gang Scheduling

```yaml
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: distributed-mnist
  namespace: ml-workloads
spec:
  pytorchReplicaSpecs:
    Master:
      replicas: 1
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "mnist-gang"
            pod-group.scheduling.kubenexus.io/min-available: "9"
          annotations:
            numa.scheduling.kubenexus.io/policy: "single-numa"
            numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu"
        spec:
          schedulerName: kubenexus-scheduler
          containers:
          - name: pytorch
            image: pytorch/pytorch:latest
            resources:
              requests:
                cpu: "4"
                memory: "16Gi"
                nvidia.com/gpu: "1"
    Worker:
      replicas: 8
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "mnist-gang"
            pod-group.scheduling.kubenexus.io/min-available: "9"
          annotations:
            numa.scheduling.kubenexus.io/policy: "single-numa"
            numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu"
        spec:
          schedulerName: kubenexus-scheduler
          containers:
          - name: pytorch
            image: pytorch/pytorch:latest
            resources:
              requests:
                cpu: "4"
                memory: "16Gi"
                nvidia.com/gpu: "1"
```

### Spark with KubeNexus

```yaml
apiVersion: sparkoperator.k8s.io/v1beta2
kind: SparkApplication
metadata:
  name: spark-pi
  namespace: spark-jobs
spec:
  driver:
    labels:
      pod-group.scheduling.kubenexus.io/name: "spark-pi-gang"
      pod-group.scheduling.kubenexus.io/min-available: "11"
    schedulerName: kubenexus-scheduler
  executor:
    instances: 10
    labels:
      pod-group.scheduling.kubenexus.io/name: "spark-pi-gang"
      pod-group.scheduling.kubenexus.io/min-available: "11"
    schedulerName: kubenexus-scheduler
```

---

## 7. Migration Guide

### From Volcano

```yaml
# Volcano
volcano.sh/job-name: "my-job"
volcano.sh/queue-name: "default"

# KubeNexus equivalent
pod-group.scheduling.kubenexus.io/name: "my-job"
pod-group.scheduling.kubenexus.io/min-available: "8"
```

### From Kubernetes SIG Scheduler Plugins

```yaml
# SIG Scheduler Plugins
pod-group.scheduling.sigs.k8s.io/name: "my-job"
pod-group.scheduling.sigs.k8s.io/min-member: "8"

# KubeNexus (both formats supported!)
pod-group.scheduling.kubenexus.io/name: "my-job"
pod-group.scheduling.kubenexus.io/min-available: "8"
```

---

## Summary

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Gang scheduling config | Labels | Queryable, simple, compatible |
| NUMA config | Annotations | Non-identifying, rich values |
| Kubeflow support | Explicit labels (v1.0), Webhook (v1.5) | Works today, better UX later |
| CRDs | No (v1.0), Optional (v1.5+) | Simplicity first, add complexity when needed |
| Compatibility | Support multiple label formats | Easy migration from other schedulers |

---

*Last Updated: February 2026*
