# KubeNexus Scheduler Documentation

> Complete documentation for the KubeNexus production-grade Kubernetes scheduler

---

## 📚 Documentation Overview

### Getting Started
- [README.md](../README.md) - Quick start, installation, and basic usage
- [CONTRIBUTING.md](../CONTRIBUTING.md) - Contributing guidelines

### Core Features

#### 1. Gang Scheduling (Coscheduling)
**Description:** All-or-nothing scheduling for distributed workloads

**Documents:**
- [ACTUAL_IMPLEMENTATION_STATUS.md](ACTUAL_IMPLEMENTATION_STATUS.md) - Current implementation status
- [architecture.md](architecture.md) - System architecture and design
- [SCHEDULER_COMPARISON.md](SCHEDULER_COMPARISON.md) - Comparison with other schedulers (YuniKorn, Volcano)
- [COMPARISON_AND_ROADMAP.md](COMPARISON_AND_ROADMAP.md) - Feature comparison and roadmap

**Use Cases:** Spark, distributed ML, MPI jobs, multi-container applications

---

#### 2. NUMA-Aware Scheduling ⭐ NEW
**Description:** Advanced NUMA topology-aware pod placement for high-performance workloads

**Main Document:**
- **[NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md)** - **START HERE**
  - Complete feature guide (all 5 advanced features)
  - Architecture and scoring algorithm
  - Node setup and labeling
  - 10+ real-world examples
  - Troubleshooting and best practices
  - Performance benchmarks
  - Comparison with other schedulers

**Supporting Documents:**
- [NUMA_NODE_LABELING.md](NUMA_NODE_LABELING.md) - Detailed node labeling guide
- [examples/advanced-numa-examples.yaml](examples/advanced-numa-examples.yaml) - Production-ready pod specs

**Features:**
- ✅ Multi-node NUMA-aware scheduling
- ✅ NUMA affinity/anti-affinity
- ✅ Memory bandwidth optimization
- ✅ NUMA distance/latency awareness
- ✅ Gang scheduling with 3 NUMA policies (packed/balanced/isolated)

**Performance:** 30-57% improvement for NUMA-sensitive workloads  
**Use Cases:** ML training, HPC, in-memory databases, latency-sensitive apps

---

#### 3. Workload-Aware Scheduling
**Description:** Adaptive scheduling based on workload types (batch vs services)

**Documents:**
- [HYBRID_SCHEDULING.md](HYBRID_SCHEDULING.md) - Hybrid scheduling strategies
- [SCHEDULING_SCENARIOS.md](SCHEDULING_SCENARIOS.md) - Common scheduling scenarios

**Features:**
- Bin packing for batch/ML workloads
- Spreading for services
- Backfill scheduling for opportunistic pods
- Topology-aware spreading

---

## 📖 Quick Reference

### By Use Case

| Use Case | Primary Document | Key Features |
|----------|-----------------|--------------|
| **Spark Jobs** | [README.md](../README.md) | Gang scheduling, resource reservation |
| **ML Training (Single Node)** | [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md) | NUMA-aware, memory optimization |
| **Distributed ML (Multi-Node)** | [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md) | Gang + NUMA, packed policy |
| **HPC Simulation** | [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md) | NUMA isolated policy |
| **In-Memory Databases** | [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md) | NUMA + affinity, low latency |
| **Microservices** | [HYBRID_SCHEDULING.md](HYBRID_SCHEDULING.md) | Topology spreading |
| **Batch Processing** | [HYBRID_SCHEDULING.md](HYBRID_SCHEDULING.md) | Bin packing, backfill |

---

### By Feature

| Feature | Document | Section |
|---------|----------|---------|
| Gang Scheduling Basics | [README.md](../README.md) | Usage |
| Gang + NUMA | [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md) | Gang Scheduling with NUMA |
| NUMA Affinity | [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md) | NUMA Affinity/Anti-Affinity |
| Memory Bandwidth | [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md) | Memory-Intensive Optimization |
| Node Labeling | [NUMA_NODE_LABELING.md](NUMA_NODE_LABELING.md) | Full Guide |
| Scheduler Comparison | [SCHEDULER_COMPARISON.md](SCHEDULER_COMPARISON.md) | vs YuniKorn/Volcano |
| NUMA Comparison | [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md) | Comparison with Other Schedulers |
| Architecture | [architecture.md](architecture.md) | System Design |
| Troubleshooting | [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md) | Troubleshooting Section |

---

## 🎯 Recommended Reading Path

### For New Users
1. [README.md](../README.md) - Quick start and basic gang scheduling
2. [architecture.md](architecture.md) - Understand the system design
3. [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md) - Advanced features (if needed)

### For ML/AI Workloads
1. [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md) - Complete NUMA guide
2. [examples/advanced-numa-examples.yaml](examples/advanced-numa-examples.yaml) - Copy-paste examples
3. [NUMA_NODE_LABELING.md](NUMA_NODE_LABELING.md) - Setup your nodes

