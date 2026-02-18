# Spark Operator Integration Guide

Complete guide for using KubeNexus Scheduler with Spark Operator on Kubernetes.

---

## Prerequisites

1. Kubernetes cluster (1.28+)
2. KubeNexus Scheduler installed
3. Spark Operator installed

### Install Spark Operator

```bash
# Install Spark Operator via Helm
helm repo add spark-operator https://kubeflow.github.io/spark-operator
helm install spark-operator spark-operator/spark-operator \
  --namespace spark-operator \
  --create-namespace \
  --set webhook.enable=true

# Verify installation
kubectl get pods -n spark-operator
```

---

## Basic Usage

### Simple Spark Job with Gang Scheduling

```yaml
apiVersion: sparkoperator.k8s.io/v1beta2
kind: SparkApplication
metadata:
  name: spark-pi
  namespace: default
spec:
  type: Scala
  mode: cluster
  image: "gcr.io/spark-operator/spark:v3.5.0"
  imagePullPolicy: Always
  mainClass: org.apache.spark.examples.SparkPi
  mainApplicationFile: "local:///opt/spark/examples/jars/spark-examples.jar"
  arguments:
    - "1000"
  
  sparkVersion: "3.5.0"
  restartPolicy:
    type: Never
  
  # Driver configuration
  driver:
    cores: 1
    coreLimit: "1200m"
    memory: "512m"
    labels:
      # Gang scheduling: wait for all pods
      pod-group.scheduling.kubenexus.io/name: "spark-pi"
      pod-group.scheduling.kubenexus.io/min-available: "4"  # 1 driver + 3 executors
    serviceAccount: spark
    schedulerName: kubenexus-scheduler
  
  # Executor configuration
  executor:
    cores: 1
    instances: 3
    memory: "512m"
    labels:
      # MUST match driver gang name
      pod-group.scheduling.kubenexus.io/name: "spark-pi"
      pod-group.scheduling.kubenexus.io/min-available: "4"
    schedulerName: kubenexus-scheduler
```

### Apply and Monitor

```bash
# Create the Spark job
kubectl apply -f spark-pi.yaml

# Watch pods being created
kubectl get pods -l pod-group.scheduling.kubenexus.io/name=spark-pi --watch

# Check SparkApplication status
kubectl get sparkapplication spark-pi

# View logs
kubectl logs spark-pi-driver
```

---

## Production Examples

### Example 1: Large-Scale Data Processing

```yaml
apiVersion: sparkoperator.k8s.io/v1beta2
kind: SparkApplication
metadata:
  name: data-processing
  namespace: spark-jobs
spec:
  type: PySpark
  pythonVersion: "3"
  mode: cluster
  image: "gcr.io/spark-operator/spark-py:v3.5.0"
  mainApplicationFile: "s3a://my-bucket/jobs/process_data.py"
  
  sparkVersion: "3.5.0"
  restartPolicy:
    type: OnFailure
    onFailureRetries: 3
    onFailureRetryInterval: 10
    onSubmissionFailureRetries: 5
    onSubmissionFailureRetryInterval: 20
  
  # Spark configuration
  sparkConf:
    "spark.dynamicAllocation.enabled": "false"
    "spark.sql.shuffle.partitions": "200"
    "spark.executor.memoryOverhead": "1g"
  
  # Hadoop configuration for S3
  hadoopConf:
    "fs.s3a.impl": "org.apache.hadoop.fs.s3a.S3AFileSystem"
    "fs.s3a.aws.credentials.provider": "com.amazonaws.auth.DefaultAWSCredentialsProviderChain"
  
  # Driver pod
  driver:
    cores: 2
    coreLimit: "2000m"
    memory: "4g"
    labels:
      pod-group.scheduling.kubenexus.io/name: "data-processing"
      pod-group.scheduling.kubenexus.io/min-available: "21"  # 1 + 20
      app: spark
      job: data-processing
    serviceAccount: spark
    schedulerName: kubenexus-scheduler
    volumeMounts:
      - name: spark-data
        mountPath: /data
    env:
      - name: AWS_REGION
        value: "us-east-1"
  
  # Executor pods
  executor:
    cores: 2
    instances: 20
    memory: "8g"
    memoryOverhead: "1g"
    labels:
      pod-group.scheduling.kubenexus.io/name: "data-processing"
      pod-group.scheduling.kubenexus.io/min-available: "21"
      app: spark
      job: data-processing
    schedulerName: kubenexus-scheduler
    volumeMounts:
      - name: spark-data
        mountPath: /data
    env:
      - name: AWS_REGION
        value: "us-east-1"
  
  volumes:
    - name: spark-data
      emptyDir: {}
```

