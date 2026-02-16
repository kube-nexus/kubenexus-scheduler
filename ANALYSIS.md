# KubeNexus Scheduler - Analysis & Roadmap

**Date**: February 16, 2026  
**Status**: Production-Ready Core, Missing Advanced Features

---

## 📊 Current State Analysis

### ✅ What We Have (PRODUCTION READY)

1. **Gang Scheduling (Co-scheduling)** - CORE FEATURE
   - All-or-nothing scheduling for pod groups
   - Starvation prevention (age-based priority boost after 60s)
   - FIFO queue ordering with priority support
   - Timeout handling (10s permit wait time)
   - Location: `pkg/coscheduling/coscheduling.go`

2. **Resource Reservation** - OPTIONAL FEATURE
   - CRD-based resource tracking
   - Prevents double-booking of resources
   - Internalized (no external dependencies)
   - Location: `pkg/resourcereservation/`, `pkg/apis/scheduling/v1alpha1/`

3. **Modern Architecture**
   - Go 1.25, Kubernetes 1.35.1
   - Scheduler Framework plugin-based
   - Zero external scheduling dependencies
   - Self-contained codebase

### ❌ What's Missing (KEY GAPS)

#### 1. **Topology Awareness** - HIGH PRIORITY MISSING
**Current State**: **NO topology awareness at all**

- ❌ No zone/region spreading
- ❌ No rack awareness
- ❌ No GPU topology
- ❌ No NUMA awareness

**Impact**: 
- Multi-AZ clusters: Pods may all land in one zone (no HA)
- GPU workloads: Can't optimize GPU placement
- Network-intensive jobs: No latency optimization

**Comparison**:
| Scheduler | Zone Awareness | GPU Topology | Rack Awareness |
|-----------|----------------|--------------|----------------|
| YuniKorn  | ✅ Yes | ✅ Yes | ✅ Yes |
| Volcano   | ✅ Yes | ✅ Yes | ⚠️ Limited |
| Kueue     | ⚠️ Via K8s | ⚠️ Via K8s | ❌ No |
| **KubeNexus** | **❌ NO** | **❌ NO** | **❌ NO** |

---

#### 2. **Queue Management** - MISSING
**Current State**: **Basic FIFO only, no queues**

- ❌ No resource quotas/limits
- ❌ No fairness policies
- ❌ No multi-tenancy support
- ❌ No queue hierarchies

**What we have**:
- FIFO ordering (oldest first)
- Priority-based sorting
- Starvation prevention (age boost)

**Comparison**:
| Scheduler | Queues | Fairness | Multi-tenant |
|-----------|--------|----------|--------------|
| YuniKorn  | ✅ Hierarchical | ✅ DRF | ✅ Full |
| Volcano   | ✅ Queue CRD | ✅ DRF | ✅ Full |
| Kueue     | ✅ ClusterQueue | ✅ Fair | ✅ Full |
| **KubeNexus** | **❌ None** | **⚠️ FIFO+Priority** | **❌ No** |

---

#### 3. **Advanced Scheduling Features** - MISSING

