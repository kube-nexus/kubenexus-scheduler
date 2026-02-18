# Kubeflow Integration Examples

This guide shows how to use KubeNexus Scheduler with Kubeflow Training Operator workloads.

---

## Prerequisites

1. Kubernetes cluster with KubeNexus Scheduler installed
2. Kubeflow Training Operator installed:
   ```bash
   kubectl apply -k "github.com/kubeflow/training-operator/manifests/overlays/standalone?ref=v1.7.0"
   ```

---

## Supported Kubeflow CRDs

- ✅ **PyTorchJob** - Distributed PyTorch training
- ✅ **TFJob** - TensorFlow distributed training
- ✅ **MPIJob** - MPI-based training (Horovod, DeepSpeed)
- ✅ **XGBoostJob** - XGBoost distributed training
- ✅ **PaddleJob** - PaddlePaddle training
- ✅ **MXJob** - MXNet training

---

## PyTorchJob Examples

### Basic PyTorchJob with Gang Scheduling

```yaml
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: pytorch-mnist
  namespace: ml-training
spec:
  pytorchReplicaSpecs:
    Master:
      replicas: 1
      template:
        metadata:
          labels:
            # Gang scheduling: all 9 pods (1 master + 8 workers) start together
            pod-group.scheduling.kubenexus.io/name: "pytorch-mnist"
            pod-group.scheduling.kubenexus.io/min-available: "9"
        spec:
          schedulerName: kubenexus-scheduler
          containers:
          - name: pytorch
            image: kubeflow/pytorch-mnist:latest
            resources:
              requests:
                cpu: "2"
                memory: "8Gi"
              limits:
                cpu: "2"
                memory: "8Gi"
    Worker:
      replicas: 8
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "pytorch-mnist"
            pod-group.scheduling.kubenexus.io/min-available: "9"
        spec:
          schedulerName: kubenexus-scheduler
          containers:
          - name: pytorch
            image: kubeflow/pytorch-mnist:latest
            resources:
              requests:
                cpu: "4"
                memory: "16Gi"
              limits:
                cpu: "4"
                memory: "16Gi"
```

### PyTorchJob with NUMA-Aware GPU Scheduling

```yaml
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: pytorch-bert-gpu
  namespace: ml-training
spec:
  pytorchReplicaSpecs:
    Master:
      replicas: 1
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "bert-training"
            pod-group.scheduling.kubenexus.io/min-available: "5"
          annotations:
            # NUMA-aware: place CPU, memory, and GPU on same NUMA node
            numa.scheduling.kubenexus.io/policy: "single-numa"
            numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu"
        spec:
          schedulerName: kubenexus-scheduler
          containers:
          - name: pytorch
            image: nvcr.io/nvidia/pytorch:23.10-py3
            command: ["python", "train_bert.py"]
            resources:
              requests:
                cpu: "8"
                memory: "32Gi"
                nvidia.com/gpu: "1"
              limits:
                nvidia.com/gpu: "1"
    Worker:
      replicas: 4
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "bert-training"
            pod-group.scheduling.kubenexus.io/min-available: "5"
          annotations:
            numa.scheduling.kubenexus.io/policy: "single-numa"
            numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu"
        spec:
          schedulerName: kubenexus-scheduler
          containers:
          - name: pytorch
            image: nvcr.io/nvidia/pytorch:23.10-py3
            command: ["python", "train_bert.py"]
            resources:
              requests:
                cpu: "16"
                memory: "64Gi"
                nvidia.com/gpu: "2"
              limits:
                nvidia.com/gpu: "2"
```

---

## TensorFlow Job (TFJob)

### Distributed TensorFlow Training

```yaml
apiVersion: kubeflow.org/v1
kind: TFJob
metadata:
  name: tensorflow-mnist
  namespace: ml-training
spec:
  tfReplicaSpecs:
    Chief:
      replicas: 1
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "tf-mnist"
            pod-group.scheduling.kubenexus.io/min-available: "5"
        spec:
          schedulerName: kubenexus-scheduler
          containers:
          - name: tensorflow
            image: tensorflow/tensorflow:latest-gpu
            command: ["python", "train.py", "--job-type=chief"]
            resources:
              requests:
                cpu: "4"
                memory: "16Gi"
                nvidia.com/gpu: "1"
              limits:
                nvidia.com/gpu: "1"
    Worker:
      replicas: 4
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "tf-mnist"
            pod-group.scheduling.kubenexus.io/min-available: "5"
        spec:
          schedulerName: kubenexus-scheduler
          containers:
          - name: tensorflow
            image: tensorflow/tensorflow:latest-gpu
            command: ["python", "train.py", "--job-type=worker"]
            resources:
              requests:
                cpu: "4"
                memory: "16Gi"
                nvidia.com/gpu: "1"
              limits:
                nvidia.com/gpu: "1"
```

