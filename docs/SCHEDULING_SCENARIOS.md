# KubeNexus Scheduler - Real-World Scheduling Scenarios

This document explains how KubeNexus handles different workload types through practical scenarios.

**Core Goal:** Co-locate stateless services and batch workloads efficiently while maintaining high availability for services and maximizing resource utilization for batch jobs.

---

## Scenario 1: Empty Cluster - First Workloads Arrive

### Workloads:
1. **Web API (Service)** - 3 replicas, 2 CPU each
2. **ML Training Job (Batch)** - 4 pods, 8 CPU each (gang-scheduled)

### What Happens:

#### Web API Pods (Service Type):
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-api
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: api
        resources:
          requests:
            cpu: "2"
        readinessProbe:  # ← Detected as Service workload
          httpGet:
            path: /health
```

**Plugin Flow:**
1. **Workload Classification** → Detects "Service" (has readiness probe, resource limits)
2. **Hybrid Scoring** → Prefers EMPTY nodes (spreading strategy)
3. **Topology Spreading** → Distributes across zones for HA

**Result:**
- Pod 1 → Node A (Zone 1) - Score: 100 (empty node)
- Pod 2 → Node B (Zone 2) - Score: 100 (empty node)  
- Pod 3 → Node C (Zone 3) - Score: 100 (empty node)

✅ **Each pod gets its own node for fault tolerance!**

#### ML Training Job (Batch/Gang):
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ml-worker-1
  labels:
    pod-group.scheduling.kubenexus.io/name: "ml-training"
    pod-group.scheduling.kubenexus.io/min-available: "4"  # ← Gang constraint
spec:
  containers:
  - name: worker
    resources:
      requests:
        cpu: "8"
```

**Plugin Flow:**
1. **Workload Classification** → Detects "Batch" (no liveness probe, batch labels)
2. **Coscheduling Plugin** → All 4 pods MUST schedule together (gang constraint)
3. **Hybrid Scoring** → Prefers FULLER nodes (bin packing)
4. **Decision:** Nodes A, B, C have service pods (not full enough)
5. **Solution:** Schedule all 4 ML pods on Nodes D, E (co-location)

**Result:**
- ML Pod 1, 2 → Node D (16 CPU used, packed together)
- ML Pod 3, 4 → Node E (16 CPU used, packed together)

✅ **Fast inter-pod communication, efficient resource use!**

---

## Scenario 2: Cluster Under Load - Resource Competition

### Current State:
- Nodes A, B, C: Running service pods (30% utilized)
- Nodes D, E: Running batch ML jobs (90% utilized)

### New Workloads Arrive:
1. **Database (Service)** - 1 pod, 4 CPU
2. **Spark Job (Batch)** - 6 pods, 4 CPU each (gang-scheduled)

### What Happens:

#### Database Pod (Service):
```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
spec:
  template:
    spec:
      containers:
      - name: postgres
        resources:
          requests:
            cpu: "4"
      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: postgres-data  # ← Detected as Service (StatefulSet + PVC)
```

**Plugin Flow:**
1. **Workload Classification** → Service (StatefulSet, has PVC)
2. **Hybrid Scoring** evaluates:
   - Node A (30% util) → Score: 70 (prefer emptier)
   - Node D (90% util) → Score: 10 (avoid full nodes)
3. **Topology** considers zone spread

**Result:**
- Database → Node A (Zone 1)
- ✅ Avoids batch-heavy nodes D/E
- ✅ Gets predictable resources for stateful workload

#### Spark Job (6 pods, gang):
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: spark-executor-1
  labels:
    pod-group.scheduling.kubenexus.io/name: "spark-job"
    pod-group.scheduling.kubenexus.io/min-available: "6"
spec:
  priority: 100  # Higher priority
  containers:
  - name: executor
    resources:
      requests:
        cpu: "4"
