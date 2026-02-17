# KubeNexus Documentation Map

```
┌───────────────────────────────────────────────────────────────────────────┐
│                         KubeNexus Scheduler                                │
│                     Production-Grade Gang + NUMA                           │
└───────────────────────────────────────────────────────────────────────────┘
                                     │
                                     ↓
┌───────────────────────────────────────────────────────────────────────────┐
│                          📚 START HERE                                     │
│                                                                            │
│  README.md (main repo)                                                    │
│  ├─ Quick start                                                            │
│  ├─ Installation                                                           │
│  ├─ Basic gang scheduling                                                  │
│  └─ Feature overview                                                       │
│                                                                            │
│  👉 For detailed docs: See docs/README.md                                 │
└───────────────────────────────────────────────────────────────────────────┘
                                     │
                                     ↓
┌───────────────────────────────────────────────────────────────────────────┐
│                    📖 Documentation Index                                  │
│                                                                            │
│  docs/README.md                                                           │
│  ├─ Table of contents                                                      │
│  ├─ Quick reference by use case                                           │
│  ├─ Recommended reading paths                                             │
│  └─ Search by topic                                                        │
│                                                                            │
│  👉 Navigate to specific features below                                   │
└───────────────────────────────────────────────────────────────────────────┘
                                     │
        ┌────────────────────────────┼────────────────────────────┐
        ↓                            ↓                            ↓
┌──────────────────┐       ┌──────────────────┐       ┌──────────────────┐
│  Gang Scheduling │       │ NUMA Scheduling  │       │ Workload-Aware   │
│   (Core Feature) │       │ (Advanced ⭐)     │       │   Scheduling     │
└──────────────────┘       └──────────────────┘       └──────────────────┘
        │                            │                            │
        ↓                            ↓                            ↓

┌─────────────────────────────────────────────────────────────────────────┐
│                        Gang Scheduling Docs                              │
│                                                                          │
│  📄 README.md (main)                                                    │
│     ├─ Basic usage & examples                                           │
│     ├─ Pod annotations                                                  │
│     └─ Configuration                                                    │
│                                                                          │
│  📄 architecture.md                                                     │
│     ├─ System design                                                    │
│     ├─ Plugin architecture                                              │
│     └─ Scheduling flow                                                  │
│                                                                          │
│  📄 SCHEDULER_COMPARISON.md                                             │
│     ├─ vs YuniKorn                                                      │
│     ├─ vs Volcano                                                       │
│     └─ vs Default K8s                                                   │
│                                                                          │
│  📄 ACTUAL_IMPLEMENTATION_STATUS.md                                     │
│     └─ Current plugin status                                            │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│                      NUMA Scheduling Docs ⭐                             │
│                                                                          │
│  📄 NUMA_SCHEDULING_GUIDE.md (22K) ⭐⭐⭐ MAIN GUIDE                    │
│     ├─ 1. Overview & Why NUMA Matters                                   │
│     ├─ 2. All 5 Advanced Features:                                      │
│     │   ├─ Multi-node NUMA awareness                                    │
│     │   ├─ NUMA affinity/anti-affinity                                  │
│     │   ├─ Memory bandwidth optimization                                │
│     │   ├─ NUMA distance/latency                                        │
│     │   └─ Gang + NUMA (3 policies)                                     │
│     ├─ 3. Architecture & Scoring                                        │
│     ├─ 4. Node Setup & Labeling                                         │
│     ├─ 5. Pod Configuration                                             │
│     ├─ 6. Use Cases & Examples (10+)                                    │
│     ├─ 7. Comparison with Others                                        │
│     ├─ 8. Troubleshooting                                               │
│     ├─ 9. Best Practices                                                │
│     └─ 10. Implementation Details                                       │
│                                                                          │
│  📋 NUMA_QUICK_REFERENCE.md (6K) ⚡ CHEAT SHEET                         │
│     ├─ All annotations                                                  │
│     ├─ Common use cases                                                 │
│     ├─ Verification commands                                            │
│     └─ Quick troubleshooting                                            │
│                                                                          │
│  🏷️ NUMA_NODE_LABELING.md (6K)                                         │
│     ├─ Manual labeling                                                  │
│     ├─ DaemonSet automation                                             │
│     └─ Label reference                                                  │
│                                                                          │
│  📦 examples/advanced-numa-examples.yaml                                │
│     ├─ ML training (single + distributed)                               │
│     ├─ HPC simulation                                                   │
│     ├─ In-memory databases                                              │
│     └─ Node labeling scripts                                            │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│                    Workload-Aware Scheduling Docs                        │
│                                                                          │
│  📄 HYBRID_SCHEDULING.md (25K)                                          │
│     ├─ Bin packing for batch                                            │
│     ├─ Spreading for services                                           │
│     ├─ Backfill scheduling                                              │
│     └─ Topology spreading                                               │
│                                                                          │
│  📄 SCHEDULING_SCENARIOS.md (12K)                                       │
│     └─ Common use case scenarios                                        │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│                         Planning & Roadmap                               │
│                                                                          │
│  📄 COMPARISON_AND_ROADMAP.md (12K)                                     │
│     ├─ Feature comparison                                               │
│     └─ Future roadmap                                                   │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 🎯 Quick Navigation by Role

### New User
```
README.md → docs/README.md → NUMA_SCHEDULING_GUIDE.md (Overview)
```

### ML/AI Engineer
```
docs/README.md → NUMA_SCHEDULING_GUIDE.md → advanced-numa-examples.yaml
                        ↓
                 NUMA_QUICK_REFERENCE.md (bookmark for daily use)
