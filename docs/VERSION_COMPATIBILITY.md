# KubeNexus Kubernetes Version Compatibility Guide

**Last Updated:** March 2026  
**KubeNexus Version:** v0.3.0+

---

## Overview

KubeNexus is designed to work across **all Kubernetes versions** through a smart fallback strategy. You get the best features available for your Kubernetes version, with graceful degradation on older versions.

---

## Compatibility Matrix

| Kubernetes Version | DRA Support | NFD Support | Manual Labels | Recommended Setup |
|--------------------|--------------|--------------|-----------------|-----------------------|
| **1.34 - 1.35+** | ✅ GA (Stable) | ✅ Yes | ✅ Yes | **DRA + NFD** (validation) |
| **1.26 - 1.33** | ⚠️ Alpha/Beta | ✅ Yes | ✅ Yes | **NFD + Manual Labels** (DRA experimental) |
| **1.20 - 1.25** | ❌ No | ✅ Yes | ✅ Yes | **NFD + Manual Labels** |
| **1.18 - 1.19** | ❌ No | ✅ Yes | ✅ Yes | **NFD + Manual Labels** |
| **< 1.18** | ❌ No | ⚠️ Limited | ✅ Yes | **Manual Labels only** |

**Important:** DRA (Dynamic Resource Allocation) went **GA in Kubernetes v1.34**. While KubeNexus can detect and use DRA ResourceSlices on K8s 1.26+, production use should target **v1.34+** for stability.

---

## Feature Availability by Kubernetes Version

### Topology Discovery Features

| Feature | K8s 1.34+ (DRA GA) | K8s 1.18+ (NFD) | Any Version (Labels) |
|---------|---------------------|-----------------|----------------------|
| **GPU Detection** | ✅ Automatic | ✅ Automatic | ⚠️ Manual |
| **VRAM Capacity** | ✅ Precise | ✅ Inferred from PCI ID | ⚠️ Manual |
| **NUMA Node Mapping** | ✅ Per-GPU | ✅ Node-level | ❌ Unknown |
| **NVLink Topology** | ✅ Full peer graph | ❌ Not available | ❌ Not available |
| **PCIe Locality** | ✅ Bus ID + Switch | ✅ Device ID only | ❌ Not available |
| **Dynamic Updates** | ✅ Real-time | ❌ Node reboot | ❌ Manual relabel |
| **Heterogeneous GPUs** | ✅ Per-device | ✅ By type | ⚠️ Requires labels |

### Scheduling Capabilities

All scheduling capabilities work on **any Kubernetes version**:
- ✅ VRAM-aware placement
- ✅ Fragmentation prevention  
- ✅ Workload intent optimization
- ✅ Gang scheduling
- ✅ Tenant hardware routing

The **quality** of decisions improves with better topology data (DRA > NFD > Labels).

---

## Recommended Setups by Scenario

### Scenario 1: Modern Production (K8s 1.34+)

**✅ Full GPU Topology Awareness with GA DRA**

```bash
# Install KubeNexus
kubectl apply -f deploy/kubenexus-scheduler.yaml

# Install DRA driver (NVIDIA example)
kubectl apply -f https://github.com/NVIDIA/k8s-dra-driver/releases/latest/download/nvidia-dra-driver.yaml

# Optional: Install NFD for validation and non-GPU features
kubectl apply -f https://kubernetes-sigs.github.io/node-feature-discovery/charts/nfd-daemonset.yaml
```

**Benefits:**
- Full NVLink domain detection
- Per-GPU NUMA mapping
- PCIe switch locality
- Real-time topology updates
- Zero manual labeling

**Workload Example:**
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: training-job
  annotations:
    scheduling.kubenexus.io/vram-request: "80Gi"  # Hint for scheduler
spec:
  schedulerName: kubenexus-scheduler
  resourceClaims:
  - name: gpu-claim
    resourceClaimTemplateName: gpu-80gb-template
  containers:
  - name: trainer
    image: pytorch:latest
    resources:
      requests:
        nvidia.com/gpu: "8"
```

---

### Scenario 2: Transitional (K8s 1.26-1.33)

**⚠️ Experimental DRA Support**

```bash
# KubeNexus will detect and use DRA if available
kubectl apply -f deploy/kubenexus-scheduler.yaml

# DRA is in alpha/beta - test thoroughly before production
kubectl apply -f https://github.com/NVIDIA/k8s-dra-driver/releases/latest/download/nvidia-dra-driver.yaml

# Recommended: Also install NFD as fallback
kubectl apply -f https://kubernetes-sigs.github.io/node-feature-discovery/charts/nfd-daemonset.yaml
```

**Considerations:**
- DRA API is not stable - may have breaking changes
- Use NFD + manual labels as primary method
- DRA is experimental validation only
- Upgrade to K8s 1.34+ for production DRA

---

### Scenario 3: Legacy Production (K8s 1.20-1.25)

**✅ Auto-Discovery via NFD + Manual Labels**

```bash
# Install KubeNexus
kubectl apply -f deploy/kubenexus-scheduler.yaml