```

**Plugin Flow:**
1. **Workload Classification** → Batch
2. **Coscheduling** → Waits for 6 slots available
3. **Problem:** Not enough space!
   - Nodes D, E only have 10% free each
   - Nodes A, B, C have service pods (should avoid)

4. **Gang Preemption Kicks In:**
   - Checks priority: Spark (priority 100) vs services (priority 50)
   - Decision: Can't preempt services (higher priority than batch!)
   - Spark job WAITS in queue

5. **Later:** Lower-priority batch job finishes on Node D
6. **Coscheduling** detects 6 slots available on D, E
7. All 6 Spark pods scheduled atomically

**Result:**
- Spark Pods 1-3 → Node D (co-located)
- Spark Pods 4-6 → Node E (co-located)
- ✅ Service pods remain untouched on A, B, C

---

## Scenario 3: Gang Deadlock - Small Jobs Block Large Job

### Current State:
- Node A, B: 4 small batch jobs each (2 GPU per job)
- Total: 8 small jobs × 2 GPU = 16 GPUs in use

### New Workload:
**Distributed Training (Gang)** - 8 pods, 2 GPU each (needs 16 GPUs total)

### Without Gang Preemption (Standard K8s):
```
❌ PROBLEM:
1. Training Pod 1 tries to schedule
2. Can't find 8 nodes with 2 GPUs free
3. Waits...
4. Small jobs keep running
5. DEADLOCK: Gang never schedules even though cluster has 16 GPUs!
```

### With KubeNexus Gang Preemption:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: training-worker-1
  labels:
    pod-group.scheduling.kubenexus.io/name: "distributed-training"
    pod-group.scheduling.kubenexus.io/min-available: "8"
spec:
  priority: 100  # High priority
  containers:
  - name: worker
    resources:
      requests:
        nvidia.com/gpu: "2"
```

**Plugin Flow:**
1. Training Pod 1 fails to schedule → **PostFilter** called
2. **Gang Preemption** detects:
   - Gang needs: 8 pods × 2 GPU = 16 GPUs total
   - Priority: Training (100) vs Small Jobs (50)
3. **Finds victims:**
   - Selects 8 small jobs (lowest priority first)
   - Verifies: 8 × 2 GPU = 16 GPUs freed ✓
4. **Preempts all 8 small jobs ATOMICALLY**
5. Gang scheduling now succeeds!

**Result:**
- All 8 training pods schedule together across nodes A, B
- Small jobs are terminated and requeued
- ✅ No deadlock, high-priority work proceeds

---

## Scenario 4: Mixed Workload - Optimal Placement

### Workloads Arrive Simultaneously:
1. **API Gateway (Service)** - 5 replicas
2. **Data Pipeline (Batch)** - 10 pods
3. **Redis (Service)** - 3 pods

### Phase 1: Priority Sorting (QueueSort)

**Coscheduling Plugin** sorts queue:
1. Services first (critical path, need HA)
2. Batch workloads second (can be packed)

**Order:**
- API Gateway (5 pods)
- Redis (3 pods)
- Data Pipeline (10 pods)

### Phase 2: Service Pods Schedule

#### API Gateway:
- **Hybrid Scoring:** Prefer empty nodes
- **Topology Spread:** Distribute across zones
- **Result:** 5 pods → 5 different nodes (A, B, C, D, E)

#### Redis:
- **Same strategy**
- **Result:** 3 pods → Node F, G, H (new nodes, spread out)

### Phase 3: Batch Pods Schedule

#### Data Pipeline:
```
Hybrid Scoring looks at cluster:
- Nodes A-E: 20% utilized (have API pods)
- Nodes F-H: 20% utilized (have Redis)
- Nodes I, J: 0% utilized (empty)

Decision: 
- Skip nodes with services (A-H) to avoid interference
- Use empty nodes I, J for batch work
- Result: All 10 pipeline pods → Nodes I, J (packed tight)
```

### Final State:
- **Services** get isolated nodes with room to scale
- **Batch work** packed on dedicated nodes
- ✅ Clear separation, optimal resource use!

---

## Scenario 5: Autoscaling Event - Services Need More Resources

### Current State:
- Nodes A-D: Services (40% util)
- Nodes E-F: Batch jobs (95% util, packed)

### Event: Traffic spike, API needs to scale from 4 → 8 pods

### New API Pods (4 more):

**Plugin Flow:**
1. **Classification** → Service
2. **Hybrid Scoring:**
   - Nodes A-D: 40% util → Score 60
   - Nodes E-F: 95% util → Score 5