### For HPC Workloads
1. [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md) - Focus on "Isolated" policy
2. [NUMA_NODE_LABELING.md](NUMA_NODE_LABELING.md) - Node configuration
3. [SCHEDULER_COMPARISON.md](SCHEDULER_COMPARISON.md) - Compare with alternatives

### For Administrators
1. [README.md](../README.md) - Installation and deployment
2. [NUMA_NODE_LABELING.md](NUMA_NODE_LABELING.md) - Automated node labeling
3. [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md) - All features and best practices
4. [SCHEDULER_COMPARISON.md](SCHEDULER_COMPARISON.md) - Evaluation criteria

### For Developers
1. [architecture.md](architecture.md) - System architecture
2. [ACTUAL_IMPLEMENTATION_STATUS.md](ACTUAL_IMPLEMENTATION_STATUS.md) - Current state
3. [COMPARISON_AND_ROADMAP.md](COMPARISON_AND_ROADMAP.md) - Future plans
4. [CONTRIBUTING.md](../CONTRIBUTING.md) - Contribution guide

---

## 📝 Document Summary

### Core Documentation

| Document | Lines | Status | Description |
|----------|-------|--------|-------------|
| [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md) | 930+ | ✅ Complete | **Main NUMA guide** - All features, examples, troubleshooting |
| [README.md](../README.md) | 379 | ✅ Complete | Quick start, installation, basic usage |
| [SCHEDULER_COMPARISON.md](SCHEDULER_COMPARISON.md) | 400+ | ✅ Complete | vs YuniKorn, Volcano, default K8s |
| [architecture.md](architecture.md) | 230 | ✅ Complete | System design and plugin architecture |

### Specialized Guides

| Document | Lines | Status | Description |
|----------|-------|--------|-------------|
| [NUMA_NODE_LABELING.md](NUMA_NODE_LABELING.md) | 225 | ✅ Complete | Node labeling, DaemonSet automation |
| [HYBRID_SCHEDULING.md](HYBRID_SCHEDULING.md) | 600+ | ✅ Complete | Workload-aware scheduling strategies |
| [SCHEDULING_SCENARIOS.md](SCHEDULING_SCENARIOS.md) | 350+ | ✅ Complete | Common use case scenarios |
| [COMPARISON_AND_ROADMAP.md](COMPARISON_AND_ROADMAP.md) | 300+ | ✅ Complete | Feature comparison, future roadmap |

### Implementation Details

| Document | Lines | Status | Description |
|----------|-------|--------|-------------|
| [ACTUAL_IMPLEMENTATION_STATUS.md](ACTUAL_IMPLEMENTATION_STATUS.md) | 400+ | ✅ Complete | Current plugin implementation |
| [examples/advanced-numa-examples.yaml](examples/advanced-numa-examples.yaml) | 450+ | ✅ Complete | Production-ready NUMA examples |

---

## 🔍 Search by Topic

### Performance
- NUMA performance benefits: [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md#why-numa-matters)
- Benchmarks: [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md#comparison-with-other-schedulers)
- Optimization tips: [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md#best-practices)

### Configuration
- Pod annotations: [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md#pod-configuration)
- Node labels: [NUMA_NODE_LABELING.md](NUMA_NODE_LABELING.md)
- Scheduler config: [README.md](../README.md#configuration)

### Examples
- NUMA examples: [examples/advanced-numa-examples.yaml](examples/advanced-numa-examples.yaml)
- Gang scheduling: [README.md](../README.md#usage)
- Use case scenarios: [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md#use-cases--examples)

### Troubleshooting
- NUMA issues: [NUMA_SCHEDULING_GUIDE.md](NUMA_SCHEDULING_GUIDE.md#troubleshooting)
- Common problems: [README.md](../README.md#troubleshooting)

---

## 🚀 What's New

### February 2026 - NUMA Scheduling Release

**Major Features:**
- ✅ Advanced NUMA-aware scheduling with 5 unique features
- ✅ Multi-factor scoring algorithm (4 components)
- ✅ Gang scheduling with NUMA constraints (3 policies)
- ✅ Memory bandwidth optimization
- ✅ NUMA distance/latency awareness
- ✅ Comprehensive documentation (2,700+ lines)

**Performance:**
- 30-50% improvement for ML training
- 37-39% improvement for distributed ML
- 52-57% improvement for HPC simulations

**Documentation:**
- Consolidated NUMA docs into single comprehensive guide
- 10+ real-world production examples
- Complete troubleshooting section
- Automated node labeling guide

---

## 📧 Support

- **Issues:** [GitHub Issues](https://github.com/your-org/kubenexus-scheduler/issues)
- **Discussions:** [GitHub Discussions](https://github.com/your-org/kubenexus-scheduler/discussions)
- **Contributing:** [CONTRIBUTING.md](../CONTRIBUTING.md)

---

## 📄 License

Apache License 2.0 - See [LICENSE](../LICENSE) for details

---

**Last Updated:** February 16, 2026  
**Documentation Version:** 2.0  
**Scheduler Version:** Compatible with Kubernetes 1.28+
