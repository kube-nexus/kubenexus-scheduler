# Operator CRD Support - Technical Deep Dive

## Overview

This document explains how KubeNexus Scheduler works with Kubernetes Operators and their Custom Resource Definitions (CRDs), such as Spark Operator, Kubeflow Training Operator, and others.

---

## How Schedulers Work with Operator CRDs

### The Key Insight: Schedulers Don't Schedule CRDs—They Schedule Pods!

**Critical Understanding:**
- Operators create CRDs (SparkApplication, PyTorchJob, etc.)
- Operators watch their CRDs and create **Pods** based on specs
- **Schedulers only schedule Pods, never CRDs**

```
User creates CRD
      ↓
Operator watches CRD
      ↓
Operator creates Pods (with labels from CRD)
      ↓
Scheduler schedules Pods ← KubeNexus works here!
```

### Example Flow

```yaml
# 1. User creates SparkApplication CRD
apiVersion: sparkoperator.k8s.io/v1beta2
kind: SparkApplication
metadata:
  name: spark-pi
spec:
  driver:
    labels:
      pod-group.scheduling.kubenexus.io/name: "spark-pi"
  executor:
    instances: 10
    labels:
      pod-group.scheduling.kubenexus.io/name: "spark-pi"

# 2. Spark Operator creates Pods
apiVersion: v1
kind: Pod
metadata:
  name: spark-pi-driver
  labels:
    pod-group.scheduling.kubenexus.io/name: "spark-pi"  # Inherited!
    spark-role: driver
spec:
  schedulerName: kubenexus-scheduler  # Set by operator or user

# 3. KubeNexus schedules the Pod (not the CRD!)
```

---

## How Production Schedulers Handle This

### Volcano Approach

**Volcano uses its own CRDs:**

```yaml
# Volcano's approach: Wrap everything in VolcanoJob CRD
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: spark-job
spec:
  minAvailable: 11
  schedulerName: volcano
  tasks:
  - replicas: 1
    name: driver
    template:
      spec:
        containers:
        - name: spark
  - replicas: 10
    name: executor
    template:
      spec:
        containers:
        - name: spark
```

**Pros:**
- ✅ Unified API for all workloads
- ✅ Rich gang scheduling features
- ✅ Status tracking and lifecycle management

**Cons:**
- ❌ Requires users to learn Volcano CRDs
- ❌ Doesn't work directly with Spark/Kubeflow operators
- ❌ Need integration layer (webhooks, controllers)

**Volcano + Spark Operator Integration:**
```yaml
# Spark Operator creates pods with Volcano labels
apiVersion: sparkoperator.k8s.io/v1beta2
kind: SparkApplication
spec:
  batchScheduler: "volcano"  # Operator injects volcano.sh/* labels
  driver:
    # Operator automatically adds:
    # labels:
    #   volcano.sh/job-name: spark-pi
    #   volcano.sh/queue-name: default
```

### YuniKorn Approach

**YuniKorn uses Application Abstraction:**

```yaml
# YuniKorn expects applicationId label on pods
labels:
  applicationId: "spark-app-001"
  queue: "root.default"
```

**Operator Integration:**
- Spark Operator: Built-in YuniKorn support (adds labels)
- Kubeflow: Training operator adds labels automatically
- Custom: Users add labels to CRD templates

### Kueue Approach

**Kueue uses Workload CRD:**

```yaml
# Kueue wraps other resources in a Workload
apiVersion: kueue.x-k8s.io/v1beta1
kind: Workload
metadata:
  name: spark-job
spec:
  podSets:
  - count: 11
    name: spark
```

**Integration:**
- Webhook intercepts Pod creation
- Creates Workload CRD automatically
- Manages queue admission

---

## KubeNexus Approach: Label-Based (Like YuniKorn)

### Philosophy

**"Work with existing operators, not against them"**

1. **No custom CRDs required** - Use standard Kubernetes labels
2. **Operator-agnostic** - Works with any operator that creates pods
3. **Opt-in** - Only affects pods with KubeNexus labels
4. **Simple** - No additional API objects to manage

### How It Works

```yaml
# Step 1: User configures Operator CRD with KubeNexus labels
apiVersion: sparkoperator.k8s.io/v1beta2
kind: SparkApplication
metadata:
  name: spark-pi
spec:
  driver:
    labels:
      pod-group.scheduling.kubenexus.io/name: "spark-pi"
      pod-group.scheduling.kubenexus.io/min-available: "11"
    schedulerName: kubenexus-scheduler
  executor:
    instances: 10
    labels:
      pod-group.scheduling.kubenexus.io/name: "spark-pi"
      pod-group.scheduling.kubenexus.io/min-available: "11"
    schedulerName: kubenexus-scheduler

# Step 2: Operator creates Pods with these labels
# (No changes to operator needed!)

# Step 3: KubeNexus scheduler sees the pods and applies gang scheduling
```

---

## Supported Operators

### ✅ Spark Operator

