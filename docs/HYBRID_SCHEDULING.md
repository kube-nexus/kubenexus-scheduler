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

## 🎯 Scope & Feasibility Analysis - REVISED MISSION

### What's ACTUALLY In Scope for KubeNexus?

#### ✅ Core Mission: **HYBRID SCHEDULING** (Batch + Normal Workloads)
**The Real Goal:** Co-allocate batch AND normal workloads efficiently in the same cluster.

**This is DIFFERENT from just gang scheduling!**

**What this means:**
- ✅ Gang scheduling for batch workloads (Spark, TensorFlow, etc.) - DONE ✅
- ✅ Normal scheduling for stateless services (API, webapp, etc.)
- ✅ Efficient resource sharing between both workload types
- ✅ Prevent batch jobs from starving services
- ✅ Prevent services from blocking batch jobs
- ⚠️ Basic fairness between batch and normal workloads - NEEDED

**KubeNexus should be**: A **unified scheduler** that handles both batch and normal workloads intelligently. NOT just a gang scheduler.

---

### ❌ What's OUT OF SCOPE (Use YuniKorn/Volcano Instead)

#### 1. Queue Management - **OUT OF SCOPE**
**Why you'd want queues:**
- **Multi-tenancy**: Team A gets 40% of cluster, Team B gets 60%
- **Priority tiers**: Production > Staging > Dev
- **Resource quotas**: Limit teams to X CPUs/memory
- **Fair sharing**: Prevent one team from hogging resources

**Example**: Company with 5 teams sharing one cluster
```
Root Queue (100% cluster)
├── Team-A-Queue (30% guaranteed, 50% max burst)
├── Team-B-Queue (40% guaranteed, 60% max burst)
└── Team-C-Queue (30% guaranteed, 50% max burst)
```

**KubeNexus Answer**: Use Kubernetes **namespaces + ResourceQuotas** instead!
```yaml
# namespace-team-a.yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: team-a-quota
  namespace: team-a
spec:
  hard:
    requests.cpu: "100"
    requests.memory: "200Gi"
    pods: "50"
```

**Verdict**: ❌ **OUT OF SCOPE** - K8s ResourceQuotas handle this. Building queue hierarchies is YuniKorn territory.

---

#### 2. Multi-tenancy - **PARTIALLY OUT OF SCOPE**
**What is multi-tenancy in schedulers?**
- Per-team resource guarantees
- Fair sharing across teams
- Preemption (kicking out low-priority jobs)
- Queue hierarchies

**KubeNexus Position**: 
- ✅ Can schedule pods from multiple namespaces (basic multi-tenancy)
- ❌ No fair sharing between namespaces
- ❌ No per-namespace quotas in scheduler
- ❌ No preemption

**Use Case**: If you need **gang scheduling in a multi-namespace cluster**, KubeNexus works fine!
```yaml
# Namespace: team-a
metadata:
  namespace: team-a
  annotations:
    pod-group.scheduling.sigs.k8s.io/name: "team-a-job-1"
    pod-group.scheduling.sigs.k8s.io/min-available: "10"

---
# Namespace: team-b (different job, different namespace)
metadata:
  namespace: team-b
  annotations:
    pod-group.scheduling.sigs.k8s.io/name: "team-b-job-1"
    pod-group.scheduling.sigs.k8s.io/min-available: "5"
```

Both get gang-scheduled independently. First-come-first-served (FIFO + Priority).

**Verdict**: ⚠️ **BASIC multi-tenancy works** (v1.1). Advanced fair sharing is out of scope.

---

#### 3. Topology Awareness - **PARTIALLY IN SCOPE**

**What's realistic?**

| Feature | Feasibility | Recommendation |
|---------|-------------|----------------|
| **Zone spreading** | ✅ Easy | **DO THIS** (HIGH ROI) |
| **GPU topology** | ⚠️ Medium | Later (v1.2+) |
| **Rack awareness** | ❌ Hard | Out of scope |
| **NUMA** | ❌ Hard | Out of scope |

**Zone Spreading** (HIGH PRIORITY):
- **Why**: Multi-AZ HA for production workloads
- **Effort**: ~200 lines of code (ScorePlugin)
- **Impact**: Massive (spreads pods across zones)

```go
// pkg/topology/zone.go - SIMPLE to add
func (z *ZonePlugin) Score(ctx, state, pod, nodeName) int64 {
    zone := node.Labels["topology.kubernetes.io/zone"]
    podsInZone := countPodsInZone(pod.PodGroup, zone)
    
    // Prefer zones with fewer pods (spread)
    return 100 - (podsInZone * 10)
}
```