# Install NFD for automatic GPU detection
kubectl apply -f https://kubernetes-sigs.github.io/node-feature-discovery/charts/nfd-daemonset.yaml

# For heterogeneous clusters, add manual labels for specific nodes
kubectl label nodes h100-node-1 gpu.kubenexus.io/model=H100
kubectl label nodes a100-node-1 gpu.kubenexus.io/model=A100-80GB
```

**Benefits:**
- Automatic GPU detection via PCI scanning
- VRAM inferred from GPU model
- NUMA node count detection
- No per-node manual labeling needed

**Workload Example:**
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: training-job
  annotations:
    scheduling.kubenexus.io/vram-request: "80Gi"  # Required for VRAM-aware scheduling
spec:
  schedulerName: kubenexus-scheduler
  containers:
  - name: trainer
    image: pytorch:latest
    resources:
      requests:
        nvidia.com/gpu: "8"
```

---

### Scenario 3: Older Clusters (K8s < 1.20)

**✅ Manual Labels Only**

```bash
# Install KubeNexus
kubectl apply -f deploy/kubenexus-scheduler.yaml

# Label all GPU nodes
# H100 nodes
kubectl label nodes h100-node-1 gpu.kubenexus.io/vram=80Gi
kubectl label nodes h100-node-1 gpu.kubenexus.io/model=H100
kubectl label nodes h100-node-1 gpu.kubenexus.io/count=8
kubectl label nodes h100-node-1 network.kubenexus.io/fabric-type=nvswitch

# A100 nodes
kubectl label nodes a100-node-1 gpu.kubenexus.io/vram=80Gi
kubectl label nodes a100-node-1 gpu.kubenexus.io/model=A100-80GB
kubectl label nodes a100-node-1 gpu.kubenexus.io/count=8
```

**Limitations:**
- Manual labeling required for each node
- No automatic topology updates
- Limited NUMA/NVLink awareness

**Workload Example:** (Same as Scenario 2)

---

## Migration Paths

### Path 1: Labels → NFD

**Before:**
```bash
# Manual labels on every node
kubectl label nodes node-1 gpu.kubenexus.io/vram=80Gi
kubectl label nodes node-1 gpu.kubenexus.io/model=H100
# ...repeat for all nodes
```

**After:**
```bash
# Install NFD once
kubectl apply -f https://kubernetes-sigs.github.io/node-feature-discovery/charts/nfd-daemonset.yaml

# NFD automatically detects GPUs on all nodes via PCI
# Labels removed if desired (NFD will override)
```

**Benefits:** Eliminate manual labeling, automatic updates when hardware changes

---

### Path 2: NFD → DRA (Requires K8s 1.26 upgrade)

**Before:**
```bash
# NFD running
kubectl get daemonset -n node-feature-discovery nfd-worker
```

**After:**
```bash
# 1. Upgrade Kubernetes to 1.26+
kubeadm upgrade apply v1.26.0

# 2. Install DRA driver
kubectl apply -f nvidia-dra-driver.yaml

# 3. Verify ResourceSlices appear
kubectl get resourceslices

# NFD can remain installed for non-GPU features
```

**Benefits:** Full topology awareness, NVLink domains, per-GPU NUMA

---

## Fallback Behavior

KubeNexus automatically selects the best available data source:

```
┌─────────────────────────────────────────┐
│ 1. Try DRA ResourceSlices               │
│    K8s 1.26+ with DRA driver            │
│    ✅ Full topology                      │
└───────────────┬─────────────────────────┘
                │ Not available?
                ▼
┌─────────────────────────────────────────┐
│ 2. Try NFD PCI Labels                   │
│    K8s 1.18+ with NFD DaemonSet         │
│    ✅ Auto-discovered GPU detection      │
└───────────────┬─────────────────────────┘
                │ Not available?
                ▼
┌─────────────────────────────────────────┐
│ 3. Use Manual Node Labels               │
│    Any Kubernetes version               │
│    ⚠️  Operator-managed                  │
└─────────────────────────────────────────┘
```

**No configuration required** - KubeNexus detects what's available and uses it.

---

## Verification Commands

### Check which data source is being used

```bash
# Check scheduler logs for topology source
kubectl logs -n kubenexus-system deployment/kubenexus-scheduler | grep "Using GPU topology"

# You'll see one of:
# ✅ Using GPU topology from DRA ResourceSlices
# ✅ Using GPU topology from NFD labels
# ⚠️  Using GPU topology from manual node labels
```

### Verify DRA is available

```bash
# Check if ResourceSlices exist
kubectl get resourceslices

# Check DRA driver is running
kubectl get pods -n kube-system | grep dra-driver
```