3. Clear winner: Spread on existing service nodes

**Result:**
- New API pods → Nodes A-D (now 70% utilized)
- ✅ Batch nodes E-F remain untouched
- ✅ Services get resources without disrupting batch work

### If All Nodes Were Full:

**Gang Preemption:**
1. API pods have priority 100
2. Batch jobs have priority 50
3. Preemption finds lowest-priority batch pods
4. Frees resources on nodes A-D
5. API pods schedule on freed resources

**Result:**
- ✅ Critical services get resources immediately
- Lower-priority batch work pauses and requeues

---

## Scenario 6: Node Failure - Different Recovery

### Event: Node E fails (had batch pods)

### Batch Pods Recovery:
1. Batch pods on Node E are lost
2. Rescheduling triggered
3. **Classification** → Batch
4. **Hybrid Scoring** → Prefers Node F (already has batch, 95% util)
5. **Result:** Batch pods repack onto Node F
6. ✅ Efficient recovery, maintains co-location

### If It Was a Service Node:
1. Service pods on failed node lost
2. Rescheduling triggered  
3. **Classification** → Service
4. **Hybrid Scoring** → Prefers emptier nodes (spreading)
5. **Topology** → Ensures zone distribution
6. **Result:** Service pods spread across A, B, C
7. ✅ High availability maintained!

---

## Key Scheduling Patterns Summary

### ✅ Services (Stateless/Stateful):
- **Always spread** across nodes and zones
- Get their own space
- Higher priority in preemption
- Predictable performance
- Fast scaling without waiting

### ✅ Batch Workloads:
- **Always packed** onto same nodes
- Co-located for fast communication
- Lower priority (can be preempted)
- Efficient resource utilization
- Gang scheduling for distributed jobs

### ✅ Gang Scheduling:
- All-or-nothing placement
- Prevents partial scheduling deadlocks
- Can preempt multiple victims to free space
- Critical for distributed ML/HPC
- Only applies when `minAvailable > 1`

### ✅ Resource Optimization:
- Services: Isolated, room to scale
- Batch: Dense packing, maximize throughput
- Clear separation prevents interference
- Cluster utilization maximized
- Automatic workload detection

---

## How to Label Your Workloads

### Regular Service (Independent Scheduling):
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-api
spec:
  replicas: 10
  template:
    # NO gang labels needed - each pod schedules independently
    spec:
      containers:
      - name: api
        resources:
          requests:
            cpu: "2"
        readinessProbe:  # Automatically detected as Service
          httpGet:
            path: /health
```

### Gang-Scheduled Batch Job:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ml-worker-1
  labels:
    # Gang scheduling labels
    pod-group.scheduling.kubenexus.io/name: "ml-training"
    pod-group.scheduling.kubenexus.io/min-available: "8"  # All 8 must schedule together
spec:
  priority: 100
  containers:
  - name: worker
    resources:
      requests:
        cpu: "8"
        nvidia.com/gpu: "2"
```

---

## Plugin Interaction Flow

```
Pod Submitted
     ↓
[Workload Classification]
     ↓
  Service?     Batch?
     ↓           ↓
[QueueSort: Services First, Batch Second]
     ↓
[PreFilter: Check gang requirements]
     ↓
[Filter: Node compatibility]
     ↓
[Score: Hybrid scoring based on workload type]
  • Service → Prefer empty nodes (spreading)
  • Batch → Prefer full nodes (bin packing)
     ↓
[Permit: Gang waits for all members]
     ↓
[Bind: Pod assigned to node]
     ↓
If scheduling fails:
     ↓
[PostFilter: Gang Preemption]
  • Find lower-priority victims
  • Preempt atomically for gang
     ↓
[Retry scheduling]
```

---

## Why This Approach Works

1. **Workload Classification** understands pod intent (service vs batch)
2. **Hybrid Scoring** applies opposite strategies (spread vs pack)
3. **Gang Scheduling** prevents deadlocks for distributed workloads
4. **Gang Preemption** unblocks high-priority gangs
5. **Topology Awareness** ensures HA for services
6. **Automatic Detection** - no complex configuration needed

**Result: Services stay responsive, batch jobs run efficiently, resources fully utilized!** 🚀
