# KubeNexus Architecture

## ProfileClassifier: The Classification Hub

KubeNexus starts with **ProfileClassifier**, which runs in PreFilter and writes a `SchedulingProfile` into CycleState that every other plugin reads:

```go
type SchedulingProfile struct {
    TenantTier    TenantTier   // gold / silver / bronze
    TenantName    string       // team or queue name
    WorkloadType  WorkloadType // training / inference / batch / service / interactive
    IsGang        bool
    IsPreemptible bool
    Priority      int32
    QoSClass      v1.PodQOSClass
}
```

## 3-Axis Scheduling

### WHO (Tenant Tier)

**How Teams Map to Tenant Tiers:**

**Option 1: Namespace Labels** (Recommended)
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: ml-team-premium
  labels:
    tenant.kubenexus.io/name: "recommendation-team"
    tenant.kubenexus.io/tier: "gold"   # gold / silver / bronze
```

**Option 2: Kueue Integration**
```yaml
# Pod automatically labeled by Kueue
metadata:
  labels:
    kueue.x-k8s.io/queue-name: "premium-queue"
# ProfileClassifier reads LocalQueue → ClusterQueue → Tier mapping
```

**Option 3: PriorityClass Fallback**
```yaml
spec:
  priorityClassName: high-priority    # → Mapped to Gold tier
```

**TenantHardware Plugin**: Routes tenants to appropriate hardware (Gold→H100, Silver→A100, Bronze→L40)

### WHAT (Workload Type)

**Workload Detection Hierarchy:**
1. Explicit label: `workload.kubenexus.io/type: training`
2. Operator detection: PyTorchJob, TFJob, SparkApplication
3. PodSpec analysis: GPU requests, resource patterns
4. Default: Service workload

**WorkloadAware Plugin**: Adapts placement strategy
- Training/Batch → Bin packing (consolidate for locality)
- Service → Spreading (distribute for HA)

### WHERE (Hardware Topology)

**Topology-Aware Plugins:**
- **NUMATopology**: CPU/Memory/GPU NUMA alignment
- **NetworkFabric**: NVSwitch, InfiniBand, RoCE awareness
- **VRAMScheduler**: GPU memory capacity matching
- **TopologySpread**: Zone/rack spreading for HA

## Plugin Pipeline

```
PreFilter (Classification)
  ↓
ProfileClassifier → Determines WHO + WHAT
  ↓
CycleState.SchedulingProfile (shared by all plugins)
  ↓
Filter (Feasibility)
  ↓
ResourceReservation → Prevents fragmentation
NUMATopology → NUMA constraints
  ↓
Score (Optimization)
  ↓
TenantHardware → WHERE (hardware tier)
WorkloadAware → Placement strategy
VRAMScheduler → GPU memory fit
NetworkFabric → Network topology
BackfillScoring → Opportunistic placement
TopologySpread → Multi-zone HA
  ↓
Reserve/Permit
  ↓
Coscheduling → Gang coordination
ResourceReservation → Reserve capacity
  ↓
PostFilter (Preemption)
  ↓
GangPreemption → Atomic gang preemption
```

## Integration Architecture

### Kueue Integration

KubeNexus reads Kueue queue metadata for tenant classification:

```yaml
apiVersion: kueue.x-k8s.io/v1beta1
kind: ClusterQueue
metadata:
  name: premium-queue
  labels:
    tenant.kubenexus.io/tier: "gold"
```

Pods admitted by Kueue automatically inherit tenant tier from ClusterQueue.

### Kubeflow Training Operator

Works out-of-the-box with PyTorchJob, TFJob, MPIJob:

```yaml
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: distributed-training
spec:
  pytorchReplicaSpecs:
    Worker:
      template:
        metadata:
          labels:
            pod-group.scheduling.kubenexus.io/name: "pytorch-job"
            pod-group.scheduling.kubenexus.io/min-available: "8"
```

ProfileClassifier auto-detects PyTorchJob → WorkloadType=Training → Bin packing strategy

### Spark Operator

Native support for SparkApplication:

```yaml
apiVersion: sparkoperator.k8s.io/v1beta2
kind: SparkApplication
metadata:
  name: spark-job
spec:
  driver:
    labels:
      pod-group.scheduling.kubenexus.io/name: "spark-job"
      pod-group.scheduling.kubenexus.io/min-available: "11"  # 1 driver + 10 executors
```

See [Spark Integration Guide](SPARK_OPERATOR_INTEGRATION.md) for details.

## Multi-Tenant Fairness

### Tenant Hierarchy

```
Gold Tier:   High priority, preempts Silver/Bronze, premium hardware
Silver Tier: Medium priority, preempts Bronze, standard hardware
Bronze Tier: Low priority, best-effort, budget hardware
```

### Starvation Prevention

**Age-based priority boost** (Coscheduling plugin):
- Pods waiting >60s get priority boost
- Within same tier: FIFO ordering
- Prevents small jobs from being starved by large jobs

**Example:**
```
Small gang (4 pods) waiting 2 minutes
Large gang (64 pods) arrives
Small gang gets priority boost → schedules first (prevents indefinite starvation)
```

### Fair Preemption

**GangPreemption** ensures atomic preemption:
- Preempts entire gang at once (not individual pods)
- Respects tenant hierarchy (Gold can preempt Silver/Bronze)
- Never preempts partially (avoids deadlock)

## Scheduler Configuration

KubeNexus uses Kubernetes native scheduler config:

```yaml
apiVersion: kubescheduler.config.k8s.io/v1
kind: KubeSchedulerConfiguration
profiles:
- schedulerName: kubenexus-scheduler
  plugins:
    preFilter:
      enabled:
      - name: ProfileClassifier  # MUST run first
      - name: Coscheduling
      - name: ResourceReservation
    
    filter:
      enabled:
      - name: ResourceReservation
      - name: NUMATopology
    
    score:
      enabled:
      - name: TenantHardware     # WHO
      - name: WorkloadAware      # WHAT  
      - name: VRAMScheduler      # WHERE
      - name: NetworkFabric      # WHERE
      - name: BackfillScoring
      - name: TopologySpread
    
    permit:
      enabled:
      - name: Coscheduling       # Gang coordination
    
    postFilter:
      enabled:
      - name: GangPreemption
```

## Performance Considerations

### Informer Cache

All plugins use shared informer cache for pod/node listing. Cache sync guaranteed before scheduling starts.

### Scoring Performance

- TenantHardware: O(1) per node (label lookup)
- WorkloadAware: O(P) per node (P = pods on node)
- VRAMScheduler: O(1) per node (node labels or DRA)
- NetworkFabric: O(1) per node (label lookup)

### Gang Scheduling Performance

- Permit phase: O(G) where G = gang size
- Uses in-memory gang state (no API calls during permit)
- Timeout: 10s default (configurable)

## High Availability

KubeNexus supports leader election for multi-replica deployments:

```yaml
leaderElection:
  leaderElect: true
  resourceName: kubenexus-scheduler
  resourceNamespace: kubenexus-system
```

Run 3+ replicas for HA. Only leader schedules, followers take over on failure.
