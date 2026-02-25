# Changelog

All notable changes to KubeNexus Scheduler will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [0.1.0] - 2026-02-18

### Added
- **Gang Scheduling (Co-scheduling)** - Atomic pod group scheduling with label-based configuration
- **NUMA-Aware Scheduling** - 4 policies (`best-effort`, `restricted`, `single-numa`, `isolated`)
- **GPU Topology Awareness** - PCIe-aware placement for multi-GPU workloads
- **Queue Management** - Starvation prevention, FIFO fairness, and priority-aware scheduling
- **High Availability** - Leader election support for multi-replica deployments
- **Operator Integration** - Works with Spark Operator, Kubeflow Training Operator, Argo, Ray out-of-the-box
- **Comprehensive Documentation** - User guide, operator integration guides, NUMA guide, scheduler comparison

### Technical Details
- Built on Kubernetes Scheduler Framework v1.28+
- Coscheduling plugin with permit phase coordination
- NUMA topology scoring plugin
- Zero external dependencies (no databases, no CRDs required)
- Minimal footprint: ~50MB memory, <0.1 core CPU

### Performance
- Single pod latency: 5ms (p50), 15ms (p99)
- Gang scheduling latency: 50ms (p50) for 8-pod groups
- Tested scale: 1,000 nodes, 10,000 pods, 100 concurrent gangs

### Supported Workloads
- Distributed ML training (PyTorch, TensorFlow, Horovod)
- Apache Spark jobs on Kubernetes
- HPC applications (molecular dynamics, CFD, FEA)
- Real-time AI inference
- Mixed stateless + batch + GPU workloads

---

## [Unreleased]

### Planned for v0.2.0 (Q2 2026)
- Enhanced Prometheus metrics and monitoring
- Admission webhook for validation and auto-injection
- Helm chart for easier deployment
- Namespace-based priority configuration
- Performance optimizations for large clusters

### Future (v1.0.0)
- Multi-queue support for >5000 node clusters
- Advanced fair-share policies
- Dynamic resource reservation
- Cluster autoscaler integration
- Optional PodGroup CRD for advanced features (backward compatible)

---

[0.1.0]: https://github.com/kube-nexus/kubenexus-scheduler/releases/tag/v0.1.0
[Unreleased]: https://github.com/kube-nexus/kubenexus-scheduler/compare/v0.1.0...HEAD