---

## MPI Job (Horovod)

### Horovod Distributed Training

```yaml
apiVersion: kubeflow.org/v2beta1
kind: MPIJob
metadata:
  name: horovod-training
  namespace: ml-training
spec:
  slotsPerWorker: 1
  runPolicy:
    cleanPodPolicy: Running
  mpiReplicaSpecs:
    Launcher:
      replicas: 1
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "horovod-job"
            pod-group.scheduling.kubenexus.io/min-available: "9"
        spec:
          schedulerName: kubenexus-scheduler
          containers:
          - name: mpi-launcher
            image: horovod/horovod:latest
            command:
            - mpirun
            - --allow-run-as-root
            - -np
            - "8"
            - -bind-to
            - none
            - -map-by
            - slot
            - python
            - train.py
            resources:
              requests:
                cpu: "1"
                memory: "4Gi"
    Worker:
      replicas: 8
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "horovod-job"
            pod-group.scheduling.kubenexus.io/min-available: "9"
          annotations:
            numa.scheduling.kubenexus.io/policy: "single-numa"
            numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu"
        spec:
          schedulerName: kubenexus-scheduler
          containers:
          - name: mpi-worker
            image: horovod/horovod:latest
            resources:
              requests:
                cpu: "16"
                memory: "64Gi"
                nvidia.com/gpu: "2"
              limits:
                nvidia.com/gpu: "2"
```

---

## XGBoost Job

### Distributed XGBoost Training

```yaml
apiVersion: kubeflow.org/v1
kind: XGBoostJob
metadata:
  name: xgboost-training
  namespace: ml-training
spec:
  xgbReplicaSpecs:
    Master:
      replicas: 1
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "xgboost-job"
            pod-group.scheduling.kubenexus.io/min-available: "5"
        spec:
          schedulerName: kubenexus-scheduler
          containers:
          - name: xgboost
            image: docker.io/kubeflow/xgboost-dist-iris:latest
            args: ["--job_type=Master"]
            resources:
              requests:
                cpu: "2"
                memory: "8Gi"
    Worker:
      replicas: 4
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "xgboost-job"
            pod-group.scheduling.kubenexus.io/min-available: "5"
        spec:
          schedulerName: kubenexus-scheduler
          containers:
          - name: xgboost
            image: docker.io/kubeflow/xgboost-dist-iris:latest
            args: ["--job_type=Worker"]
            resources:
              requests:
                cpu: "4"
                memory: "16Gi"
```

---

## Best Practices

### 1. Calculate minAvailable Correctly

```yaml
# Formula: minAvailable = masters + workers + launchers
#
# PyTorchJob with 1 master + 8 workers:
pod-group.scheduling.kubenexus.io/min-available: "9"

# TFJob with 1 chief + 4 workers + 2 PS:
pod-group.scheduling.kubenexus.io/min-available: "7"

# MPIJob with 1 launcher + 8 workers:
pod-group.scheduling.kubenexus.io/min-available: "9"
```

### 2. Use Same Gang Name Across Replicas

```yaml
# ✅ Good: Same gang name for all replica types
Master:
  template:
    metadata:
      labels:
        pod-group.scheduling.kubenexus.io/name: "my-job"

Worker:
  template:
    metadata:
      labels:
        pod-group.scheduling.kubenexus.io/name: "my-job"  # Same!

# ❌ Bad: Different gang names
Master:
  template:
    metadata:
      labels:
        pod-group.scheduling.kubenexus.io/name: "my-job-master"

Worker:
  template:
    metadata:
      labels:
        pod-group.scheduling.kubenexus.io/name: "my-job-worker"  # Different!
```

### 3. NUMA Policy Selection