```

### HPC Administrator
```
docs/README.md → NUMA_NODE_LABELING.md → NUMA_SCHEDULING_GUIDE.md (Best Practices)
```

### Spark User
```
README.md → Gang Scheduling section → SCHEDULER_COMPARISON.md (optional)
```

### Developer/Contributor
```
docs/README.md → architecture.md → ACTUAL_IMPLEMENTATION_STATUS.md
```

---

## 📊 Document Size Reference

```
Large (20K+):     NUMA_SCHEDULING_GUIDE.md, SCHEDULER_COMPARISON.md, HYBRID_SCHEDULING.md
Medium (10-20K):  ACTUAL_IMPLEMENTATION_STATUS.md, COMPARISON_AND_ROADMAP.md, SCHEDULING_SCENARIOS.md
Small (5-10K):    README.md (docs), architecture.md, NUMA_NODE_LABELING.md, NUMA_QUICK_REFERENCE.md
```

---

## 🔍 Find by Topic

```
Topic: NUMA Performance
├─ Why it matters: NUMA_SCHEDULING_GUIDE.md → Why NUMA Matters
├─ Benchmarks: NUMA_SCHEDULING_GUIDE.md → Comparison section
└─ Optimization: NUMA_SCHEDULING_GUIDE.md → Best Practices

Topic: Pod Configuration
├─ Gang: README.md → Usage
├─ NUMA: NUMA_QUICK_REFERENCE.md → Pod Annotations
└─ Both: NUMA_SCHEDULING_GUIDE.md → Use Cases & Examples

Topic: Node Setup
├─ Manual: NUMA_NODE_LABELING.md → Manual Labeling
├─ Automated: NUMA_NODE_LABELING.md → DaemonSet
└─ Verification: NUMA_QUICK_REFERENCE.md → Verification Commands

Topic: Troubleshooting
├─ Quick fixes: NUMA_QUICK_REFERENCE.md → Troubleshooting
├─ Detailed: NUMA_SCHEDULING_GUIDE.md → Troubleshooting
└─ Common issues: README.md → Troubleshooting section

Topic: Comparison
├─ Overall: SCHEDULER_COMPARISON.md
├─ NUMA-specific: NUMA_SCHEDULING_GUIDE.md → Comparison section
└─ Feature matrix: COMPARISON_AND_ROADMAP.md
```

---

## 🏆 Documentation Highlights

### Most Important (Read First)
1. **README.md** (main repo) - Start here
2. **docs/README.md** - Documentation navigation
3. **NUMA_SCHEDULING_GUIDE.md** - Complete NUMA guide

### Most Useful (Bookmark)
1. **NUMA_QUICK_REFERENCE.md** - Daily use
2. **NUMA_NODE_LABELING.md** - Node setup
3. **advanced-numa-examples.yaml** - Copy-paste examples

### Most Comprehensive
1. **SCHEDULER_COMPARISON.md** (39K) - Detailed comparison
2. **HYBRID_SCHEDULING.md** (25K) - Workload strategies
3. **NUMA_SCHEDULING_GUIDE.md** (22K) - Complete NUMA

---

## 📱 Quick Access Links

| I want to... | Go to |
|--------------|-------|
| Get started | `README.md` (main) |
| Navigate docs | `docs/README.md` |
| Learn NUMA | `NUMA_SCHEDULING_GUIDE.md` |
| Get NUMA syntax | `NUMA_QUICK_REFERENCE.md` |
| Setup nodes | `NUMA_NODE_LABELING.md` |
| See examples | `examples/advanced-numa-examples.yaml` |
| Compare schedulers | `SCHEDULER_COMPARISON.md` |
| Understand design | `architecture.md` |
| Check status | `ACTUAL_IMPLEMENTATION_STATUS.md` |
| Plan future | `COMPARISON_AND_ROADMAP.md` |

---

**Last Updated:** February 16, 2026  
**Total Docs:** 11 markdown files  
**Total Size:** ~175K  
**Status:** ✅ Well-organized and consolidated