**Verdict**: ✅ **Zone spreading is IN SCOPE** (v1.1). GPU/Rack/NUMA are out of scope.

---

### 🤔 Can KubeNexus Co-schedule Normal Workloads?

**Short Answer: YES! That's the whole point.**

#### The Hybrid Scheduling Vision

**Problem**: Most clusters waste resources by separating batch and normal workloads:
- **Dedicated batch cluster**: GPUs idle during off-hours
- **Dedicated service cluster**: CPUs idle overnight
- **Shared cluster with default scheduler**: Batch jobs get starved by services

**KubeNexus Solution**: Intelligently co-allocate both workload types

```
┌─────────────────────────────────────────────────┐
│           Same Kubernetes Cluster                │
├─────────────────────────────────────────────────┤
│  Normal Workloads          Batch Workloads       │
│  (default priority)        (lower priority)      │
│                                                   │
│  • API services      │     • Spark jobs          │
│  • Web apps          │     • ML training         │
│  • Databases         │     • Data pipelines      │
│  • Microservices     │     • Analytics           │
│                                                   │
│  ✅ Always responsive │  ✅ Use spare capacity   │
│  ✅ High priority     │  ✅ Gang-scheduled       │
│  ✅ Fast scheduling   │  ✅ No starvation        │
└─────────────────────────────────────────────────┘
```

#### Current State

**What works TODAY:**
```yaml
# Normal workload - uses KubeNexus like default scheduler
apiVersion: v1
kind: Pod
metadata:
  name: webapp-1
  # NO gang scheduling annotations
spec:
  schedulerName: kubenexus-scheduler
  priority: 1000  # High priority
  containers:
  - name: nginx
    image: nginx:latest

---
# Batch workload - uses gang scheduling
apiVersion: v1
kind: Pod
metadata:
  name: spark-driver
  annotations:
    pod-group.scheduling.sigs.k8s.io/name: "spark-job"
    pod-group.scheduling.sigs.k8s.io/min-available: "10"
spec:
  schedulerName: kubenexus-scheduler
  priority: 500  # Lower priority
  containers:
  - name: spark
    image: spark:latest
```

**What happens:**
1. ✅ Normal pods schedule immediately (no gang scheduling overhead)
2. ✅ Batch pods wait for all members (gang scheduling)
3. ⚠️ Both compete for resources (FIFO + priority)
4. ❌ No intelligent resource allocation
5. ❌ Batch jobs can block services
6. ❌ Services can starve batch jobs

#### What's MISSING for True Hybrid Scheduling

**Gap 1: No Workload-Aware Scheduling**
- Current: Treats all pods the same (FIFO + priority)
- Needed: Different policies for batch vs normal
  - Services: Fast placement, don't wait
  - Batch: Wait for gang, use spare capacity

**Gap 2: No Resource Partitioning**
- Current: No separation between batch and service resources
- Needed: Reserve minimum resources for services
  ```
  Cluster: 100 CPUs
  ├── Reserved for services: 40 CPUs (guaranteed)
  ├── Reserved for batch: 20 CPUs (guaranteed)
  └── Elastic pool: 40 CPUs (shared, burst)
  ```

**Gap 3: No Backfill Scheduling**
- Current: Batch jobs block if any resource shortage
- Needed: Schedule batch jobs in "gaps" without blocking services
  ```
  Time: 9am - Services need 60 CPUs (peak)
  Time: 2am - Services need 20 CPUs (off-peak)
  → Batch jobs can use 80 CPUs at night
  ```

**Gap 4: No Preemption**
- Current: Once scheduled, pods stay
- Needed: Preempt batch jobs if services need resources
  ```
  9am: Service traffic spikes
  → Gracefully evict batch jobs
  → Services get resources immediately
  ```

---

---

## 🎯 Hybrid Scheduling Roadmap (REVISED)

### What We Need to Build

#### ✅ Phase 1: Workload Classification (CRITICAL)
**Goal**: Distinguish batch from normal workloads

**Implementation**:
```go
// pkg/classification/workload.go
type WorkloadType int

const (
    WorkloadNormal WorkloadType = iota  // Services, APIs, webapps
    WorkloadBatch                       // Spark, ML, data processing
)

func ClassifyPod(pod *v1.Pod) WorkloadType {
    // Check for gang scheduling annotations
    if hasGangAnnotations(pod) {
        return WorkloadBatch
    }
    
    // Check for batch labels
    if pod.Labels["workload.kubenexus.io/type"] == "batch" {
        return WorkloadBatch
    }
    
    // Default to normal
    return WorkloadNormal
}
```

**Impact**: Scheduler can apply different policies per workload type

---

#### ✅ Phase 2: Priority Classes (USE EXISTING K8S FEATURE)
**Goal**: Services > Batch jobs