```yaml
# GPU workloads: Use single-numa
annotations:
  numa.scheduling.kubenexus.io/policy: "single-numa"
  numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu"

# CPU-only workloads: Use restricted or best-effort
annotations:
  numa.scheduling.kubenexus.io/policy: "restricted"
  numa.scheduling.kubenexus.io/resources: "cpu,memory"

# Ultra-performance: Use isolated (exclusive NUMA node)
annotations:
  numa.scheduling.kubenexus.io/policy: "isolated"
  numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu"
```

### 4. Set schedulerName Correctly

```yaml
# ✅ Required: Set schedulerName for KubeNexus
spec:
  pytorchReplicaSpecs:
    Master:
      template:
        spec:
          schedulerName: kubenexus-scheduler  # Required!

# ❌ Missing: Will use default scheduler (no gang scheduling)
spec:
  pytorchReplicaSpecs:
    Master:
      template:
        spec:
          # Missing schedulerName!
```

---

## Troubleshooting

### Issue: Pods Stuck in Pending

```bash
# Check pod status
kubectl get pods -n ml-training -l pod-group.scheduling.kubenexus.io/name=my-job

# Check events
kubectl describe pod <pod-name> -n ml-training

# Common causes:
# 1. Not all pods created yet (wait for gang to form)
# 2. Insufficient cluster resources
# 3. NUMA constraints too strict
# 4. Wrong minAvailable count
```

### Issue: Only Some Pods Scheduled

```bash
# Verify all pods have same gang name
kubectl get pods -n ml-training -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.labels.pod-group\.scheduling\.kubenexus\.io/name}{"\n"}{end}'

# Verify schedulerName is set
kubectl get pods -n ml-training -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.schedulerName}{"\n"}{end}'
```

### Issue: NUMA Scheduling Not Working

```bash
# Check if nodes have NUMA labels
kubectl get nodes -o json | jq '.items[].metadata.labels' | grep numa

# Label nodes if missing
kubectl apply -f deploy/numa-labeler-daemonset.yaml

# Or manually
kubectl label node <node-name> \
  numa.kubenexus.io/node-0-cpus="0-15" \
  numa.kubenexus.io/node-0-memory="64Gi"
```

---

## Monitoring

### Check Gang Status

```bash
# List all pods in a gang
kubectl get pods -l pod-group.scheduling.kubenexus.io/name=<gang-name>

# Count ready pods
kubectl get pods -l pod-group.scheduling.kubenexus.io/name=<gang-name> \
  --field-selector=status.phase=Running --no-headers | wc -l

# Watch gang formation
watch 'kubectl get pods -l pod-group.scheduling.kubenexus.io/name=<gang-name>'
```

### Check Scheduler Logs

```bash
# View KubeNexus scheduler logs
kubectl logs -n kube-system -l app=kubenexus-scheduler -f

# Filter for specific job
kubectl logs -n kube-system -l app=kubenexus-scheduler | grep <gang-name>
```

---

## Migration from Default Scheduler

To migrate existing Kubeflow jobs to KubeNexus:

1. **Add gang labels** to all replica specs
2. **Set schedulerName** to `kubenexus-scheduler`
3. **Add NUMA annotations** (optional, for GPU jobs)
4. **Update minAvailable** to match total replicas

**Before:**
```yaml
apiVersion: kubeflow.org/v1
kind: PyTorchJob
spec:
  pytorchReplicaSpecs:
    Master:
      replicas: 1
    Worker:
      replicas: 8
```

**After:**
```yaml
apiVersion: kubeflow.org/v1
kind: PyTorchJob
spec:
  pytorchReplicaSpecs:
    Master:
      replicas: 1
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "my-job"
            pod-group.scheduling.kubenexus.io/min-available: "9"
        spec:
          schedulerName: kubenexus-scheduler
    Worker:
      replicas: 8
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "my-job"
            pod-group.scheduling.kubenexus.io/min-available: "9"
        spec:
          schedulerName: kubenexus-scheduler
```

---

## Additional Resources

- **Kubeflow Training Operator**: https://github.com/kubeflow/training-operator
- **KubeNexus User Guide**: [USER_GUIDE.md](USER_GUIDE.md)
- **NUMA Scheduling**: [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md)
- **Design Decisions**: [DESIGN_DECISIONS.md](DESIGN_DECISIONS.md)

---

*Last Updated: February 2026*