Missing capabilities:
- ❌ Preemption (can't reclaim resources)
- ❌ Bin packing optimization
- ❌ GPU scheduling/sharing
- ❌ Job dependencies
- ❌ Backfill scheduling

---

## 🤔 Answers to Your Questions

### Q1: Is this scheduler topology-aware?
**Answer: NO, not at all.**

The scheduler has:
- ✅ Gang scheduling
- ✅ Priority/FIFO ordering
- ❌ **Zero topology awareness**

To add topology awareness, we need to implement:
1. **ScorePlugin interface** - Doesn't exist yet
2. **Zone/region spreading** logic
3. **Node label/taint handling** for topology

---

### Q2: Are resource reservations required for non-batch workloads?
**Answer: NO, they're optional even for batch workloads.**

**Resource Reservation Plugin Purpose**:
- Tracks which resources are "spoken for" by pending pods
- Prevents smaller jobs from fragmenting resources
- Useful in **multi-tenant clusters** with many concurrent batch jobs

**When to use**:
- ✅ Multi-tenant clusters (many teams)
- ✅ Long-running batch jobs (Spark, ML training)
- ✅ Preventing resource fragmentation
- ❌ Single-tenant clusters (not needed)
- ❌ Small clusters (not needed)
- ❌ Stateless services (definitely not needed)

**For non-batch workloads**: Not needed at all. Standard K8s scheduling is fine.

---

### Q3: How does it compare to Kueue vs Volcano vs YuniKorn?

#### Feature Comparison Matrix

| Feature | YuniKorn | Volcano | Kueue | **KubeNexus** |
|---------|----------|---------|-------|---------------|
| **Gang Scheduling** | ✅ Advanced | ✅ Advanced | ✅ Via Volcano | ✅ **Core** |
| **Queues** | ✅ Hierarchical | ✅ Queue CRD | ✅ ClusterQueue | **❌ None** |
| **Fairness** | ✅ DRF | ✅ DRF | ✅ Fair | **⚠️ FIFO only** |
| **Topology Aware** | ✅ Yes | ✅ Yes | ⚠️ Via K8s | **❌ NO** |
| **GPU Support** | ✅ Advanced | ✅ Good | ⚠️ Basic | **❌ None** |
| **Preemption** | ✅ Yes | ✅ Yes | ✅ Yes | **❌ No** |
| **Multi-tenancy** | ✅ Full | ✅ Full | ✅ Full | **❌ No** |
| **Resource Footprint** | ~500MB | ~300MB | ~100MB | **~50MB** |
| **Setup Complexity** | High | High | Medium | **Very Low** |
| **Dependencies** | etcd, DB | CRDs | CRDs | **None** |
| **Use Case** | Multi-cluster | HPC, Workflows | Batch Quotas | **Simple gang** |

#### When to use each:

**Use YuniKorn** if you need:
- Multi-cluster management
- Advanced queue hierarchies
- Large-scale multi-tenancy
- Don't mind the complexity

**Use Volcano** if you need:
- HPC workloads (MPI, multi-node)
- Complex job dependencies
- Advanced scheduling policies
- TensorFlow/PyTorch distributed training

**Use Kueue** if you need:
- Resource quota management
- Multi-queue batch scheduling
- Integration with existing K8s primitives
- Simpler than YuniKorn/Volcano

**Use KubeNexus** if you need:
- **Simple gang scheduling only**
- Minimal resource overhead
- No complex features needed
- Quick deployment (<5 min)
- Spark/ML jobs without bells & whistles

---

### Q4: Was the labeling logic in PreExtender redundant?
**Answer: YES, it was Spark-specific and redundant.**

**PreExtender Plugin** (REMOVED):
- Purpose: Added `spark-app-id` label to pods
- Why: Palantir's external resource service needed it
- Redundant because:
  1. Spark operator already adds labels
  2. Coscheduling uses `pod-group.scheduling.sigs.k8s.io/name` (standard)
  3. ResourceReservation uses `scheduling.kubenexus.io/app-id` (internalized)

**Labeling is now handled**:
- By workload controllers (Spark Operator, TFJob, etc.)
- Via standard annotations: `pod-group.scheduling.sigs.k8s.io/name`
- No special plugin needed

**Coscheduling plugin** handles:
- Reading `pod-group.scheduling.sigs.k8s.io/name` label
- Reading `pod-group.scheduling.sigs.k8s.io/min-available` label
- No special labeling needed

---

### Q5: How do we generalize this for all batch workloads?
**Answer: It's already generalized! But labeled "Spark-focused" in docs.**

#### Current State: **Already Generic**

The coscheduling plugin uses **standard annotations**:
```yaml
annotations:
  pod-group.scheduling.sigs.k8s.io/name: "my-job"
  pod-group.scheduling.sigs.k8s.io/min-available: "10"
```

This works for:
- ✅ Apache Spark (driver + executors)
- ✅ TensorFlow (ps + workers)
- ✅ PyTorch (master + workers)
- ✅ MPI jobs (launcher + workers)
- ✅ Kubeflow pipelines
- ✅ Ray clusters
- ✅ **Any pod group that needs all-or-nothing scheduling**

#### What Makes It Seem "Spark-Focused"?

**Documentation only!** The code mentions Spark in:
- README examples (Spark jobs)
- Comments (Spark driver/executor)
- Utility functions: `IsSparkDriver()`, `GetSparkRole()` in `pkg/utils/pod.go`

**But**: These are **optional helper functions**, not used by core scheduling.

---

## 🎯 Recommendations

### Immediate Actions (Do Now)

1. **Update Documentation** - Remove Spark-centric language
   - Change "Spark scheduler" → "Batch workload scheduler"
   - Add examples for TensorFlow, PyTorch, MPI
   - Clarify: Gang scheduling is workload-agnostic

2. **Deprecate Spark-specific utils** - Mark as optional
   - `pkg/utils/pod.go`: `IsSparkDriver()`, etc.
   - Add comment: "Optional helpers, not used by core"

3. **Create workload examples**
   - `examples/spark-job.yaml`
   - `examples/tensorflow-training.yaml`
   - `examples/pytorch-training.yaml`
   - `examples/mpi-job.yaml`

### Short-term (Q2 2026)

4. **Add Topology Awareness** - HIGH PRIORITY
   - Implement ScorePlugin for zone spreading
   - Add annotation: `topology.kubenexus.io/spread-zones: "true"`
   - Test with multi-AZ clusters

5. **Basic Queue Management**
   - Add resource quotas per namespace
   - Implement simple fairness (round-robin across namespaces)

6. **Add Tests**
   - Integration tests with real workloads
   - E2E tests for gang scheduling
   - Topology awareness tests

### Long-term (Q3-Q4 2026)

7. **GPU Scheduling**
   - GPU topology awareness
   - GPU sharing/fractional GPUs

8. **Advanced Features**
   - Preemption policies
   - Bin packing optimization
   - Job dependencies

---

## 📝 Updated Positioning

### Before (Misleading):
> "Kubernetes scheduler for Apache Spark workloads"

### After (Accurate):
> "Lightweight Kubernetes scheduler for batch workloads with gang scheduling"

### Target Workloads:
- Apache Spark (driver + executors)
- TensorFlow/PyTorch distributed training (ps + workers)
- MPI/HPC jobs (launcher + workers)
- Kubeflow pipelines
- Ray clusters
- **Any workload requiring all-or-nothing scheduling**

---

## 🏆 Competitive Positioning

| Aspect | KubeNexus Position |
|--------|-------------------|
| **Complexity** | 🥇 **Simplest** (5 min setup) |
| **Resource Footprint** | 🥇 **Smallest** (~50MB) |
| **Gang Scheduling** | 🥈 **Good** (core feature) |
| **Advanced Features** | 🥉 **Limited** (no queues, topology) |
| **Multi-tenancy** | ❌ **None** |
| **Best For** | Single-tenant, simple gang scheduling |

**TL;DR**: KubeNexus is the **"just works" scheduler** for teams who:
- Need gang scheduling without complexity
- Run single-tenant clusters
- Don't need advanced queue management
- Want minimal overhead

For advanced features (queues, topology, multi-tenancy), use YuniKorn or Volcano.

---

## 🚀 Next Steps

**Priority 1: Documentation Fix**
```bash
# Update these files to remove Spark-centric language
- README.md
- claude.md
- examples/
```

**Priority 2: Add Topology Awareness**
```bash
# Create new plugin
mkdir -p pkg/topology
# Implement ScorePlugin for zone spreading
```

**Priority 3: Add Workload Examples**
```bash
mkdir -p examples
# Add TensorFlow, PyTorch, MPI examples
```

Want me to start with any of these?