### Example 2: Spark with GPU and NUMA-Aware Scheduling

```yaml
apiVersion: sparkoperator.k8s.io/v1beta2
kind: SparkApplication
metadata:
  name: spark-ml-gpu
  namespace: spark-jobs
spec:
  type: Python
  pythonVersion: "3"
  mode: cluster
  image: "nvidia/cuda:11.8.0-runtime-ubuntu20.04"
  imagePullPolicy: Always
  mainApplicationFile: "s3a://my-bucket/ml_training.py"
  
  sparkVersion: "3.5.0"
  sparkConf:
    "spark.task.resource.gpu.amount": "1"
    "spark.executor.resource.gpu.amount": "1"
    "spark.plugins": "com.nvidia.spark.SQLPlugin"
  
  driver:
    cores: 4
    coreLimit: "4000m"
    memory: "8g"
    labels:
      pod-group.scheduling.kubenexus.io/name: "spark-ml-gpu"
      pod-group.scheduling.kubenexus.io/min-available: "9"  # 1 + 8
    annotations:
      # NUMA-aware scheduling for GPU
      numa.scheduling.kubenexus.io/policy: "single-numa"
      numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu"
    serviceAccount: spark
    schedulerName: kubenexus-scheduler
    gpu:
      name: "nvidia.com/gpu"
      quantity: 1
  
  executor:
    cores: 4
    instances: 8
    memory: "16g"
    labels:
      pod-group.scheduling.kubenexus.io/name: "spark-ml-gpu"
      pod-group.scheduling.kubenexus.io/min-available: "9"
    annotations:
      numa.scheduling.kubenexus.io/policy: "single-numa"
      numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu"
    schedulerName: kubenexus-scheduler
    gpu:
      name: "nvidia.com/gpu"
      quantity: 1
```

### Example 3: Spark Structured Streaming

```yaml
apiVersion: sparkoperator.k8s.io/v1beta2
kind: SparkApplication
metadata:
  name: kafka-stream-processor
  namespace: spark-jobs
spec:
  type: Scala
  mode: cluster
  image: "my-registry/spark-streaming:3.5.0"
  mainClass: com.mycompany.KafkaStreamProcessor
  mainApplicationFile: "local:///opt/spark/jars/stream-processor.jar"
  
  sparkVersion: "3.5.0"
  restartPolicy:
    type: Always  # For streaming jobs
  
  sparkConf:
    "spark.streaming.stopGracefullyOnShutdown": "true"
    "spark.sql.streaming.checkpointLocation": "s3a://my-bucket/checkpoints"
  
  driver:
    cores: 2
    memory: "2g"
    labels:
      pod-group.scheduling.kubenexus.io/name: "kafka-stream"
      pod-group.scheduling.kubenexus.io/min-available: "6"  # 1 + 5
    schedulerName: kubenexus-scheduler
  
  executor:
    cores: 2
    instances: 5
    memory: "4g"
    labels:
      pod-group.scheduling.kubenexus.io/name: "kafka-stream"
      pod-group.scheduling.kubenexus.io/min-available: "6"
    schedulerName: kubenexus-scheduler
```

---

## Best Practices

### 1. Calculate minAvailable Correctly

```yaml
# Formula: minAvailable = 1 (driver) + executor.instances

# Example with 10 executors:
driver:
  labels:
    pod-group.scheduling.kubenexus.io/min-available: "11"  # 1 + 10

executor:
  instances: 10
  labels:
    pod-group.scheduling.kubenexus.io/min-available: "11"  # Same!
```

### 2. Use Consistent Gang Names

```yaml
# ✅ Good: Same gang name
driver:
  labels:
    pod-group.scheduling.kubenexus.io/name: "my-job"
executor:
  labels:
    pod-group.scheduling.kubenexus.io/name: "my-job"  # Match!

# ❌ Bad: Different names
driver:
  labels:
    pod-group.scheduling.kubenexus.io/name: "my-job-driver"
executor:
  labels:
    pod-group.scheduling.kubenexus.io/name: "my-job-executor"  # Won't gang!
```