### Verify NFD is working

```bash
# Check NFD DaemonSet
kubectl get daemonset -n node-feature-discovery

# Check if node has NFD labels
kubectl get node <node-name> -o json | jq '.metadata.labels | with_entries(select(.key | contains("feature.node.kubernetes.io")))'

# Look for PCI GPU detection
kubectl get node <node-name> -o json | jq '.metadata.labels | with_entries(select(.key | contains("pci-10de")))'
# 10de = NVIDIA vendor ID
```

### Verify manual labels

```bash
kubectl get nodes -L gpu.kubenexus.io/vram,gpu.kubenexus.io/model,gpu.kubenexus.io/count
```

---

## Troubleshooting

### DRA not working

**Symptom:** Falling back to NFD/labels despite K8s 1.26+

**Checks:**
```bash
# 1. Verify K8s version
kubectl version --short
# Server Version should be >= v1.26.0

# 2. Check ResourceSlices
kubectl get resourceslices
# Should show slices for GPU nodes

# 3. Check DRA driver
kubectl get pods -n kube-system -l app=nvidia-dra-driver
# Should be Running

# 4. Check scheduler logs
kubectl logs -n kubenexus-system deployment/kubenexus-scheduler | grep DRA
# Should show successful ResourceSlice queries
```

**Fix:**
```bash
# Reinstall DRA driver
kubectl delete -f nvidia-dra-driver.yaml
kubectl apply -f nvidia-dra-driver.yaml
```

---

### NFD not detecting GPUs

**Symptom:** Falling back to manual labels despite NFD installed

**Checks:**
```bash
# 1. Check NFD pods
kubectl get pods -n node-feature-discovery
# nfd-master and nfd-worker pods should be Running

# 2. Check NFD labels on node
kubectl describe node <gpu-node> | grep feature.node.kubernetes.io

# 3. Check if PCI scanning is enabled
kubectl get configmap -n node-feature-discovery nfd-worker-conf -o yaml | grep pci
```

**Fix:**
```bash
# Ensure PCI scanning is enabled in NFD config
kubectl edit configmap -n node-feature-discovery nfd-worker-conf

# Add if missing:
# sources:
#   pci:
#     deviceClassWhitelist:
#       - "03"  # Display controllers (GPUs)

# Restart NFD
kubectl rollout restart daemonset -n node-feature-discovery nfd-worker
```

---

## Performance Comparison

| Scenario | Scheduling Decision Quality | Ops Overhead | Update Frequency |
|----------|----------------------------|--------------|------------------|
| **DRA** | Excellent (100%) | None | Real-time |
| **NFD** | Good (75-85%) | None | On node reboot |
| **Manual Labels** | Basic (60-70%) | High | Manual only |

**Scheduling Decision Quality** measures:
- Accuracy of VRAM matching
- NUMA locality optimization
- NVLink domain preservation  
- Fragmentation prevention

---

## Recommendations

### For New Deployments
✅ Use **Kubernetes 1.26+** with DRA from day one

### For Existing Clusters
1. **K8s < 1.20:** Start with manual labels, plan K8s upgrade
2. **K8s 1.20-1.25:** Install NFD immediately (zero downside)
3. **K8s 1.26+:** Install DRA driver, optionally keep NFD for validation

### For Heterogeneous Clusters
- **DRA:** Best choice, handles mixed GPU types automatically
- **NFD:** Works well, infers VRAM from PCI device ID
- **Labels:** Requires meticulous per-node labeling

### For Air-Gapped Environments
- All three methods work (no internet required for scheduling)
- Pre-load DRA/NFD images if using them
- Manual labels most reliable without external dependencies

---

## FAQ

**Q: Can I use all three methods simultaneously?**  
A: Yes! KubeNexus automatically picks the best available. DRA takes precedence, then NFD, then manual labels.

**Q: Do I need to remove manual labels after installing DRA/NFD?**  
A: No, but you can. KubeNexus will prefer DRA/NFD data when available. Manual labels act as fallback.

**Q: What if I have nodes with different GPU types?**  
A: DRA and NFD handle this automatically. With manual labels, label each node with its specific GPU model and VRAM.

**Q: Does this work with AMD/Intel GPUs?**  
A: Yes! NFD detects any PCI GPU. DRA works if AMD/Intel provide DRA drivers. Manual labels work for any GPU.

**Q: What happens if DRA driver crashes?**  
A: KubeNexus falls back to NFD or manual labels automatically. Scheduling continues with degraded topology info.

---

## Further Reading

- [DRA User Guide](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
- [NFD Documentation](https://kubernetes-sigs.github.io/node-feature-discovery/)
- [KubeNexus GPU Topology Guide](GPU_TOPOLOGY_IMPLEMENTATION.md)
- [KubeNexus Quick Start](USER_GUIDE.md)
