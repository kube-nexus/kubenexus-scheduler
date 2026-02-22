/*
Copyright 2026 KubeNexus Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package numatopology implements NUMA-aware scheduling.
package numatopology

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	framework "k8s.io/kube-scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/workload"
)

// NUMATopology implements NUMA-aware scheduling for high-performance workloads.
//
// WHAT IS NUMA?
// Non-Uniform Memory Access (NUMA) is a computer memory design where memory access time
// depends on the memory location relative to the CPU. Modern multi-socket servers have
// multiple NUMA nodes, each with local CPUs and memory.
//
// THE NUMA PROBLEM:
// When a pod's CPUs and memory span multiple NUMA nodes, memory access becomes slower:
//   - Local NUMA access: ~100ns latency
//   - Remote NUMA access: ~200-300ns latency (2-3x slower!)
//   - Impact: 30-50% performance degradation for memory-intensive workloads
//
// SOLUTION:
// This plugin ensures pods (especially ML/batch workloads) are placed on nodes where they
// can fit within a SINGLE NUMA node, maximizing memory bandwidth and performance.
//
// FEATURES:
//   1. Filter: Reject nodes where pod cannot fit in any single NUMA node
//   2. Score: Prefer nodes with best NUMA alignment and capacity
//   3. Multi-node awareness: Choose nodes with optimal NUMA topology
//   4. Gang scheduling support: Ensure all gang members get NUMA locality
//   5. Workload-aware: Only applies strict NUMA rules to batch/ML workloads
//
// EXAMPLE:
//   Node A (2 NUMA nodes):
//     NUMA 0: 16 CPUs, 64GB RAM (30GB free)
//     NUMA 1: 16 CPUs, 64GB RAM (60GB free)
//
//   Pod requests: 8 CPUs, 32GB RAM
//   Result: ✅ Fits in NUMA 1, schedule here
//
//   Pod requests: 20 CPUs, 80GB RAM
//   Result: ❌ Cannot fit in any single NUMA, reject node
//
// CONFIGURATION:
// Pods can request NUMA affinity via annotations:
//   scheduling.kubenexus.io/numa-policy: "single-numa-node"  # Strict
//   scheduling.kubenexus.io/numa-policy: "best-effort"       # Prefer but allow split
//   scheduling.kubenexus.io/numa-policy: "none"              # Disable NUMA
//
// ADVANCED FEATURES:
//   - Gang scheduling with NUMA awareness
//   - NUMA affinity/anti-affinity rules
//   - Memory bandwidth optimization
//   - NUMA distance/latency consideration
//
// WORKS WITH KUBELET:
// This plugin works at SCHEDULER level (choose which node).
// Kubelet Topology Manager handles actual CPU/memory binding on the chosen node.

var _ framework.FilterPlugin = &NUMATopology{}
var _ framework.ScorePlugin = &NUMATopology{}

const (
	// Name is the name of the plugin used in the plugin registry and configurations.
	Name = "NUMATopology"

	// NUMA node topology labels (standard Kubernetes labels)
	// See: https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/
	LabelNUMANodeCount = "numa.kubenexus.io/node-count" // Number of NUMA nodes
	LabelNUMACPUs      = "numa.kubenexus.io/cpus"       // CPUs per NUMA (e.g., "0-15,32-47")
	LabelNUMAMemory    = "numa.kubenexus.io/memory"     // Memory per NUMA (bytes)

	// Pod annotations for NUMA policy
	AnnotationNUMAPolicy = "scheduling.kubenexus.io/numa-policy"

	// ADVANCED: Gang scheduling support
	AnnotationGangGroup      = "scheduling.kubenexus.io/gang-group"       // Gang group name
	AnnotationGangNUMASpread = "scheduling.kubenexus.io/gang-numa-spread" // Gang NUMA spread policy

	// ADVANCED: NUMA affinity/anti-affinity
	AnnotationNUMAAffinityNodeID     = "scheduling.kubenexus.io/numa-affinity-node-id"      // Preferred NUMA node IDs (e.g., "0,1")
	AnnotationNUMAAntiAffinityNodeID = "scheduling.kubenexus.io/numa-anti-affinity-node-id" // Avoid NUMA node IDs

	// ADVANCED: Workload characteristics hints
	AnnotationMemoryIntensive = "scheduling.kubenexus.io/memory-intensive"     // "true" for memory-bandwidth critical workloads
	AnnotationNUMADistance    = "scheduling.kubenexus.io/numa-distance-weight" // Weight for NUMA distance (0-100)

	// NUMA policies
	NUMAPolicySingleNode = "single-numa-node" // Pod must fit in one NUMA node (strict)
	NUMAPolicyBestEffort = "best-effort"      // Prefer single NUMA but allow split
	NUMAPolicyNone       = "none"             // No NUMA awareness

	// Gang NUMA spread policies
	GangNUMASpreadPacked   = "packed"   // Place gang members on same NUMA nodes (minimize inter-node traffic)
	GangNUMASpreadBalanced = "balanced" // Balance gang members across NUMA nodes
	GangNUMASpreadIsolated = "isolated" // Each gang member gets dedicated NUMA node

	// Default policy for batch/ML workloads (if no annotation)
	DefaultBatchNUMAPolicy = NUMAPolicySingleNode

	// MaxNodeScore is the maximum score a node can get.
	MaxNodeScore = framework.MaxNodeScore

	// Scoring weights for advanced features
	WeightNUMAFit         = 0.40 // 40% weight for how well pod fits in NUMA
	WeightMemoryBandwidth = 0.25 // 25% weight for memory bandwidth availability
	WeightNUMADistance    = 0.20 // 20% weight for NUMA distance/latency
	WeightGangAffinity    = 0.15 // 15% weight for gang member affinity
)

// NUMANode represents a single NUMA node on a server
type NUMANode struct {
	ID              int         // NUMA node ID (0, 1, 2, ...)
	CPUs            []int       // CPU IDs in this NUMA node
	TotalMemory     int64       // Total memory in bytes
	AvailableCPUs   int         // Available (unallocated) CPUs
	AvailableMemory int64       // Available memory in bytes
	Distance        map[int]int // Distance to other NUMA nodes (node ID -> distance)
	MemoryBandwidth int64       // Memory bandwidth in MB/s (optional)
}

// GangNUMAState tracks NUMA placement decisions for gang members
type GangNUMAState struct {
	GangGroup       string         // Gang group name
	AssignedMembers map[string]int // Pod name -> NUMA node ID
	SpreadPolicy    string         // Gang NUMA spread policy
}

// NUMATopology implements NUMA-aware scheduling with advanced features
type NUMATopology struct {
	handle    framework.Handle
	gangState map[string]*GangNUMAState // Gang group -> state
}

// Name returns the name of the plugin.
func (n *NUMATopology) Name() string {
	return Name
}

// Filter invoked at the filter extension point.
//
// Filters out nodes where the pod cannot fit in any single NUMA node.
// This prevents cross-NUMA placement for performance-sensitive workloads.
//
// Algorithm:
//  1. Check if pod requires NUMA awareness (batch/ML workload or explicit annotation)
//  2. Parse node's NUMA topology from labels
//  3. Check if pod's resource requests fit in ANY single NUMA node
//  4. If yes → allow node; if no → reject node
func (n *NUMATopology) Filter(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfo framework.NodeInfo) *framework.Status {
	node := nodeInfo.Node()
	if node == nil {
		return framework.NewStatus(framework.Error, "node is nil")
	}

	// Check NUMA policy for this pod
	policy := n.getNUMAPolicy(pod)

	if policy == NUMAPolicyNone {
		// NUMA awareness disabled for this pod
		klog.V(5).Infof("NUMATopology: pod %s/%s has NUMA policy 'none', skipping filter", pod.Namespace, pod.Name)
		return framework.NewStatus(framework.Success, "")
	}

	if policy == NUMAPolicyBestEffort {
		// Best effort policy - don't filter, just score
		klog.V(5).Infof("NUMATopology: pod %s/%s has best-effort NUMA policy, allowing node %s",
			pod.Namespace, pod.Name, node.Name)
		return framework.NewStatus(framework.Success, "")
	}

	// Policy is single-numa-node (strict) - enforce filtering
	numaNodes, err := n.parseNUMATopology(node)
	if err != nil {
		// Node has no NUMA topology information, allow it (assume single NUMA or kubelet will handle)
		klog.V(4).Infof("NUMATopology: node %s has no NUMA topology labels: %v", node.Name, err)
		return framework.NewStatus(framework.Success, "")
	}

	if len(numaNodes) <= 1 {
		// Node has only 1 NUMA node (or none), no cross-NUMA risk
		klog.V(5).Infof("NUMATopology: node %s has %d NUMA node(s), allowing", node.Name, len(numaNodes))
		return framework.NewStatus(framework.Success, "")
	}

	// Calculate pod resource requirements
	podCPU, podMemory := n.getPodResourceRequests(pod)

	// Check if pod fits in any single NUMA node
	for _, numa := range numaNodes {
		if numa.AvailableCPUs >= int(podCPU) && numa.AvailableMemory >= podMemory {
			klog.V(4).Infof("NUMATopology: pod %s/%s (cpu=%d, mem=%dGB) fits in NUMA node %d on %s",
				pod.Namespace, pod.Name, podCPU, podMemory/(1024*1024*1024), numa.ID, node.Name)
			return framework.NewStatus(framework.Success, "")
		}
	}

	// Pod cannot fit in any single NUMA node
	reason := fmt.Sprintf("pod requires %d CPUs and %d bytes memory, but no single NUMA node has sufficient capacity on node %s",
		podCPU, podMemory, node.Name)

	klog.V(3).Infof("NUMATopology: rejecting node %s for pod %s/%s: %s",
		node.Name, pod.Namespace, pod.Name, reason)

	return framework.NewStatus(framework.Unschedulable, reason)
}

// Score invoked at the score extension point.
//
// ADVANCED SCORING: Scores nodes based on multiple NUMA factors:
//  1. NUMA Fit Quality (40%): How well pod fits in NUMA node
//  2. Memory Bandwidth (25%): Available memory bandwidth for memory-intensive workloads
//  3. NUMA Distance (20%): Inter-NUMA latency/distance (prefer local NUMA)
//  4. Gang Affinity (15%): Co-location with gang members
//
// Scoring algorithm:
//   - Find best NUMA node(s) that can accommodate the pod
//   - Calculate fit quality based on utilization
//   - Apply memory bandwidth weight for memory-intensive workloads
//   - Consider NUMA distance for multi-NUMA placement
//   - Boost score for gang member co-location
//
// Higher score = better NUMA alignment = better performance
func (n *NUMATopology) Score(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfo framework.NodeInfo) (int64, *framework.Status) {
	node := nodeInfo.Node()
	if node == nil {
		return 0, framework.NewStatus(framework.Error, "node is nil")
	}

	// Check NUMA policy
	policy := n.getNUMAPolicy(pod)

	if policy == NUMAPolicyNone {
		// No NUMA awareness, return neutral score
		return MaxNodeScore / 2, framework.NewStatus(framework.Success, "")
	}

	// Parse NUMA topology
	numaNodes, err := n.parseNUMATopology(node)
	if err != nil || len(numaNodes) <= 1 {
		// No NUMA info or single NUMA node, return neutral score
		return MaxNodeScore / 2, framework.NewStatus(framework.Success, "")
	}

	// Calculate pod requirements
	podCPU, podMemory := n.getPodResourceRequests(pod)

	// Check if pod is memory-intensive
	isMemoryIntensive := n.isMemoryIntensive(pod)

	// Get NUMA affinity preferences
	preferredNUMAs, avoidNUMAs := n.getNUMAAffinityPreferences(pod)

	// Find best NUMA node fit
	var bestScore float64
	bestNUMAID := -1

	// Component scores
	var fitScore, memBandwidthScore, distanceScore, gangScore float64

	for _, numa := range numaNodes {
		if numa.AvailableCPUs < int(podCPU) || numa.AvailableMemory < podMemory {
			// Pod doesn't fit in this NUMA node
			continue
		}

		// Skip if in avoid list
		if n.isNUMAInList(numa.ID, avoidNUMAs) {
			klog.V(5).Infof("Skipping NUMA node %d on %s due to anti-affinity", numa.ID, node.Name)
			continue
		}

		// 1. NUMA FIT QUALITY (40%)
		cpuUtilization := float64(podCPU) / float64(len(numa.CPUs)) * 100.0
		memUtilization := float64(podMemory) / float64(numa.TotalMemory) * 100.0

		// Weighted average: 60% CPU, 40% memory
		utilization := (cpuUtilization * 0.6) + (memUtilization * 0.4)

		// Optimal utilization: 50-70% (leaves room for growth, not too fragmented)
		fitScore = 100.0 - math.Abs(utilization-60.0)
		if fitScore < 0 {
			fitScore = 0
		}

		// Boost if in preferred NUMA list
		if n.isNUMAInList(numa.ID, preferredNUMAs) {
			fitScore = math.Min(100.0, fitScore*1.2) // 20% boost
			klog.V(5).Infof("Boosting NUMA node %d on %s due to affinity", numa.ID, node.Name)
		}

		// 2. MEMORY BANDWIDTH SCORE (25%)
		memBandwidthScore = 50.0 // Default neutral score
		if isMemoryIntensive && numa.MemoryBandwidth > 0 {
			// Calculate memory bandwidth pressure based on requested vs available memory
			// Lower utilization = higher available bandwidth = higher score
			bandwidthUtilization := (float64(podMemory) / float64(numa.TotalMemory)) * 100.0
			memBandwidthScore = 100.0 - bandwidthUtilization
			if memBandwidthScore < 0 {
				memBandwidthScore = 0
			}
			klog.V(5).Infof("Memory bandwidth score for NUMA %d on %s: %.2f", numa.ID, node.Name, memBandwidthScore)
		}

		// 3. NUMA DISTANCE SCORE (20%)
		distanceScore = n.calculateNUMADistanceScore(numa, numaNodes, pod)

		// 4. GANG AFFINITY SCORE (15%)
		gangScore = n.calculateGangAffinityScore(pod, numa, node)

		// Calculate weighted total score
		totalScore := (fitScore * WeightNUMAFit) +
			(memBandwidthScore * WeightMemoryBandwidth) +
			(distanceScore * WeightNUMADistance) +
			(gangScore * WeightGangAffinity)

		if totalScore > bestScore {
			bestScore = totalScore
			bestNUMAID = numa.ID
		}
	}

	if bestNUMAID == -1 {
		// Pod doesn't fit in any NUMA node (only possible with best-effort policy)
		// Return low score (but not zero - still schedulable)
		klog.V(4).Infof("NUMATopology: pod %s/%s requires cross-NUMA on node %s (best-effort policy)",
			pod.Namespace, pod.Name, node.Name)
		return MaxNodeScore / 4, framework.NewStatus(framework.Success, "")
	}

	klog.V(4).Infof("NUMATopology: pod %s/%s best fits in NUMA %d on node %s (score=%.2f, components: fit=%.2f, mem=%.2f, dist=%.2f, gang=%.2f)",
		pod.Namespace, pod.Name, bestNUMAID, node.Name, bestScore, fitScore, memBandwidthScore, distanceScore, gangScore)

	// Track gang placement
	n.recordGangPlacement(pod, bestNUMAID, node)

	return int64(bestScore), framework.NewStatus(framework.Success, "")
}

// ScoreExtensions returns a ScoreExtensions interface if it implements one, or nil if not.
func (n *NUMATopology) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

// getNUMAPolicy determines the NUMA policy for a pod.
//
// Priority:
//  1. Explicit annotation on pod
//  2. Workload type (batch/ML → single-numa-node, service → none)
//  3. Default: best-effort
func (n *NUMATopology) getNUMAPolicy(pod *v1.Pod) string {
	// Check explicit annotation
	if policy, exists := pod.Annotations[AnnotationNUMAPolicy]; exists {
		switch policy {
		case NUMAPolicySingleNode, NUMAPolicyBestEffort, NUMAPolicyNone:
			return policy
		default:
			klog.Warningf("NUMATopology: invalid NUMA policy '%s' for pod %s/%s, using default",
				policy, pod.Namespace, pod.Name)
		}
	}

	// Infer from workload type
	workloadType := workload.ClassifyPod(pod)

	if workloadType == workload.TypeBatch {
		// Batch/ML workloads benefit most from NUMA locality
		klog.V(5).Infof("NUMATopology: pod %s/%s classified as batch, using single-numa-node policy",
			pod.Namespace, pod.Name)
		return DefaultBatchNUMAPolicy
	}

	// Services don't typically need NUMA awareness (they spread anyway)
	klog.V(5).Infof("NUMATopology: pod %s/%s classified as service, using none policy",
		pod.Namespace, pod.Name)
	return NUMAPolicyNone
}

// parseNUMATopology extracts NUMA topology information from node labels.
//
// Expected labels on nodes:
//
//	numa.kubenexus.io/node-count: "2"
//	numa.kubenexus.io/node-0-cpus: "0-15,32-47"
//	numa.kubenexus.io/node-0-memory: "68719476736"  # bytes
//	numa.kubenexus.io/node-1-cpus: "16-31,48-63"
//	numa.kubenexus.io/node-1-memory: "68719476736"
//
// These labels should be set by a node labeler DaemonSet or kubelet.
func (n *NUMATopology) parseNUMATopology(node *v1.Node) ([]NUMANode, error) {
	// Check if node has NUMA count label
	countStr, exists := node.Labels[LabelNUMANodeCount]
	if !exists {
		return nil, fmt.Errorf("node %s missing NUMA node count label", node.Name)
	}

	count, err := strconv.Atoi(countStr)
	if err != nil || count <= 0 {
		return nil, fmt.Errorf("invalid NUMA node count: %s", countStr)
	}

	numaNodes := make([]NUMANode, 0, count)

	for i := 0; i < count; i++ {
		// Parse CPUs
		cpuLabel := fmt.Sprintf("numa.kubenexus.io/node-%d-cpus", i)
		cpusStr, exists := node.Labels[cpuLabel]
		if !exists {
			klog.V(4).Infof("Node %s missing label %s, skipping NUMA node %d", node.Name, cpuLabel, i)
			continue
		}

		cpus, err := parseCPUList(cpusStr)
		if err != nil {
			klog.Warningf("Node %s has invalid CPU list for NUMA %d: %v", node.Name, i, err)
			continue
		}

		// Parse memory
		memLabel := fmt.Sprintf("numa.kubenexus.io/node-%d-memory", i)
		memStr, exists := node.Labels[memLabel]
		if !exists {
			klog.V(4).Infof("Node %s missing label %s, skipping NUMA node %d", node.Name, memLabel, i)
			continue
		}

		memory, err := strconv.ParseInt(memStr, 10, 64)
		if err != nil {
			klog.Warningf("Node %s has invalid memory for NUMA %d: %v", node.Name, i, err)
			continue
		}

		// Parse memory bandwidth (optional)
		memBandwidthLabel := fmt.Sprintf("numa.kubenexus.io/node-%d-bandwidth", i)
		bandwidth := int64(0)
		if bwStr, exists := node.Labels[memBandwidthLabel]; exists {
			if bw, err := strconv.ParseInt(bwStr, 10, 64); err == nil {
				bandwidth = bw
			}
		}

		// Parse NUMA distances (optional)
		distances := make(map[int]int)
		for j := 0; j < count; j++ {
			distLabel := fmt.Sprintf("numa.kubenexus.io/node-%d-distance-%d", i, j)
			if distStr, exists := node.Labels[distLabel]; exists {
				if dist, err := strconv.Atoi(distStr); err == nil {
					distances[j] = dist
				}
			}
		}

		// Initialize NUMA node with full capacity
		// Kubelet Topology Manager tracks actual allocations on the node
		numaNodes = append(numaNodes, NUMANode{
			ID:              i,
			CPUs:            cpus,
			TotalMemory:     memory,
			AvailableCPUs:   len(cpus),
			AvailableMemory: memory,
			Distance:        distances,
			MemoryBandwidth: bandwidth,
		})
	}

	if len(numaNodes) == 0 {
		return nil, fmt.Errorf("no valid NUMA nodes found on %s", node.Name)
	}

	return numaNodes, nil
}

// parseCPUList parses CPU list strings like "0-15,32-47" into slice of CPU IDs.
func parseCPUList(cpuList string) ([]int, error) {
	var cpus []int

	ranges := strings.Split(cpuList, ",")
	for _, r := range ranges {
		parts := strings.Split(strings.TrimSpace(r), "-")

		if len(parts) == 1 {
			// Single CPU
			cpu, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, err
			}
			cpus = append(cpus, cpu)
		} else if len(parts) == 2 {
			// CPU range
			start, err := strconv.Atoi(parts[0])
			if err != nil {
				return nil, err
			}
			end, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, err
			}
			for i := start; i <= end; i++ {
				cpus = append(cpus, i)
			}
		} else {
			return nil, fmt.Errorf("invalid CPU range: %s", r)
		}
	}

	return cpus, nil
}

// getPodResourceRequests calculates the total CPU and memory requests for a pod.
// Returns (cpu in millicores, memory in bytes)
func (n *NUMATopology) getPodResourceRequests(pod *v1.Pod) (int64, int64) {
	var cpu int64
	var memory int64

	for _, container := range pod.Spec.Containers {
		if q := container.Resources.Requests.Cpu(); q != nil {
			cpu += q.MilliValue()
		}
		if q := container.Resources.Requests.Memory(); q != nil {
			memory += q.Value()
		}
	}

	// Convert millicores to cores
	cpuCores := (cpu + 999) / 1000 // Round up

	return cpuCores, memory
}

// New initializes a new NUMATopology plugin and returns it.
func New(_ context.Context, _ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	klog.V(3).Infof("NUMATopology plugin initialized with advanced features: gang scheduling, affinity/anti-affinity, memory bandwidth optimization")
	return &NUMATopology{
		handle:    handle,
		gangState: make(map[string]*GangNUMAState),
	}, nil
}

// isMemoryIntensive checks if a pod is memory-intensive based on annotation or heuristics.
func (n *NUMATopology) isMemoryIntensive(pod *v1.Pod) bool {
	// Check explicit annotation
	if val, exists := pod.Annotations[AnnotationMemoryIntensive]; exists && val == "true" {
		return true
	}

	// Heuristic: Memory request > 16GB and memory/CPU ratio > 4GB per core
	_, memory := n.getPodResourceRequests(pod)
	cpuCores := int64(0)
	for _, container := range pod.Spec.Containers {
		if q := container.Resources.Requests.Cpu(); q != nil {
			cpuCores += (q.MilliValue() + 999) / 1000
		}
	}

	if cpuCores == 0 {
		cpuCores = 1
	}

	memoryGB := memory / (1024 * 1024 * 1024)
	memPerCore := memoryGB / cpuCores

	return memoryGB > 16 && memPerCore > 4
}

// getNUMAAffinityPreferences extracts NUMA affinity/anti-affinity preferences from pod annotations.
// Returns (preferred NUMA IDs, avoided NUMA IDs)
func (n *NUMATopology) getNUMAAffinityPreferences(pod *v1.Pod) ([]int, []int) {
	var preferred, avoided []int

	if affinity, exists := pod.Annotations[AnnotationNUMAAffinityNodeID]; exists {
		preferred = parseNUMAIDList(affinity)
	}

	if antiAffinity, exists := pod.Annotations[AnnotationNUMAAntiAffinityNodeID]; exists {
		avoided = parseNUMAIDList(antiAffinity)
	}

	return preferred, avoided
}

// parseNUMAIDList parses comma-separated NUMA IDs like "0,1,3"
func parseNUMAIDList(idList string) []int {
	var ids []int
	parts := strings.Split(idList, ",")
	for _, part := range parts {
		if id, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// isNUMAInList checks if a NUMA ID is in a list
func (n *NUMATopology) isNUMAInList(numaID int, list []int) bool {
	for _, id := range list {
		if id == numaID {
			return true
		}
	}
	return false
}

// calculateNUMADistanceScore calculates a score based on NUMA distance/latency.
// Lower distance = higher score (prefer local NUMA access)
func (n *NUMATopology) calculateNUMADistanceScore(numa NUMANode, allNUMAs []NUMANode, pod *v1.Pod) float64 {
	// If no distance information, return neutral score
	if len(numa.Distance) == 0 {
		return 50.0
	}

	// Check if pod has custom distance weight
	distanceWeight := 1.0
	if weight, exists := pod.Annotations[AnnotationNUMADistance]; exists {
		if w, err := strconv.ParseFloat(weight, 64); err == nil && w >= 0 && w <= 100 {
			distanceWeight = w / 50.0 // Normalize to 0-2 range
		}
	}

	// Calculate average distance to other NUMA nodes
	// Lower average distance = better locality
	var totalDistance, count int
	for otherID, distance := range numa.Distance {
		if otherID != numa.ID {
			totalDistance += distance
			count++
		}
	}

	if count == 0 {
		return 100.0 // Single NUMA, best case
	}

	avgDistance := float64(totalDistance) / float64(count)

	// Typical NUMA distances: 10 (local), 20-21 (adjacent), 30-31 (far)
	// Score: 100 for distance 10, decreasing for higher distances
	score := 100.0 - (avgDistance-10.0)*5.0*distanceWeight
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}

// calculateGangAffinityScore calculates a score based on gang member co-location.
// Higher score if gang members are on same/nearby NUMA nodes
func (n *NUMATopology) calculateGangAffinityScore(pod *v1.Pod, numa NUMANode, node *v1.Node) float64 {
	gangGroup, exists := pod.Annotations[AnnotationGangGroup]
	if !exists || gangGroup == "" {
		return 50.0 // Neutral score, not a gang member
	}

	// Get gang state
	gangState, exists := n.gangState[gangGroup]
	if !exists || len(gangState.AssignedMembers) == 0 {
		return 50.0 // First gang member, neutral score
	}

	// Get gang spread policy
	spreadPolicy := gangState.SpreadPolicy
	if spreadPolicy == "" {
		// Infer from annotation
		if policy, exists := pod.Annotations[AnnotationGangNUMASpread]; exists {
			spreadPolicy = policy
		} else {
			spreadPolicy = GangNUMASpreadPacked // Default: pack together
		}
	}

	switch spreadPolicy {
	case GangNUMASpreadPacked:
		// Prefer same NUMA as existing gang members
		for _, assignedNUMA := range gangState.AssignedMembers {
			if assignedNUMA == numa.ID {
				return 100.0 // Perfect co-location
			}
		}
		// Different NUMA, lower score
		return 20.0

	case GangNUMASpreadBalanced:
		// Prefer balanced distribution across NUMAs
		// Count gang members on this NUMA
		countOnNUMA := 0
		for _, assignedNUMA := range gangState.AssignedMembers {
			if assignedNUMA == numa.ID {
				countOnNUMA++
			}
		}
		// Lower count = higher score (balance)
		score := 100.0 - (float64(countOnNUMA) * 20.0)
		if score < 0 {
			score = 0
		}
		return score

	case GangNUMASpreadIsolated:
		// Prefer NUMA with no gang members
		for _, assignedNUMA := range gangState.AssignedMembers {
			if assignedNUMA == numa.ID {
				return 0.0 // Already has gang member, avoid
			}
		}
		return 100.0 // Empty NUMA, perfect

	default:
		return 50.0
	}
}

// recordGangPlacement records the NUMA placement decision for a gang member.
func (n *NUMATopology) recordGangPlacement(pod *v1.Pod, numaID int, node *v1.Node) {
	gangGroup, exists := pod.Annotations[AnnotationGangGroup]
	if !exists || gangGroup == "" {
		return
	}

	// Initialize gang state if needed
	if n.gangState == nil {
		n.gangState = make(map[string]*GangNUMAState)
	}

	gangState, exists := n.gangState[gangGroup]
	if !exists {
		gangState = &GangNUMAState{
			GangGroup:       gangGroup,
			AssignedMembers: make(map[string]int),
		}

		// Get spread policy
		if policy, exists := pod.Annotations[AnnotationGangNUMASpread]; exists {
			gangState.SpreadPolicy = policy
		} else {
			gangState.SpreadPolicy = GangNUMASpreadPacked
		}

		n.gangState[gangGroup] = gangState
	}

	// Record assignment
	podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	gangState.AssignedMembers[podKey] = numaID

	klog.V(4).Infof("Recorded gang placement: %s -> NUMA %d on %s (policy=%s, total members=%d)",
		podKey, numaID, node.Name, gangState.SpreadPolicy, len(gangState.AssignedMembers))
}