### 3. Set schedulerName for Both Driver and Executors

```yaml
driver:
  schedulerName: kubenexus-scheduler  # Required!
executor:
  schedulerName: kubenexus-scheduler  # Required!
```

### 4. Use Resource Requests

```yaml
driver:
  cores: 2
  coreLimit: "2000m"  # Set limit slightly higher
  memory: "4g"

executor:
  cores: 4
  memory: "8g"
  memoryOverhead: "1g"  # Add overhead for JVM
```

### 5. Configure Retries Appropriately

```yaml
restartPolicy:
  type: OnFailure
  onFailureRetries: 3  # Retry failed executors
  onFailureRetryInterval: 10  # Wait 10s between retries
  onSubmissionFailureRetries: 5
  onSubmissionFailureRetryInterval: 20
```

---

## Troubleshooting

### Issue: Pods Stuck in Pending

**Symptom:**
```bash
$ kubectl get pods -l pod-group.scheduling.kubenexus.io/name=spark-pi
NAME                  READY   STATUS    RESTARTS   AGE
spark-pi-driver       0/1     Pending   0          5m
spark-pi-exec-1       0/1     Pending   0          5m
spark-pi-exec-2       0/1     Pending   0          5m
```

**Causes & Solutions:**

1. **Waiting for gang to form**
   ```bash
   # Check if all pods are created
   kubectl get pods -l pod-group.scheduling.kubenexus.io/name=spark-pi
   
   # If count < minAvailable, wait for operator to create all pods
   ```

2. **Insufficient cluster resources**
   ```bash
   # Check node resources
   kubectl describe nodes | grep -A 5 "Allocated resources"
   
   # Solution: Add nodes or reduce resource requests
   ```

3. **Wrong minAvailable count**
   ```bash
   # Verify minAvailable matches reality
   # Should be: 1 (driver) + executor.instances
   
   # Fix in SparkApplication and reapply
   ```

4. **Scheduler not running**
   ```bash
   # Check KubeNexus scheduler
   kubectl get pods -n kube-system -l app=kubenexus-scheduler
   
   # Check logs
   kubectl logs -n kube-system -l app=kubenexus-scheduler
   ```

### Issue: Only Driver Scheduled

**Symptom:**
```bash
$ kubectl get pods
NAME                  READY   STATUS    NODE
spark-pi-driver       1/1     Running   node-1
spark-pi-exec-1       0/1     Pending   <none>
spark-pi-exec-2       0/1     Pending   <none>
```

**Cause:** Different gang names or missing schedulerName

**Solution:**
```yaml
# Ensure BOTH have same gang name and schedulerName
driver:
  labels:
    pod-group.scheduling.kubenexus.io/name: "spark-pi"
  schedulerName: kubenexus-scheduler

executor:
  labels:
    pod-group.scheduling.kubenexus.io/name: "spark-pi"  # Must match!
  schedulerName: kubenexus-scheduler  # Required!
```

### Issue: SparkApplication Fails to Start

**Check operator logs:**
```bash
kubectl logs -n spark-operator deployment/spark-operator-controller
```

**Common issues:**
- Invalid SparkApplication spec
- Missing service account
- Image pull errors
- RBAC permissions

### Issue: Executors Crash After Start

**Not a scheduling issue!** This is application-level:

```bash
# Check executor logs
kubectl logs spark-pi-exec-1

# Common causes:
# - Out of memory (increase executor.memory)
# - Missing dependencies
# - Configuration errors
# - Data issues
```

---

## Monitoring and Debugging

### Check Gang Status

```bash
# List all pods in gang
kubectl get pods -l pod-group.scheduling.kubenexus.io/name=<gang-name>

# Check which are scheduled
kubectl get pods -l pod-group.scheduling.kubenexus.io/name=<gang-name> \
  -o custom-columns=NAME:.metadata.name,STATUS:.status.phase,NODE:.spec.nodeName

# Count scheduled pods
kubectl get pods -l pod-group.scheduling.kubenexus.io/name=<gang-name> \
  --field-selector=status.phase=Running --no-headers | wc -l
```

### View Spark UI

```bash
# Port-forward to driver
kubectl port-forward spark-pi-driver 4040:4040

# Open in browser
open http://localhost:4040
```