```yaml
apiVersion: sparkoperator.k8s.io/v1beta2
kind: SparkApplication
metadata:
  name: spark-pi
spec:
  driver:
    labels:
      pod-group.scheduling.kubenexus.io/name: "spark-pi"
      pod-group.scheduling.kubenexus.io/min-available: "11"
    schedulerName: kubenexus-scheduler
  executor:
    instances: 10
    labels:
      pod-group.scheduling.kubenexus.io/name: "spark-pi"
      pod-group.scheduling.kubenexus.io/min-available: "11"
    schedulerName: kubenexus-scheduler
```

**Works because:** Spark Operator passes labels from CRD to Pods

### ✅ Kubeflow Training Operator

```yaml
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: pytorch-mnist
spec:
  pytorchReplicaSpecs:
    Master:
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "mnist"
            pod-group.scheduling.kubenexus.io/min-available: "9"
        spec:
          schedulerName: kubenexus-scheduler
    Worker:
      replicas: 8
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "mnist"
            pod-group.scheduling.kubenexus.io/min-available: "9"
        spec:
          schedulerName: kubenexus-scheduler
```

**Works because:** Training Operator allows custom labels on pod templates

### ✅ Argo Workflows

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  name: distributed-workflow
spec:
  entrypoint: main
  templates:
  - name: main
    dag:
      tasks:
      - name: train
        templateRef:
          name: pytorch-training
          template: worker
        arguments:
          parameters:
          - name: replicas
            value: "8"
        # Gang scheduling via pod metadata
        podSpecPatch: |
          metadata:
            labels:
              pod-group.scheduling.kubenexus.io/name: "workflow-gang"
              pod-group.scheduling.kubenexus.io/min-available: "8"
          spec:
            schedulerName: kubenexus-scheduler
```

**Works because:** Argo allows pod spec patches

### ✅ Ray Operator

```yaml
apiVersion: ray.io/v1
kind: RayCluster
metadata:
  name: ray-cluster
spec:
  headGroupSpec:
    template:
      metadata:
        labels:
          pod-group.scheduling.kubenexus.io/name: "ray-cluster"
          pod-group.scheduling.kubenexus.io/min-available: "11"
      spec:
        schedulerName: kubenexus-scheduler
  workerGroupSpecs:
  - replicas: 10
    template:
      metadata:
        labels:
          pod-group.scheduling.kubenexus.io/name: "ray-cluster"
          pod-group.scheduling.kubenexus.io/min-available: "11"
      spec:
        schedulerName: kubenexus-scheduler
```

**Works because:** Ray Operator supports custom pod templates

---

## Design Comparison

| Approach | Volcano | YuniKorn | Kueue | KubeNexus |
|----------|---------|----------|-------|-----------|
| **CRD Required** | Yes (VolcanoJob) | No | Yes (Workload) | No |
| **Operator Integration** | Need webhooks | Labels only | Webhook | Labels only |
| **Works Today** | Need changes | ✅ Yes | Need changes | ✅ Yes |
| **Complexity** | High | Medium | Medium | Low |
| **Flexibility** | High | Medium | High | Medium |
| **Learning Curve** | Steep | Gentle | Steep | Gentle |

---

## Why KubeNexus Doesn't Need Its Own CRDs

### 1. Operators Already Create Pods

```
SparkApplication → Spark Operator → Driver Pod + Executor Pods
PyTorchJob → Training Operator → Master Pod + Worker Pods
Workflow → Argo → Task Pods
```

**KubeNexus insight:** We don't need to wrap these—just schedule the pods they create!

### 2. Labels Are Sufficient

```yaml
# All we need:
labels:
  pod-group.scheduling.kubenexus.io/name: "my-job"
  pod-group.scheduling.kubenexus.io/min-available: "8"

# This gives us:
✅ Gang scheduling
✅ Pod grouping
✅ Query capability (kubectl get pods -l pod-group.scheduling.kubenexus.io/name=my-job)
✅ Operator compatibility
```

### 3. CRDs Add Complexity Without Value (For Now)

**What CRDs would give us:**
- Validation (can use admission webhooks instead)
- Status tracking (operators already do this)
- Lifecycle management (operators already do this)

**What CRDs would cost us:**
- Installation complexity
- API version management
- Breaking changes risk
- Operator integration burden

### 4. Future Flexibility

**v1.0:** Labels only (current)
**v1.5:** Optional webhook for auto-injection
**v2.0:** Optional CRD for advanced features (backward compatible)

---

## Spark Operator Detailed Example

### Configuration

```yaml
apiVersion: sparkoperator.k8s.io/v1beta2
kind: SparkApplication
metadata:
  name: spark-pi
  namespace: spark-jobs
