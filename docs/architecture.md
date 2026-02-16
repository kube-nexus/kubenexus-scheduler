# KubeNexus Scheduler Architecture

## Overview

KubeNexus is a Kubernetes scheduler built on the **Scheduler Framework** that provides advanced scheduling capabilities for batch workloads, particularly optimized for Apache Spark and other gang-scheduled applications.

## Design Principles

1. **Lightweight** - Minimal resource overhead
2. **Modular** - Plugin-based architecture
3. **Compatible** - Works alongside default Kubernetes scheduler
4. **Extensible** - Easy to add new scheduling policies
5. **Observable** - Rich metrics and logging

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                      Kubernetes Control Plane                    │
│                                                                   │
│  ┌────────────────────┐          ┌─────────────────────────┐   │
│  │   API Server       │◄─────────┤  kubenexus-scheduler    │   │
│  │                    │          │                         │   │
│  └────────────────────┘          └─────────────────────────┘   │
│           ▲                                    │                 │
│           │                                    ▼                 │
│           │                      ┌──────────────────────────┐  │
│           │                      │  Scheduler Framework     │  │
│           │                      │                          │  │
│           │                      │  ┌────────────────────┐ │  │
│           │                      │  │  QueueSort         │ │  │
│           │                      │  │  (Priority-based)  │ │  │
│           │                      │  └────────────────────┘ │  │
│           │                      │  ┌────────────────────┐ │  │
│           │                      │  │  PreFilter         │ │  │
│           │                      │  │  (Gang validation) │ │  │
│           │                      │  └────────────────────┘ │  │
│           │                      │  ┌────────────────────┐ │  │
│           │                      │  │  Filter            │ │  │
│           │                      │  │  (Node selection)  │ │  │
│           │                      │  └────────────────────┘ │  │
│           │                      │  ┌────────────────────┐ │  │
│           │                      │  │  Permit            │ │  │
│           │                      │  │  (Gang scheduling) │ │  │
│           │                      │  └────────────────────┘ │  │
│           │                      │  ┌────────────────────┐ │  │
│           │                      │  │  Reserve           │ │  │
│           │                      │  │  (Resource lock)   │ │  │
│           │                      │  └────────────────────┘ │  │
│           │                      │  ┌────────────────────┐ │  │
│           │                      │  │  Bind              │ │  │
│           │                      │  └────────────────────┘ │  │
│           │                      └──────────────────────────┘  │
│           │                                                     │
│           └─────────────────────────────────────────────────────┘
└─────────────────────────────────────────────────────────────────┘
```

## Core Components

### 1. Coscheduling Plugin

**Purpose**: Implements gang scheduling (all-or-nothing scheduling)

**Extension Points**:
- `QueueSort`: Orders pod groups by priority and creation timestamp
- `PreFilter`: Validates that enough pods exist in the group
- `Filter`: Additional node-level filtering
- `Permit`: Waits for all pods in a group before allowing scheduling
- `UnReserve`: Rejects all pods if one fails

**Algorithm**:
```
1. Pod arrives for scheduling
2. Check if pod belongs to a pod group (via labels)
3. If yes:
   a. Check if total pods >= min-available
   b. If not, reject immediately (PreFilter)
   c. If yes, count running + waiting pods
   d. If count >= min-available, allow all waiting pods
   e. If count < min-available, wait for more pods
4. If no pod group, schedule normally
```

### 2. Resource Reservation Plugin

**Purpose**: Pre-reserves cluster resources to prevent resource starvation

**Extension Points**:
- `Reserve`: Creates resource reservation CRDs
- `UnReserve`: Cleans up reservations on failure

**How it Works**:
- When a driver pod is scheduled, reserves resources for all executors
- Prevents other workloads from using those resources
- Uses Custom Resource Definitions (CRDs) to track reservations

### 3. PreExtender Plugin

**Purpose**: Prepares pods for external scheduler extenders

**Extension Points**:
- `PreFilter`: Adds/normalizes labels for compatibility
- `Filter`: Additional validation

## Scheduling Flow

### Normal Pod Scheduling
```
Pod Creation → PreFilter → Filter → Score → Reserve → Bind
```

### Gang-Scheduled Pod Group
```
Driver Pod:
  1. PreFilter: Check total pods >= min-available
  2. Filter: Node selection
  3. Permit: Wait for executor pods
  4. Reserve: Create resource reservations
  5. Bind: Schedule driver

Executor Pods (in parallel):
  1. PreFilter: Validate group
  2. Filter: Check reserved resources
  3. Permit: Wait for all executors
  4. When all ready → Allow all → Bind all executors
```

## Data Structures

### PodGroupInfo
```go
type PodGroupInfo struct {
    name              string
    namespace         string
    minAvailable      int
    timestamp         time.Time
    scheduledPods     int
    waitingPods       int
    lastUpdateTime    time.Time
}
```

### Pod Labels
```yaml
pod-group.scheduling.kubenexus.io/name: "spark-app-1"
pod-group.scheduling.kubenexus.io/min-available: "10"
```

## Performance Optimizations

1. **In-Memory Cache**: PodGroupInfo map for fast lookups
2. **Lock-Free Reads**: Use sync.RWMutex for concurrent access
3. **Lazy Cleanup**: Garbage collect old PodGroupInfo entries
4. **Batch Operations**: Allow multiple pods simultaneously

## Failure Handling

### Timeout Scenario
- If pod group doesn't complete within timeout
- All waiting pods are rejected via `Unreserve`
- Resources are freed for other workloads

### Partial Failure
- If one pod in group fails to bind
- Entire group is rejected
- Prevents partial scheduling

## Metrics & Observability

### Exposed Metrics (Prometheus format)
- `kubenexus_scheduling_attempts_total`
- `kubenexus_scheduling_duration_seconds`
- `kubenexus_pod_group_size`
- `kubenexus_waiting_pods`
- `kubenexus_resource_reservations`

### Log Levels
- V(2): Important events (gang complete, failures)
- V(3): Informational (permit wait, group status)
- V(4): Debug (individual pod decisions)

## Comparison with Other Schedulers

### vs Default Kubernetes Scheduler
- ✅ Gang scheduling support
- ✅ Resource reservation
- ✅ FIFO ordering for fairness
- ❌ No hierarchical queues (yet)

### vs Apache YuniKorn
- ✅ Much lighter weight (~50MB vs ~200MB+)
- ✅ Simpler configuration
- ✅ Faster deployment
- ❌ No hierarchical queue management
- ❌ No advanced preemption policies

### vs Volcano
- ✅ Native Kubernetes scheduler framework
- ✅ Better performance
- ✅ Less complex
- ❌ Fewer queue management features
- ❌ No job lifecycle management

## Future Enhancements

1. **Hierarchical Queues**: Multi-level queue management
2. **Advanced Preemption**: Priority-based preemption
3. **Fair Sharing**: Resource quotas per namespace/tenant
4. **Auto-Scaling Integration**: Work with cluster autoscaler
5. **GPU-Aware Scheduling**: Optimize for GPU workloads
6. **Bin-Packing Strategies**: Multiple packing algorithms

## Security Considerations

- RBAC properly configured
- Non-root containers (distroless images)
- Minimal permissions required
- Resource limits enforced
- Secrets managed via Kubernetes

## References

- [Kubernetes Scheduler Framework](https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/)
- [Gang Scheduling Paper](https://dl.acm.org/doi/10.1145/76263.76337)
- [Palantir Spark Scheduler](https://github.com/palantir/k8s-spark-scheduler)