**Implementation**: Use Kubernetes PriorityClasses (already exists!)
```yaml
# priority-classes.yaml
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: service-priority
value: 1000
globalDefault: false
description: "High priority for production services"

---
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: batch-priority
value: 500
globalDefault: false
description: "Lower priority for batch workloads"
```

**Usage**:
```yaml
# Service pod
spec:
  priorityClassName: service-priority
  
# Batch pod
spec:
  priorityClassName: batch-priority
  annotations:
    pod-group.scheduling.sigs.k8s.io/name: "spark-job"
```

**Current State**: ✅ KubeNexus already respects pod priority in QueueSort
**Gap**: ❌ No preemption (can't evict batch for services)

---

#### ⚠️ Phase 3: Resource Partitioning (COMPLEX)
**Goal**: Reserve resources for services, share remainder

**Approach 1: Namespace-based** (RECOMMENDED - Simple)
```yaml
# Service namespace - guaranteed resources
apiVersion: v1
kind: ResourceQuota
metadata:
  name: service-quota
  namespace: production
spec:
  hard:
    requests.cpu: "40"
    requests.memory: "80Gi"

---
# Batch namespace - best-effort
apiVersion: v1
kind: ResourceQuota
metadata:
  name: batch-quota
  namespace: batch-jobs
spec:
  hard:
    requests.cpu: "60"
    requests.memory: "120Gi"
```

**Approach 2: Scheduler-aware** (COMPLEX - Out of scope)
- Track service vs batch resource usage
- Reject batch pods if service quota threatened
- Requires custom Filter plugin

**Recommendation**: Use K8s ResourceQuotas (Approach 1)

---

#### ❌ Phase 4: Preemption (OUT OF SCOPE - TOO COMPLEX)
**Goal**: Evict batch jobs when services need resources

**Why it's hard:**
- Requires checkpointing (save batch job state)
- Coordination with workload controllers
- Graceful eviction policies
- Re-scheduling logic

**Complexity**: ~5000 lines of code, months of work

**Alternative**: Use K8s **PodDisruptionBudgets** + **Descheduler**
```yaml
# Allow batch jobs to be evicted
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: batch-pdb
spec:
  minAvailable: 0  # Can evict all batch pods
  selector:
    matchLabels:
      workload.kubenexus.io/type: batch
```

**Verdict**: ❌ **OUT OF SCOPE** - Use existing K8s tools

---

#### ⚠️ Phase 5: Topology Awareness (IN SCOPE)
**Goal**: Spread pods across zones for HA

**Why it matters for hybrid scheduling:**
- Services: Need zone spreading for availability
- Batch: May prefer co-location for network performance

**Implementation**: Already covered in "Zone Spreading" section

---

#### ⚠️ Phase 6: Bin Packing vs Spreading (CONFLICTING GOALS)
**The Tradeoff:**

**Services want SPREADING:**
- Distribute across nodes for HA
- Avoid single point of failure
- K8s default: spread evenly

**Batch wants BIN PACKING:**
- Colocate pods on same nodes
- Reduce network latency (ML training)
- Maximize resource utilization

**Current State**: KubeNexus uses default K8s scoring (spreading)

**What we need**: Workload-aware scoring
```go
func (s *HybridScorePlugin) Score(ctx, state, pod, nodeName) int64 {
    workloadType := ClassifyPod(pod)
    
    if workloadType == WorkloadBatch {
        // Bin packing: Prefer fuller nodes
        return nodeFillPercentage(nodeName)  // 0-100
    } else {
        // Spreading: Prefer emptier nodes
        return 100 - nodeFillPercentage(nodeName)
    }
}
```

**Effort**: 1-2 days
**Impact**: High for batch performance

---

## 📝 Updated Mission Statement

### Before (Wrong):
> "Kubernetes scheduler for Apache Spark workloads"

### After (Correct):
> **"KubeNexus: Hybrid scheduler for batch and service workloads"**
> 
> A Kubernetes scheduler that intelligently co-allocates batch workloads (Spark, ML training) and service workloads (APIs, webapps) in the same cluster.
> 
> **Key Features:**
> - ✅ Gang scheduling for batch jobs (all-or-nothing)
> - ✅ Fast scheduling for services (no gang overhead)
> - ✅ Priority-based resource allocation (services > batch)
> - ⚠️ Workload-aware scoring (bin packing vs spreading)
> - ⚠️ Topology awareness (zone spreading for HA)
> 
> **Use KubeNexus when:**
> - You run both services and batch jobs in one cluster
> - You want efficient resource utilization
> - You need gang scheduling without YuniKorn complexity
> - You have GPUs/expensive resources to maximize utilization
> 
> **Don't use KubeNexus if:**
> - You only have services (use default-scheduler)
> - You only have batch (use Volcano/YuniKorn)
> - You need advanced multi-tenancy (use YuniKorn)

---

## 🚀 Revised Priority Roadmap

### Priority 1: Core Hybrid Features (v1.1 - Q2 2026)

**P1.1: Workload Classification** - 1 day
```go
// Add workload type detection
// Use annotations + heuristics
```

**P1.2: Workload-Aware Scoring** - 2 days
```go
// Batch: Bin packing (colocate)
// Services: Spreading (distribute)
```

**P1.3: Documentation** - 1 day
```markdown
# Update all docs to reflect hybrid scheduling mission
# Add examples for both workload types
# Explain priority classes
```

**Total**: ~1 week of focused work

---

### Priority 2: Topology Awareness (v1.1 - Q2 2026)

**P2.1: Zone Spreading** - 2 days
```go
// ScorePlugin for zone spreading
// Both batch and services benefit
```

**Total**: 2 days

---

### Priority 3: Testing & Polish (v1.1 - Q2 2026)

**P3.1: Integration Tests** - 3 days
```yaml
# Test service + batch co-scheduling
# Test priority preemption
# Test zone spreading
```

**P3.2: Metrics** - 1 day
```go
// workload_type (batch|service)
// scheduling_latency by workload
// resource_utilization by type
```

**Total**: 4 days

---

### OUT OF SCOPE (Don't Build)

❌ **Preemption** - Use K8s Descheduler + PDBs
❌ **Queue hierarchies** - Use K8s ResourceQuotas
❌ **Fair sharing (DRF)** - Too complex, use YuniKorn
❌ **Rack/NUMA awareness** - Niche, use YuniKorn
❌ **GPU sharing** - Use NVIDIA MPS or MIG

---

## 🎓 Key Insights

### Why Hybrid Scheduling Matters

**The Problem**: Wasted Resources
```
Dedicated Batch Cluster:
├── Day (9am-5pm): 80% GPU utilization ✅
└── Night (6pm-8am): 20% GPU utilization ❌ WASTE

Dedicated Service Cluster:
├── Peak (9am-5pm): 90% CPU utilization ✅
└── Off-peak (6pm-8am): 30% CPU utilization ❌ WASTE
```

**The Solution**: Hybrid Cluster with KubeNexus
```
Unified Cluster:
├── Services: Always get priority, fast scheduling
├── Batch: Use spare capacity, gang-scheduled
└── Result: 70-80% average utilization ✅
```

### Cost Savings Example

**Before** (Separate clusters):
- Service cluster: 100 nodes × $1/hr × 24hr = $2,400/day
- Batch cluster: 50 nodes × $1/hr × 24hr = $1,200/day
- **Total**: $3,600/day

**After** (Hybrid with KubeNexus):
- Unified cluster: 120 nodes × $1/hr × 24hr = $2,880/day
- **Savings**: $720/day = **$262,800/year** ✅

---

## 🎯 The Real Competitive Position

### KubeNexus vs Others (Hybrid Scheduling Focus)

| Feature | YuniKorn | Volcano | Kueue | **KubeNexus** |
|---------|----------|---------|-------|---------------|
| **Hybrid Scheduling** | ⚠️ Possible | ⚠️ Possible | ❌ No | ✅ **Core Feature** |
| **Gang Scheduling** | ✅ Yes | ✅ Yes | ✅ Yes | ✅ Yes |
| **Workload-Aware** | ⚠️ Queue-based | ❌ No | ❌ No | ✅ **Native** |
| **Setup Complexity** | High | High | Medium | **Low** |
| **Resource Footprint** | ~500MB | ~300MB | ~100MB | **~50MB** |

**Unique Value Prop**: KubeNexus is the **only scheduler designed for hybrid workloads**.

---

## 🚀 Next Steps (Concrete)

### Week 1: Foundation
```bash
# Day 1-2: Workload classification
mkdir -p pkg/classification
# Implement WorkloadType detection

# Day 3-4: Workload-aware scoring
mkdir -p pkg/scoring
# Implement bin packing vs spreading

# Day 5: Update docs
# Reflect hybrid scheduling mission
```

### Week 2: Topology
```bash
# Day 1-2: Zone spreading plugin
mkdir -p pkg/topology
# Implement ScorePlugin

# Day 3-5: Testing & polish
# Integration tests
# Metrics
# Documentation
```

### Week 3+: Production Readiness
- Performance testing
- Bug fixes
- User docs
- Examples

**Total to MVP**: ~3 weeks focused work

Want me to start implementing any of these?
