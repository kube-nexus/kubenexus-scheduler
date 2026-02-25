# Testing KubeNexus Scheduler

This guide walks through testing the KubeNexus Scheduler locally using Kind (Kubernetes in Docker).

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- Go 1.23+ (for building)

## Quick Start

### 1. Setup Kind Cluster and Deploy Scheduler

```bash
make kind-setup
```

This will:
- Create a 4-node Kind cluster
- Build the scheduler image for Linux
- Load the image into Kind
- Deploy CRDs
- Deploy the KubeNexus scheduler

### 2. Run Test Workloads

```bash
make kind-test
```

This runs three test scenarios:
- **Gang Scheduling**: 3 pods that must be scheduled together
- **Batch Workload**: Kubernetes Job with workload-aware scoring
- **Service Workload**: Deployment with topology spreading

### 3. Watch Scheduler Logs

```bash
make kind-logs
```

### 4. Cleanup

```bash
make kind-cleanup
```

## Manual Testing

### Deploy Individual Test Workloads

#### Gang Scheduling Test

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: gang-worker-1
  namespace: default
  labels:
    pod-group.scheduling.kubenexus.io/name: "my-gang"
    pod-group.scheduling.kubenexus.io/min-available: "3"
spec:
  schedulerName: kubenexus-scheduler
  containers:
  - name: worker
    image: busybox:1.36
    command: ["sleep", "3600"]
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
EOF
```

Repeat for `gang-worker-2` and `gang-worker-3`. All three will be scheduled together atomically.

#### Check Pod Status

```bash
kubectl get pods -o wide
kubectl describe pod gang-worker-1
```

#### Check Scheduler Logs

```bash
kubectl logs -n kube-system -l app=kubenexus-scheduler -f
```

## Test Scenarios

### 1. Gang Scheduling (Co-scheduling)
- **Objective**: Verify all pods in a gang are scheduled together
- **Workload**: 3 pods with matching gang labels
- **Expected**: All 3 pods transition to Running simultaneously

### 2. Workload-Aware Scoring
- **Objective**: Verify batch workloads use bin-packing strategy
- **Workload**: Kubernetes Job with batch label
- **Expected**: Pods co-located on same nodes when possible

### 3. Topology Spreading
- **Objective**: Verify service workloads spread across nodes
- **Workload**: Deployment with 3 replicas
- **Expected**: Pods distributed evenly across worker nodes

## Troubleshooting

### Scheduler not starting

```bash
kubectl describe pod -n kube-system -l app=kubenexus-scheduler
kubectl logs -n kube-system -l app=kubenexus-scheduler
```

### Pods not being scheduled

1. Check if scheduler is running:
   ```bash
   kubectl get pods -n kube-system -l app=kubenexus-scheduler
   ```

2. Check pod events:
   ```bash
   kubectl describe pod <pod-name>
   ```

3. Verify scheduler name:
   ```bash
   kubectl get pod <pod-name> -o yaml | grep schedulerName
   ```

### Image not found

Rebuild and reload the image:
```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags '-w' -o bin/kubenexus-scheduler-linux cmd/scheduler/main.go
docker build -t kubenexus-scheduler:latest .
kind load docker-image kubenexus-scheduler:latest --name kubenexus-test
kubectl delete pod -n kube-system -l app=kubenexus-scheduler
```

## Advanced Testing

### Test with Different Cluster Sizes

Modify `hack/kind-cluster.yaml` to add/remove worker nodes:

```yaml
nodes:
  - role: control-plane
  - role: worker
  - role: worker
  # Add more workers as needed
```

Then recreate the cluster:
```bash
make kind-cleanup
make kind-setup
```

### Test with Resource Constraints

Create pods with high resource requests to test resource reservation:

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: high-resource-pod
spec:
  schedulerName: kubenexus-scheduler
  containers:
  - name: app
    image: nginx:1.25-alpine
    resources:
      requests:
        cpu: "2"
        memory: "4Gi"
EOF
```

### Monitor Scheduling Decisions

```bash
# Watch all events
kubectl get events --all-namespaces --watch

# Watch scheduler-specific events
kubectl get events --all-namespaces --field-selector source=kubenexus-scheduler --watch
```

## CI/CD Integration

The test scripts can be integrated into CI pipelines:

```bash
#!/bin/bash
set -e

# Run full test suite
make kind-setup
make kind-test
make kind-cleanup
```

## Next Steps

- Try with [Kubeflow PyTorchJob](../docs/KUBEFLOW_INTEGRATION.md)
- Test with [Spark Operator](../docs/SPARK_OPERATOR_INTEGRATION.md)
- Enable [NUMA scheduling](../docs/NUMA_SCHEDULING_GUIDE.md) for GPU workloads
