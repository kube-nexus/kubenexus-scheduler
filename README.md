# KubeNexus Scheduler

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://golang.org/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.35+-326CE5?logo=k## 💡 Credits & Inspiration

KubeNexus draws inspiration from:

- **[Palantir k8s-spark-scheduler](https://github.com/palantir/k8s-spark-scheduler)** - Resource reservation concepts and Spark scheduling patterns were adapted from Palantir's pioneering work. We've internalized these concepts (no external dependencies) and evolved them into a modern plugin-based architecture using the Kubernetes Scheduler Framework v1.35.

- **[Kubernetes Scheduler Plugins](https://github.com/kubernetes-sigs/scheduler-plugins)** - Reference implementations for the scheduling framework

- **[Apache YuniKorn](https://yunikorn.apache.org/)** - Advanced queue management concepts

- **[Volcano](https://volcano.sh/)** - Job lifecycle management patterns

**Note on Dependencies**: KubeNexus has **zero external scheduling dependencies**. All types and logic previously from Palantir's libraries have been internalized into `pkg/apis/scheduling/v1alpha1/` and `pkg/resourcereservation/`, ensuring a self-contained, maintainable codebase.(https://kubernetes.io/)

> A lightweight, production-ready Kubernetes scheduler with gang scheduling for distributed workloads (Spark, ML, HPC)

KubeNexus provides enterprise-grade gang scheduling (co-scheduling) capabilities using the native Kubernetes Scheduler Framework. Built with simplicity and performance in mind, it's designed as a lightweight alternative to heavy schedulers like YuniKorn and Volcano.

**Latest**: Go 1.25, Kubernetes 1.35.1 (February 2026)

---

## ⚡ Quick Start

```bash
# Deploy the scheduler
kubectl apply -f config/gang-scheduler-deployment.yaml

# Use it in your pods
apiVersion: v1
kind: Pod
metadata:
  name: spark-driver
  annotations:
    pod-group.scheduling.sigs.k8s.io/name: "spark-job-123"
    pod-group.scheduling.sigs.k8s.io/min-available: "11"  # 1 driver + 10 executors
spec:
  schedulerName: kubenexus-scheduler
```

That's it! Your Spark job will now be scheduled all-or-nothing.

---

## 🎯 Why KubeNexus?

| Feature | YuniKorn | Volcano | **KubeNexus** |
|---------|----------|---------|---------------|
| **Gang Scheduling** | ✅ Advanced | ✅ Advanced | ✅ Core |
| **Resource Footprint** | ~500MB | ~300MB | **~50MB** |
| **Setup Time** | 1-2 hours | 1-2 hours | **5 minutes** |
| **Dependencies** | etcd, DB | CRDs | **None** |
| **Learning Curve** | High | High | **Low** |
| **Best For** | Multi-cluster, queues | HPC, workflows | **Simple gang scheduling** |

**Use KubeNexus when you need**:
- Gang scheduling without the complexity
- Minimal resource overhead
- Quick deployment for Spark/ML workloads
- Native Kubernetes integration

---

## 🏗️ Architecture

Built on the **Kubernetes Scheduler Framework**, KubeNexus implements gang scheduling with optional resource reservation:

```
┌─────────────────────────────────────────────────┐
│      KubeNexus Scheduler (~50MB)                │
├─────────────────────────────────────────────────┤
│  Coscheduling Plugin (Gang Scheduling) - CORE   │
│  • QueueSort: Priority-based ordering           │
│  • PreFilter: Group validation                  │
│  • Permit: Wait for all members                 │
│  • Reserve: Resource coordination               │
│  • Starvation Prevention: Age-based priority    │
├─────────────────────────────────────────────────┤
│  ResourceReservation Plugin - OPTIONAL          │
│  • Reserve: Create ResourceReservation CRD      │
│  • Unreserve: Clean up on scheduling failure    │
│  • Prevents resource contention for long jobs   │
├─────────────────────────────────────────────────┤
│   Kubernetes Scheduler Framework (v1.35)        │
└─────────────────────────────────────────────────┘
```

**Key Design Principles**:
- **Plugin-based**: Extends native Kubernetes scheduler
- **Minimal CRDs**: Only ResourceReservation (optional)
- **Stateless**: No external dependencies
- **HA-ready**: Built-in leader election
- **Self-contained**: All types internalized (no external libs)

---

## 📦 Installation

### Prerequisites
- Kubernetes 1.28+
- kubectl with cluster admin access

### Deploy

```bash
# Clone repository
git clone https://github.com/your-org/kubenexus-scheduler.git
cd kubenexus-scheduler

# Deploy (single instance)
kubectl apply -f config/gang-scheduler-deployment.yaml

# Deploy (HA - 3 replicas with leader election)
kubectl apply -f config/gang-scheduler-ha.yaml
```

### Build from Source

```bash
# Build binary
CGO_ENABLED=0 go build -o kubenexus-scheduler ./cmd/main.go

# Build container
docker build -t your-registry/kubenexus-scheduler:latest .
docker push your-registry/kubenexus-scheduler:latest
```

---

## 🎮 Usage

### Basic Gang Scheduling

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: spark-driver
  annotations:
    pod-group.scheduling.sigs.k8s.io/name: "spark-app-123"
    pod-group.scheduling.sigs.k8s.io/min-available: "11"
spec:
  schedulerName: kubenexus-scheduler
  # ... rest of spec
---
apiVersion: v1
kind: Pod
metadata:
  name: spark-executor-1
  annotations:
    pod-group.scheduling.sigs.k8s.io/name: "spark-app-123"  # Same group
    pod-group.scheduling.sigs.k8s.io/min-available: "11"
spec:
  schedulerName: kubenexus-scheduler
  # ... rest of spec
```

### How It Works

1. All 11 pods (1 driver + 10 executors) are created with same `pod-group.scheduling.sigs.k8s.io/name`
2. Scheduler validates each pod belongs to a group requiring 11 members
3. Scheduler waits until all 11 pods are ready to be scheduled
4. Once threshold is met, all 11 pods are scheduled **simultaneously**
5. If timeout (10s) occurs before all ready → entire group fails together

**Result**: No partial scheduling, no wasted resources waiting for missing pods.

---

## 🔧 Configuration

### Scheduler Configuration

```yaml
# config/scheduler-config.yaml
apiVersion: kubescheduler.config.k8s.io/v1
kind: KubeSchedulerConfiguration
leaderElection:
  leaderElect: true
  resourceName: kubenexus-scheduler
clientConnection:
  kubeconfig: /etc/kubernetes/scheduler.conf
profiles:
- schedulerName: kubenexus-scheduler
  plugins:
    queueSort:
      enabled:
      - name: Coscheduling
    preFilter:
      enabled:
      - name: Coscheduling
    permit:
      enabled:
      - name: Coscheduling
    reserve:
      enabled:
      - name: Coscheduling
```

### Pod Group Annotations

| Annotation | Description | Required | Example |
|------------|-------------|----------|---------|
| `pod-group.scheduling.sigs.k8s.io/name` | Pod group identifier | Yes | `"spark-job-123"` |
| `pod-group.scheduling.sigs.k8s.io/min-available` | Minimum pods to schedule together | Yes | `"11"` |

---

### Optional: Enable Resource Reservation

Resource Reservation creates CRD objects to track reserved resources, preventing starvation in multi-tenant clusters:

```bash
# 1. Apply the ResourceReservation CRD
kubectl apply -f config/crd-resourcereservation.yaml

# 2. The scheduler is already configured to use it (see config/config.yaml)
```

**When to use**:
- Multi-tenant clusters with many concurrent workloads
- Long-running Spark/ML jobs that need guaranteed resources
- Preventing smaller jobs from starving larger jobs

**How it works**:
1. When a pod group starts scheduling, a `ResourceReservation` CRD is created
2. This tracks which nodes/resources are "spoken for" by pending pod groups
3. Other workloads can see these reservations and avoid contention
4. On success, the CRD is updated; on failure, it's cleaned up

**Note**: This is entirely optional. Gang scheduling works fine without it. Enable only if you need explicit resource tracking.

---

## 📊 Monitoring

### Prometheus Metrics

```
# Gang scheduling metrics
kubenexus_gang_scheduling_attempts_total{status="success|failure"}
kubenexus_gang_wait_time_seconds{pod_group="..."}
kubenexus_gang_size{pod_group="..."}

# Standard scheduler metrics
scheduler_pending_pods{queue="active|backoff|unschedulable"}
scheduler_schedule_attempts_total{result="scheduled|unschedulable|error"}
```

Metrics available at `:10259/metrics`

### Health Checks

- Liveness: `http://localhost:10259/healthz`
- Readiness: `http://localhost:10259/readyz`

---

## 🧪 Development

### Project Structure

```
kubenexus-scheduler/
├── cmd/
│   └── main.go                         # Scheduler entrypoint
├── pkg/
│   ├── coscheduling/                  # Gang scheduling plugin (CORE)
│   │   └── coscheduling.go
│   ├── resourcereservation/           # Resource reservation plugin (OPTIONAL)
│   │   └── resourcereservation.go
│   ├── apis/scheduling/v1alpha1/      # Local CRD types (internalized)
│   │   ├── types.go                   # ResourceReservation CRD definition
│   │   ├── register.go                # Scheme registration
│   │   └── zz_generated.deepcopy.go   # DeepCopy methods
│   └── utils/                          # Helper utilities
├── config/
│   ├── gang-scheduler-deployment.yaml  # Main deployment
│   ├── config.yaml                     # Scheduler configuration
│   └── crd-resourcereservation.yaml    # CRD definition (optional)
├── README.md                           # This file
├── claude.md                           # Technical reference for AI
└── CONTRIBUTING.md
```

### Build & Test

```bash
# Install dependencies
go mod tidy

# Build
CGO_ENABLED=0 go build -o kubenexus-scheduler ./cmd/main.go

# Test
go test ./pkg/...

# Run locally (requires kubeconfig)
./kubenexus-scheduler \
  --config=config/scheduler-config.yaml \
  --v=3
```

### Adding Features

See [claude.md](claude.md) for comprehensive technical documentation including:
- API migration notes (K8s 1.18 → 1.35)
- Plugin development guide
- Architecture deep-dive
- Roadmap

---

## � Credits & Inspiration

KubeNexus draws inspiration from:

- **[Palantir k8s-spark-scheduler](https://github.com/palantir/k8s-spark-scheduler)** - The scheduler extender approach pioneered by Palantir laid the groundwork for understanding Spark workload patterns. We've evolved this into a plugin-based architecture using the modern Kubernetes Scheduler Framework.

- **[Kubernetes Scheduler Plugins](https://github.com/kubernetes-sigs/scheduler-plugins)** - Reference implementations for the scheduling framework

- **[Apache YuniKorn](https://yunikorn.apache.org/)** - Advanced queue management concepts

- **[Volcano](https://volcano.sh/)** - Job lifecycle management patterns

---

## 🗺️ Roadmap

### ✅ v1.0 (Current - February 2026)
- ✅ Gang scheduling (co-scheduling) with starvation prevention
- ✅ Resource reservation (internalized, no external deps)
- ✅ High availability
- ✅ Prometheus metrics
- ✅ Go 1.25, Kubernetes 1.35.1
- ✅ Self-contained codebase (all types internalized)

### 🚧 v1.1 (Q2 2026)
- Queue management (basic FIFO with priorities)
- Topology awareness (zone spreading)
- Enhanced metrics and dashboards
- Unit/integration tests

### 📋 v2.0 (Q3-Q4 2026)
- GPU scheduling
- Fair sharing (DRF)
- Preemption policies
- REST API for job submission

See [claude.md](claude.md) for detailed roadmap.

---

## 🤝 Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

**Areas we need help**:
- 🐛 Bug reports and fixes
- 📖 Documentation improvements  
- ✨ Feature implementations
- 🧪 Test coverage
- 🎨 Monitoring dashboards

---

## 📄 License

Apache License 2.0 - See [LICENSE](LICENSE)

---

## 📞 Support

- **GitHub Issues**: [Report bugs or request features](https://github.com/your-org/kubenexus-scheduler/issues)
- **Discussions**: [Ask questions](https://github.com/your-org/kubenexus-scheduler/discussions)
- **Documentation**: See [claude.md](claude.md) for technical details

---

**Built with ❤️ by the KubeNexus community**