### Check Scheduler Decisions

```bash
# View scheduler logs for specific job
kubectl logs -n kube-system -l app=kubenexus-scheduler | grep spark-pi

# Check pod events
kubectl describe pod spark-pi-driver | grep -A 10 Events
```

### Metrics

```bash
# Spark metrics (if enabled)
kubectl get --raw /api/v1/namespaces/spark-jobs/services/spark-pi-driver-svc:4040/proxy/metrics/json

# KubeNexus metrics
kubectl get --raw /api/v1/namespaces/kube-system/services/kubenexus-scheduler:8080/metrics
```

---

## Performance Tuning

### 1. Right-Size Resources

```yaml
# Start conservative
executor:
  cores: 4
  memory: "8g"
  instances: 10

# Monitor and adjust based on:
# - CPU utilization: kubectl top pods
# - Memory usage: Check Spark UI
# - Task completion time: Spark UI stages
```

### 2. Use NUMA for GPU Workloads

```yaml
executor:
  annotations:
    numa.scheduling.kubenexus.io/policy: "single-numa"
    numa.scheduling.kubenexus.io/resources: "cpu,memory,nvidia.com/gpu"
  gpu:
    name: "nvidia.com/gpu"
    quantity: 1
```

### 3. Configure Spark Properly

```yaml
sparkConf:
  # Parallelism
  "spark.default.parallelism": "200"  # 2-3x executor cores
  "spark.sql.shuffle.partitions": "200"
  
  # Memory
  "spark.executor.memoryOverhead": "1g"  # 10-15% of executor memory
  "spark.memory.fraction": "0.8"
  
  # Shuffle
  "spark.shuffle.service.enabled": "false"  # Not needed with fixed executors
  "spark.dynamicAllocation.enabled": "false"  # Gang scheduling incompatible
```

---

## Advanced Configuration

### Node Affinity

```yaml
driver:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key: node-type
            operator: In
            values:
            - spark-driver

executor:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key: node-type
            operator: In
            values:
            - spark-executor
```

### Tolerations

```yaml
driver:
  tolerations:
  - key: "spark"
    operator: "Equal"
    value: "true"
    effect: "NoSchedule"

executor:
  tolerations:
  - key: "spark"
    operator: "Equal"
    value: "true"
    effect: "NoSchedule"
```

### Priority Classes

```yaml
# Create priority class
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: spark-high-priority
value: 1000000
globalDefault: false
description: "High priority for critical Spark jobs"
---
# Use in SparkApplication
driver:
  priorityClassName: spark-high-priority
executor:
  priorityClassName: spark-high-priority
```

---

## Migration from Default Scheduler

### Before (Default Scheduler)

```yaml
apiVersion: sparkoperator.k8s.io/v1beta2
kind: SparkApplication
spec:
  driver:
    cores: 1
    memory: "1g"
  executor:
    instances: 5
    cores: 1
    memory: "1g"
```

### After (KubeNexus with Gang Scheduling)

```yaml
apiVersion: sparkoperator.k8s.io/v1beta2
kind: SparkApplication
spec:
  driver:
    cores: 1
    memory: "1g"
    labels:
      pod-group.scheduling.kubenexus.io/name: "my-job"
      pod-group.scheduling.kubenexus.io/min-available: "6"  # 1 + 5
    schedulerName: kubenexus-scheduler
  executor:
    instances: 5
    cores: 1
    memory: "1g"
    labels:
      pod-group.scheduling.kubenexus.io/name: "my-job"
      pod-group.scheduling.kubenexus.io/min-available: "6"
    schedulerName: kubenexus-scheduler
```

**Benefits:**
- ✅ No partial scheduling (all-or-nothing)
- ✅ No resource waste from stuck executors
- ✅ Faster job start (all pods ready together)
- ✅ Better cluster utilization

---

## Additional Resources

- **Spark Operator Docs**: https://github.com/kubeflow/spark-operator
- **KubeNexus User Guide**: [USER_GUIDE.md](USER_GUIDE.md)
- **Operator CRD Support**: [OPERATOR_CRD_SUPPORT.md](OPERATOR_CRD_SUPPORT.md)
- **Kubeflow Integration**: [KUBEFLOW_INTEGRATION.md](KUBEFLOW_INTEGRATION.md)

---

*Last Updated: February 2026*
