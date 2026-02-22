# CRD Installation Guide

This document explains how to install Custom Resource Definitions (CRDs) required by KubeNexus Scheduler.

## Prerequisites

- Kubernetes cluster 1.30+ (1.35+ recommended for native Workload API)
- `kubectl` configured with admin access
- Cluster with appropriate feature gates enabled

## CRD Overview

KubeNexus Scheduler supports multiple CRDs for advanced scheduling:

1. **ResourceReservation CRD** - For resource reservation and preemption
2. **Workload CRD** - For K8s 1.35+ native gang scheduling (recommended)

## Installation

### 1. ResourceReservation CRD

Install the ResourceReservation CRD for advanced resource management:

```bash
kubectl apply -f config/crd-resourcereservation.yaml
```

Verify installation:

```bash
kubectl get crd resourcereservations.scheduling.kubenexus.io
```

### 2. Workload CRD (K8s 1.35+ Native Gang Scheduling)

**Note**: The native Kubernetes Workload API (scheduling.k8s.io/v1alpha1) requires additional controllers like [Kueue](https://kueue.sigs.k8s.io/) or [JobSet](https://github.com/kubernetes-sigs/jobset) to be fully functional. The CRD provided here is a simplified version for documentation purposes.

**For production use, we recommend using the label-based gang scheduling approach**, which is fully supported and tested.

#### Quick Install with Kueue

We provide a setup script for easy installation:

```bash
# Install Kueue and enable Workload API
./hack/install-kueue.sh
```

#### Manual Installation

If you want to use Kueue's Workload API:

```bash
# Install Kueue (provides Workload controller)
kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/v0.10.0/manifests.yaml

# Wait for Kueue to be ready
kubectl wait --for=condition=available --timeout=300s deployment/kueue-controller-manager -n kueue-system

# Verify installation
kubectl get pods -n kueue-system
kubectl api-resources | grep workload
```

For testing without Kueue, use the label-based approach (see below).

### 3. Install All CRDs at Once

To install all CRDs in one command:

```bash
kubectl apply -f config/crd-resourcereservation.yaml -f config/crd-workload.yaml
```

## Verification

Check all installed CRDs:

```bash
kubectl get crds | grep -E "kubenexus|scheduling.k8s.io"
```

Expected output:
```
resourcereservations.scheduling.kubenexus.io    2024-01-01T00:00:00Z
workloads.scheduling.k8s.io                     2024-01-01T00:00:00Z
```

## Usage Examples

### ResourceReservation Example

See [docs/examples/resourcereservation-example.yaml](../docs/examples/resourcereservation-example.yaml)

### Workload API Example (Gang Scheduling)

See [test/e2e/workload-api-test.yaml](../test/e2e/workload-api-test.yaml) for a complete example.

Quick example:

```yaml
apiVersion: scheduling.k8s.io/v1alpha1
kind: Workload
metadata:
  name: my-gang
  namespace: default
spec:
  podSets:
  - name: workers
    count: 3
---
apiVersion: v1
kind: Pod
metadata:
  name: worker-1
  labels:
    scheduling.k8s.io/workload: my-gang
    scheduling.k8s.io/podset: workers
spec:
  schedulerName: kubenexus-scheduler
  containers:
  - name: worker
    image: nginx:latest
```

### Label-Based Gang Scheduling (Fallback)

For clusters without Workload CRD, use label-based approach:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: worker-1
  labels:
    pod-group.scheduling.sigs.k8s.io/name: my-gang
    pod-group.scheduling.sigs.k8s.io/min-available: "3"
spec:
  schedulerName: kubenexus-scheduler
  containers:
  - name: worker
    image: nginx:latest
```

## Cluster Configuration

### For K8s 1.35+ with DRA

Ensure these feature gates are enabled in your cluster:

```yaml
apiVersion: kind.x-k8s.io/v1alpha4
kind: Cluster
featureGates:
  "DynamicResourceAllocation": true
nodes:
- role: control-plane
  kubeadmConfigPatches:
  - |
    kind: ClusterConfiguration
    apiServer:
      extraArgs:
        feature-gates: "DynamicResourceAllocation=true"
        runtime-config: "scheduling.k8s.io/v1alpha1=true,resource.k8s.io/v1alpha3=true"
```

See [hack/kind-cluster-v1.35.yaml](../hack/kind-cluster-v1.35.yaml) for complete Kind cluster configuration.

## Troubleshooting

### CRD Not Found

If you get "no matches for kind" error:

```bash
# Check if CRD is installed
kubectl get crd workloads.scheduling.k8s.io

# If not found, install it
kubectl apply -f config/crd-workload.yaml

# Wait for CRD to be established
kubectl wait --for condition=established --timeout=60s crd/workloads.scheduling.k8s.io
```

### API Server Not Recognizing CRD

Ensure runtime-config is enabled:

```bash
kubectl get --raw /apis/scheduling.k8s.io/v1alpha1
```

### Permission Issues

Ensure your ServiceAccount has appropriate RBAC permissions. The scheduler deployment includes all necessary permissions.

## Uninstallation

To remove CRDs (WARNING: This deletes all associated resources):

```bash
kubectl delete -f config/crd-workload.yaml
kubectl delete -f config/crd-resourcereservation.yaml
```

## Additional Resources

- [Kubernetes CRD Documentation](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/)
- [K8s 1.35 Workload API KEP](https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling)
- [KubeNexus User Guide](USER_GUIDE.md)