spec:
  type: Scala
  mode: cluster
  image: "gcr.io/spark-operator/spark:v3.5.0"
  imagePullPolicy: Always
  mainClass: org.apache.spark.examples.SparkPi
  mainApplicationFile: "local:///opt/spark/examples/jars/spark-examples.jar"
  
  # Spark configuration
  sparkVersion: "3.5.0"
  restartPolicy:
    type: Never
  
  # Driver configuration with KubeNexus labels
  driver:
    cores: 1
    coreLimit: "1200m"
    memory: "2g"
    labels:
      # Gang scheduling labels
      pod-group.scheduling.kubenexus.io/name: "spark-pi"
      pod-group.scheduling.kubenexus.io/min-available: "11"  # 1 driver + 10 executors
      version: "3.5.0"
      app: spark-pi
    annotations:
      # NUMA scheduling (optional)
      numa.scheduling.kubenexus.io/policy: "best-effort"
    serviceAccount: spark
    # Use KubeNexus scheduler
    schedulerName: kubenexus-scheduler
  
  # Executor configuration with KubeNexus labels
  executor:
    cores: 1
    instances: 10
    memory: "2g"
    labels:
      # MUST match driver gang name!
      pod-group.scheduling.kubenexus.io/name: "spark-pi"
      pod-group.scheduling.kubenexus.io/min-available: "11"
      version: "3.5.0"
      app: spark-pi
    annotations:
      numa.scheduling.kubenexus.io/policy: "best-effort"
    # Use KubeNexus scheduler
    schedulerName: kubenexus-scheduler
```

### What Happens

```
1. User creates SparkApplication CRD
   ↓
2. Spark Operator watches and validates
   ↓
3. Operator creates:
   - spark-pi-driver Pod (with gang labels)
   - spark-pi-exec-1 Pod (with gang labels)
   - spark-pi-exec-2 Pod (with gang labels)
   - ... (10 total executor pods)
   ↓
4. KubeNexus sees 11 pods with pod-group.scheduling.kubenexus.io/name=spark-pi
   ↓
5. KubeNexus waits until ALL 11 pods can be scheduled
   ↓
6. KubeNexus schedules all 11 pods atomically
   ↓
7. Spark job runs successfully!
```

### Verification

```bash
# Check SparkApplication
kubectl get sparkapplication -n spark-jobs

# Check pods (should all be scheduled together)
kubectl get pods -n spark-jobs -l pod-group.scheduling.kubenexus.io/name=spark-pi

# Check gang status
kubectl get pods -n spark-jobs -l pod-group.scheduling.kubenexus.io/name=spark-pi \
  -o custom-columns=NAME:.metadata.name,STATUS:.status.phase,NODE:.spec.nodeName

# All should show "Running" on nodes at the same time
```

---

## Migration Guide: From Other Schedulers

### From Volcano

**Volcano:**
```yaml
spec:
  batchScheduler: "volcano"
  batchSchedulerOptions:
    queue: default
    priorityClassName: high
```

**KubeNexus:**
```yaml
spec:
  driver:
    labels:
      pod-group.scheduling.kubenexus.io/name: "spark-pi"
      pod-group.scheduling.kubenexus.io/min-available: "11"
    schedulerName: kubenexus-scheduler
  executor:
    labels:
      pod-group.scheduling.kubenexus.io/name: "spark-pi"
      pod-group.scheduling.kubenexus.io/min-available: "11"
    schedulerName: kubenexus-scheduler
```

### From YuniKorn

**YuniKorn:**
```yaml
spec:
  driver:
    labels:
      applicationId: "spark-pi"
      queue: "root.spark"
    schedulerName: yunikorn
```

**KubeNexus:**
```yaml
spec:
  driver:
    labels:
      pod-group.scheduling.kubenexus.io/name: "spark-pi"
      pod-group.scheduling.kubenexus.io/min-available: "11"
    schedulerName: kubenexus-scheduler
```

---

## Future Enhancements

### v1.5: Admission Webhook (Planned)

Auto-inject labels into operator-created pods:

```yaml
# User creates simple SparkApplication
apiVersion: sparkoperator.k8s.io/v1beta2
kind: SparkApplication
metadata:
  name: spark-pi
  annotations:
    kubenexus.io/gang-scheduling: "true"  # Opt-in
spec:
  driver:
    schedulerName: kubenexus-scheduler
  executor:
    instances: 10
    schedulerName: kubenexus-scheduler

# Webhook automatically adds:
# labels:
#   pod-group.scheduling.kubenexus.io/name: "spark-pi"
#   pod-group.scheduling.kubenexus.io/min-available: "11"
```

### v2.0: Optional PodGroup CRD (Planned)

For advanced features:

```yaml
apiVersion: scheduling.kubenexus.io/v1alpha1
kind: PodGroup
metadata:
  name: spark-pi
spec:
  minMember: 11
  scheduleTimeoutSeconds: 300
  priorityClassName: high
  queue: spark-queue
```

**Backward compatible:** Labels still work!

---

## Summary

### How KubeNexus Works with Operator CRDs

1. ✅ **Operators create pods** from their CRDs
2. ✅ **Pods inherit labels** from CRD specs
3. ✅ **KubeNexus schedules pods** (not CRDs)
4. ✅ **No operator changes needed**

### Supported Operators (Out of the Box)

- ✅ Spark Operator
- ✅ Kubeflow Training Operator (PyTorchJob, TFJob, MPIJob, etc.)
- ✅ Argo Workflows
- ✅ Ray Operator
- ✅ Any operator that allows custom pod labels

### Design Philosophy

**"Schedulers schedule pods, not CRDs"**

This makes KubeNexus:
- Simple to use
- Compatible with existing operators
- Easy to adopt
- No vendor lock-in

---

*Last Updated: February 2026*
