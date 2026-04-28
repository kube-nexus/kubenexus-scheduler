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

// Package networkfabric implements network fabric topology-aware scheduling for gang workloads.
//
// THE PROBLEM:
// Distributed training and gang-scheduled workloads require high-bandwidth, low-latency
// communication between pods. When pods are scattered across different network domains
// (NVLink partitions, racks, availability zones), network performance degrades significantly:
//   - Same NVLink partition (clique): 1.8 TB/s chip-to-chip (GB200 NVL72)
//   - Same rack (NVSwitch): ~300-600 GB/s GPU-GPU bandwidth
//   - Cross-rack (InfiniBand/RoCE): ~100-200 GB/s bandwidth
//   - Cross-AZ (backbone): ~10-25 GB/s bandwidth
//   - Performance impact: 3-50x slowdown for cross-boundary collective operations
//
// THE SOLUTION:
// This plugin provides both hard filtering and soft scoring across all topology levels:
//   - Filter: For gang pods with require-clique or strict co-location, hard-reject nodes
//     in the wrong NVLink partition (cross-partition = 10-50x bandwidth drop)
//   - Score: Multi-level locality scoring that packs gang members into the tightest
//     topology domain: NVLink clique (+40) > fabric domain (+30) > rack (+20) > AZ (+10)
//
// TOPOLOGY HIERARCHY (tightest -> broadest):
//  1. NVLink Clique: nvidia.com/gpu.clique (NVIDIA DRA driver / ComputeDomain)
//  2. Fabric Domain: network.kubenexus.io/fabric-id (NVSwitch/IB/RoCE domain)
//  3. Rack: network.kubenexus.io/rack-id
//  4. Availability Zone: network.kubenexus.io/az
//
// NETWORK FABRIC TIERS (from best to worst):
//  1. NVSwitch Fabric: GPU-to-GPU direct, 900 GB/s, <1μs latency (DGX SuperPods)
//  2. NVLink Domain: GPU-to-GPU in single node, 600 GB/s, <2μs latency
//  3. InfiniBand EDR/HDR: High-speed RDMA, 100-200 GB/s, <10μs latency
//  4. RoCE v2 (100GbE): Ethernet-based RDMA, 12.5-25 GB/s, ~50μs latency
//  5. Standard Ethernet: 1-10 GbE, 125 MB - 1.25 GB/s, >100μs latency
//
// SCORING STRATEGY:
//   - Co-locate gang members in same NVLink clique and network domain when possible
//   - Prefer higher-tier fabrics for communication-intensive workloads
//   - Consider existing pod placement to avoid fragmenting network domains
//   - Balance with other constraints (GPU availability, NUMA, etc.)
//
// NODE LABELS (required for fabric detection):
//
//	network.kubenexus.io/fabric-type: "nvswitch|nvlink|infiniband|roce|ethernet"
//	network.kubenexus.io/fabric-id: "<unique-fabric-domain-id>"
//	network.kubenexus.io/rack-id: "<rack-identifier>"
//	network.kubenexus.io/az: "<availability-zone>"
//	nvidia.com/gpu.clique: "<NVLink-partition-id>"  (set by NVIDIA DRA driver)
//
// POD ANNOTATIONS (optional overrides):
//
//	scheduling.kubenexus.io/network-sensitive: "true|false"  # Boost scoring weight
//	scheduling.kubenexus.io/min-fabric-tier: "nvswitch|infiniband|roce"  # Minimum required
//	scheduling.kubenexus.io/co-locate: "strict|preferred|none"  # Gang locality requirement
//	scheduling.kubenexus.io/require-clique: "true"  # Hard: filter to same NVLink partition
//
// EXAMPLE TOPOLOGY:
//
//	Rack A (NVSwitch Fabric "nsw-fabric-01"):
//	  Node 1, Node 2, Node 3, Node 4 (8 GPUs each) - 900 GB/s interconnect
//	Rack B (InfiniBand "ib-fabric-02"):
//	  Node 5, Node 6, Node 7, Node 8 (8 GPUs each) - 200 GB/s interconnect
//
//	Gang of 4 pods × 8 GPUs = 32 GPUs total
//	✅ GOOD: All in Rack A (NVSwitch, same fabric domain)
//	⚠️ OK: All in Rack B (InfiniBand, same fabric domain)
//	❌ BAD: 2 in Rack A, 2 in Rack B (cross-rack, mixed fabrics)
//
// INTEGRATION WITH OTHER PLUGINS:
//   - Works with Coscheduling plugin for gang awareness
//   - Complements ResourceFragmentation for GPU placement
//   - Coordinates with NUMA topology for intra-node optimization
package networkfabric

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	corelisters "k8s.io/client-go/listers/core/v1"
	resourcev1listers "k8s.io/client-go/listers/resource/v1"
	klog "k8s.io/klog/v2"
	framework "k8s.io/kube-scheduler/framework"

	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/profileclassifier"
	"github.com/kube-nexus/kubenexus-scheduler/pkg/workload"
)

// NetworkFabricScore implements network topology-aware scoring and NVLink
// partition filtering for gang scheduling. It provides:
//   - Filter: Hard rejection of nodes in the wrong NVLink clique for gang pods
//   - Score: Multi-level locality scoring (clique > fabric-id > rack > AZ)
type NetworkFabricScore struct {
	handle              framework.Handle
	podLister           corelisters.PodLister
	nodeLister          corelisters.NodeLister
	resourceSliceLister resourcev1listers.ResourceSliceLister // DRA fallback for clique discovery
}

var _ framework.FilterPlugin = &NetworkFabricScore{}
var _ framework.ScorePlugin = &NetworkFabricScore{}

const (
	// Name is the name of the plugin used in the plugin registry and configurations.
	Name = "NetworkFabricScore"

	// Node labels for network fabric topology
	LabelFabricType = "network.kubenexus.io/fabric-type" // nvswitch|nvlink|infiniband|roce|ethernet
	LabelFabricID   = "network.kubenexus.io/fabric-id"   // Unique fabric domain identifier
	LabelRackID     = "network.kubenexus.io/rack-id"     // Rack identifier
	LabelAZ         = "network.kubenexus.io/az"          // Availability zone

	// NVIDIA DRA driver labels for NVLink partition (ComputeDomain) awareness.
	// In GB200 NVL72 racks, GPUs are split into NVLink partitions (cliques).
	// Gang pods crossing clique boundaries fall back to IB — a 10-50x bandwidth drop.
	LabelGPUClique = "nvidia.com/gpu.clique" // NVLink partition ID

	// Pod annotations
	AnnotationNetworkSensitive = "scheduling.kubenexus.io/network-sensitive" // true|false
	AnnotationMinFabricTier    = "scheduling.kubenexus.io/min-fabric-tier"   // nvswitch|infiniband|roce
	AnnotationCoLocate         = "scheduling.kubenexus.io/co-locate"         // strict|preferred|none
	AnnotationPodGroup         = "pod-group.scheduling.sigs.k8s.io/name"     // Gang group name
	AnnotationRequireClique    = "scheduling.kubenexus.io/require-clique"    // true = hard NVLink clique filter

	// Fabric tier scores (higher is better)
	ScoreNVSwitch   = 100 // DGX SuperPod with NVSwitch fabric
	ScoreNVLink     = 90  // Single-node NVLink domain
	ScoreInfiniBand = 75  // InfiniBand EDR/HDR
	ScoreRoCE       = 60  // RoCE v2 over 100GbE
	ScoreEthernet   = 40  // Standard Ethernet (fallback)
	ScoreUnknown    = 50  // No fabric info, neutral score

	// Locality bonuses (added to base fabric score)
	BonusSameClique       = 40 // All gang members in same NVLink partition (highest)
	BonusSameFabricDomain = 30 // All gang members in same fabric domain
	BonusSameRack         = 20 // All gang members in same rack
	BonusSameAZ           = 10 // All gang members in same AZ
	PenaltyCrossClique    = 40 // Gang members split across NVLink partitions
	PenaltyCrossFabric    = 30 // Gang members split across fabric domains
	PenaltyCrossRack      = 20 // Gang members split across racks
	PenaltyCrossAZ        = 10 // Gang members split across AZs

	// Network sensitivity multiplier
	WeightNetworkSensitive = 1.5 // Boost scoring for network-intensive workloads

	// Workload-specific fabric tier boosts (added when workload type matches fabric needs)
	BoostTrainingHighTier    = 15 // Training workloads on NVSwitch/InfiniBand (need high bandwidth)
	BoostInferenceLowTier    = 10 // Inference workloads can tolerate lower-tier fabrics
	PenaltyTrainingLowTier   = 20 // Training workloads on low-tier fabrics (performance penalty)
	PenaltyInferenceHighTier = 5  // Minor penalty for wasting high-tier fabric on inference
)

// FabricType represents different network fabric technologies.
type FabricType string

const (
	FabricNVSwitch   FabricType = "nvswitch"
	FabricNVLink     FabricType = "nvlink"
	FabricInfiniBand FabricType = "infiniband"
	FabricRoCE       FabricType = "roce"
	FabricEthernet   FabricType = "ethernet"
	FabricUnknown    FabricType = "unknown"
)

// Name returns the name of the plugin.
func (nf *NetworkFabricScore) Name() string {
	return Name
}

// Filter rejects nodes in the wrong NVLink clique for gang pods that require
// strict clique co-location. This prevents cross-partition placement that would
// force fallback to InfiniBand/Ethernet with 10-50x bandwidth degradation.
func (nf *NetworkFabricScore) Filter(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfo framework.NodeInfo) *framework.Status {
	node := nodeInfo.Node()
	if node == nil {
		return framework.NewStatus(framework.Error, "node not found")
	}

	// Only enforce clique filtering for gang pods with strict co-locate or require-clique
	podGroup := pod.Annotations[AnnotationPodGroup]
	if podGroup == "" {
		return framework.NewStatus(framework.Success)
	}

	if !nf.requiresCliqueFilter(pod) {
		return framework.NewStatus(framework.Success)
	}

	candidateClique := nf.getNodeClique(node)
	if candidateClique == "" {
		// Node is not in a NVLink environment — check if gang already has clique members
		existingClique := nf.getGangClique(pod.Namespace, podGroup)
		if existingClique != "" {
			return framework.NewStatus(framework.Unschedulable,
				fmt.Sprintf("node %s has no NVLink partition label, but gang %s requires clique %s",
					node.Name, podGroup, existingClique))
		}
		return framework.NewStatus(framework.Success)
	}

	// If gang members already scheduled, must match their clique
	existingClique := nf.getGangClique(pod.Namespace, podGroup)
	if existingClique == "" {
		return framework.NewStatus(framework.Success)
	}

	if candidateClique != existingClique {
		klog.V(3).InfoS("NetworkFabricScore: filtered node for clique mismatch",
			"pod", pod.Name, "node", node.Name,
			"nodeClique", candidateClique, "gangClique", existingClique)
		return framework.NewStatus(framework.Unschedulable,
			fmt.Sprintf("node %s is in NVLink partition %s, but gang %s requires partition %s",
				node.Name, candidateClique, podGroup, existingClique))
	}

	return framework.NewStatus(framework.Success)
}

// requiresCliqueFilter checks if the pod needs hard NVLink clique filtering.
func (nf *NetworkFabricScore) requiresCliqueFilter(pod *v1.Pod) bool {
	if pod.Annotations[AnnotationRequireClique] == "true" {
		return true
	}
	if strings.ToLower(pod.Annotations[AnnotationCoLocate]) == "strict" {
		return true
	}
	return false
}

// getGangClique returns the NVLink partition that already-scheduled gang members are placed in.
func (nf *NetworkFabricScore) getGangClique(namespace, podGroup string) string {
	gangPods := nf.getGangMemberPods(namespace, podGroup)
	for _, gangPod := range gangPods {
		if gangPod.Spec.NodeName == "" {
			continue
		}
		node, err := nf.nodeLister.Get(gangPod.Spec.NodeName)
		if err != nil {
			continue
		}
		if clique := nf.getNodeClique(node); clique != "" {
			return clique
		}
	}
	return ""
}

// getNodeClique returns the NVLink partition ID for a node using graceful degradation:
//  1. Node label nvidia.com/gpu.clique (set by NVIDIA DRA driver)
//  2. DRA ResourceSlice nvlink-domain attribute (K8s 1.34+)
func (nf *NetworkFabricScore) getNodeClique(node *v1.Node) string {
	// Priority 1: Node label (fastest, set by NVIDIA GPU operator / DRA driver)
	if clique := node.Labels[LabelGPUClique]; clique != "" {
		return clique
	}

	// Priority 2: DRA ResourceSlice attributes (fallback for environments without label)
	if nf.resourceSliceLister == nil {
		return ""
	}
	allSlices, err := nf.resourceSliceLister.List(labels.Everything())
	if err != nil {
		return ""
	}
	for _, slice := range allSlices {
		if slice.Spec.NodeName == nil || *slice.Spec.NodeName != node.Name {
			continue
		}
		if !isGPUDriver(slice.Spec.Driver) {
			continue
		}
		for _, device := range slice.Spec.Devices {
			if device.Attributes == nil {
				continue
			}
			if domainAttr, exists := device.Attributes["nvlink-domain"]; exists {
				if domainAttr.IntValue != nil {
					return strconv.Itoa(int(*domainAttr.IntValue))
				}
				if domainAttr.StringValue != nil && *domainAttr.StringValue != "" {
					return *domainAttr.StringValue
				}
			}
		}
	}
	return ""
}

// isGPUDriver checks if a DRA driver name indicates a GPU driver.
func isGPUDriver(driver string) bool {
	driver = strings.ToLower(driver)
	return strings.Contains(driver, "nvidia") ||
		strings.Contains(driver, "gpu") ||
		strings.Contains(driver, "amd")
}

// ScoreExtensions returns nil (no normalization needed).
func (nf *NetworkFabricScore) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

// Score evaluates a node based on network fabric topology for gang scheduling.
func (nf *NetworkFabricScore) Score(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfo framework.NodeInfo) (int64, *framework.Status) {
	node := nodeInfo.Node()
	if node == nil {
		return 0, framework.NewStatus(framework.Error, "node not found")
	}

	// Check if pod is part of a gang (pod group)
	podGroup := pod.Annotations[AnnotationPodGroup]
	isGangMember := podGroup != ""

	// Get network fabric topology for candidate node
	fabricType := getFabricType(node)
	fabricID := node.Labels[LabelFabricID]
	rackID := node.Labels[LabelRackID]
	az := node.Labels[LabelAZ]

	// Base score from fabric tier quality
	baseScore := getFabricTierScore(fabricType)

	// If not a gang member, just return fabric tier score
	if !isGangMember {
		klog.V(5).InfoS("NetworkFabricScore: scoring non-gang pod", "namespace", pod.Namespace, "pod", pod.Name, "node", node.Name, "fabric", fabricType, "score", baseScore)
		return int64(baseScore), nil
	}

	// For gang members, analyze existing pod placements
	gangPods := nf.getGangMemberPods(pod.Namespace, podGroup)

	if len(gangPods) == 0 {
		// First pod in gang, return base fabric score
		klog.V(4).InfoS("NetworkFabricScore: scoring first pod in gang", "namespace", pod.Namespace, "pod", pod.Name, "podGroup", podGroup, "node", node.Name, "fabric", fabricType, "score", baseScore)
		return int64(baseScore), nil
	}

	// Calculate locality bonuses/penalties based on gang member placement
	candidateClique := nf.getNodeClique(node)
	localityScore := calculateLocalityScore(gangPods, candidateClique, fabricID, rackID, az, nf.nodeLister)

	// Apply workload-aware fabric tier adjustment
	workloadAdjustment := nf.getWorkloadFabricBonus(state, pod, fabricType)

	finalScore := baseScore + localityScore + workloadAdjustment

	// Apply network sensitivity multiplier if specified
	if isNetworkSensitive(pod) {
		finalScore = int(float64(finalScore) * WeightNetworkSensitive)
	}

	// Check minimum fabric tier requirement
	if minTier := getMinFabricTier(pod); minTier != FabricUnknown {
		if !meetsFabricTierRequirement(fabricType, minTier) {
			klog.V(3).InfoS("NetworkFabricScore: fabric below minimum tier, returning 0", "namespace", pod.Namespace, "pod", pod.Name, "node", node.Name, "fabric", fabricType, "minTier", minTier)
			return 0, nil
		}
	}

	// Cap score at framework maximum
	if finalScore > 100 {
		finalScore = 100
	}
	if finalScore < 0 {
		finalScore = 0
	}

	klog.V(4).InfoS("NetworkFabricScore: scored gang pod", "namespace", pod.Namespace, "pod", pod.Name, "podGroup", podGroup, "node", node.Name, "fabric", fabricType, "baseScore", baseScore, "localityScore", localityScore, "workloadAdjustment", workloadAdjustment, "finalScore", finalScore)

	return int64(finalScore), nil
}

// getFabricType extracts fabric type from node labels.
func getFabricType(node *v1.Node) FabricType {
	fabricStr := node.Labels[LabelFabricType]
	switch strings.ToLower(fabricStr) {
	case "nvswitch":
		return FabricNVSwitch
	case "nvlink":
		return FabricNVLink
	case "infiniband", "ib":
		return FabricInfiniBand
	case "roce", "rdma":
		return FabricRoCE
	case "ethernet", "eth":
		return FabricEthernet
	default:
		return FabricUnknown
	}
}

// getFabricTierScore returns base score for fabric tier quality.
func getFabricTierScore(fabricType FabricType) int {
	switch fabricType {
	case FabricNVSwitch:
		return ScoreNVSwitch
	case FabricNVLink:
		return ScoreNVLink
	case FabricInfiniBand:
		return ScoreInfiniBand
	case FabricRoCE:
		return ScoreRoCE
	case FabricEthernet:
		return ScoreEthernet
	default:
		return ScoreUnknown
	}
}

// getGangMemberPods returns all scheduled pods in the same gang group.
func (nf *NetworkFabricScore) getGangMemberPods(namespace, podGroup string) []*v1.Pod {
	if podGroup == "" {
		return nil
	}

	allPods, err := nf.podLister.Pods(namespace).List(labels.Everything())
	if err != nil {
		klog.ErrorS(err, "NetworkFabricScore: failed to list pods", "namespace", namespace)
		return nil
	}

	// Filter for gang members that are scheduled
	var gangPods []*v1.Pod
	for _, pod := range allPods {
		// Only consider scheduled pods (have NodeName assigned)
		if pod.Spec.NodeName == "" {
			continue
		}
		// Check if pod is in same gang group
		if pod.Annotations[AnnotationPodGroup] == podGroup {
			gangPods = append(gangPods, pod)
		}
	}

	return gangPods
}

// calculateLocalityScore computes bonus/penalty based on gang member co-location
// across all topology levels: NVLink clique > fabric domain > rack > AZ.
func calculateLocalityScore(gangPods []*v1.Pod, candidateClique, candidateFabricID, candidateRackID, candidateAZ string, nodeLister corelisters.NodeLister) int {
	if len(gangPods) == 0 {
		return 0
	}

	// Get node info for all scheduled gang members
	cliques := make(map[string]int)       // NVLink clique -> count
	fabricDomains := make(map[string]int) // fabricID -> count
	racks := make(map[string]int)         // rackID -> count
	azs := make(map[string]int)           // az -> count

	// Fetch actual node labels from scheduled gang member nodes
	for _, pod := range gangPods {
		nodeName := pod.Spec.NodeName
		if nodeName == "" {
			continue
		}

		// Skip node lookup if nodeLister not provided (testing scenario)
		if nodeLister == nil {
			klog.V(5).InfoS("NetworkFabricScore: nodeLister nil, skipping node lookup", "node", nodeName)
			continue
		}

		node, err := nodeLister.Get(nodeName)
		if err != nil {
			klog.V(5).InfoS("NetworkFabricScore: failed to get node", "node", nodeName, "err", err)
			continue
		}

		if clique := node.Labels[LabelGPUClique]; clique != "" {
			cliques[clique]++
		}
		if fabricID := node.Labels[LabelFabricID]; fabricID != "" {
			fabricDomains[fabricID]++
		}
		if rackID := node.Labels[LabelRackID]; rackID != "" {
			racks[rackID]++
		}
		if az := node.Labels[LabelAZ]; az != "" {
			azs[az]++
		}
	}

	localityScore := 0

	// NVLink clique: strongest locality signal (GB200 NVL72 partition co-location)
	if candidateClique != "" {
		if count := cliques[candidateClique]; count > 0 {
			localityScore += BonusSameClique
			klog.V(5).InfoS("NetworkFabricScore: NVLink clique match", "clique", candidateClique, "count", count, "bonus", BonusSameClique)
		} else if len(cliques) > 0 {
			localityScore -= PenaltyCrossClique
			klog.V(5).InfoS("NetworkFabricScore: cross-clique placement", "penalty", PenaltyCrossClique)
		}
	}

	// Same fabric domain
	if candidateFabricID != "" {
		if count := fabricDomains[candidateFabricID]; count > 0 {
			localityScore += BonusSameFabricDomain
			klog.V(5).InfoS("NetworkFabricScore: fabric domain match", "fabricID", candidateFabricID, "count", count, "bonus", BonusSameFabricDomain)
		} else if len(fabricDomains) > 0 {
			localityScore -= PenaltyCrossFabric
			klog.V(5).InfoS("NetworkFabricScore: cross-fabric placement", "penalty", PenaltyCrossFabric)
		}
	}

	// Same rack
	if candidateRackID != "" {
		if count := racks[candidateRackID]; count > 0 {
			localityScore += BonusSameRack
		} else if len(racks) > 0 {
			localityScore -= PenaltyCrossRack
		}
	}

	// Same AZ
	if candidateAZ != "" {
		if count := azs[candidateAZ]; count > 0 {
			localityScore += BonusSameAZ
		} else if len(azs) > 0 {
			localityScore -= PenaltyCrossAZ
		}
	}

	return localityScore
}

// getWorkloadFabricBonus calculates fabric tier bonus/penalty based on workload type.
//
// FABRIC MATCHING LOGIC:
//   - Training workloads: Need high bandwidth (all-reduce), prefer NVSwitch/InfiniBand
//   - Inference workloads: Lower bandwidth needs, can use lower-tier fabrics
//   - Batch workloads: Similar to training for distributed batch jobs
//   - Service workloads: Similar to inference for serving endpoints
//
// Integration with ProfileClassifier:
//   - Uses profile.WorkloadType for centralized classification
//   - Falls back to local workload detection if ProfileClassifier unavailable
func (nf *NetworkFabricScore) getWorkloadFabricBonus(state framework.CycleState, pod *v1.Pod, fabricType FabricType) int {
	// Try ProfileClassifier first
	profile, err := profileclassifier.GetProfile(state)
	var workloadTypeStr string
	if err == nil && profile != nil {
		workloadTypeStr = strings.ToLower(string(profile.WorkloadType))
		klog.V(5).InfoS("NetworkFabricScore: workload type from ProfileClassifier", "namespace", pod.Namespace, "pod", pod.Name, "workloadType", workloadTypeStr)
	} else {
		// Fallback to local classification
		workloadType := workload.ClassifyPod(pod)
		switch workloadType {
		case workload.TypeBatch:
			workloadTypeStr = "training" // Batch often means training
		case workload.TypeService:
			workloadTypeStr = "inference" // Service often means inference
		default:
			workloadTypeStr = "unknown"
		}
		klog.V(5).InfoS("NetworkFabricScore: workload type from local classification", "namespace", pod.Namespace, "pod", pod.Name, "workloadType", workloadTypeStr)
	}

	// Classify fabric tier (high vs low)
	isHighTierFabric := fabricType == FabricNVSwitch || fabricType == FabricInfiniBand
	isLowTierFabric := fabricType == FabricRoCE || fabricType == FabricEthernet

	// Apply workload-specific bonuses/penalties
	switch workloadTypeStr {
	case "training", "batch":
		// Training needs high bandwidth for collective operations (all-reduce, all-gather)
		if isHighTierFabric {
			klog.V(5).InfoS("NetworkFabricScore: training workload on high-tier fabric", "bonus", BoostTrainingHighTier)
			return BoostTrainingHighTier
		}
		if isLowTierFabric {
			klog.V(5).InfoS("NetworkFabricScore: training workload on low-tier fabric", "penalty", -PenaltyTrainingLowTier)
			return -PenaltyTrainingLowTier
		}

	case "inference", "service":
		// Inference has lower bandwidth needs, can tolerate lower-tier fabrics
		if isLowTierFabric {
			klog.V(5).InfoS("NetworkFabricScore: inference workload on low-tier fabric", "bonus", BoostInferenceLowTier)
			return BoostInferenceLowTier
		}
		if isHighTierFabric {
			// Minor penalty for "wasting" high-tier fabric
			klog.V(5).InfoS("NetworkFabricScore: inference workload on high-tier fabric", "penalty", -PenaltyInferenceHighTier)
			return -PenaltyInferenceHighTier
		}

	default:
		// Unknown workload type, no adjustment
		return 0
	}

	// Default: no adjustment for mid-tier fabrics (NVLink)
	return 0
}

// isNetworkSensitive checks if pod is marked as network-sensitive workload.
func isNetworkSensitive(pod *v1.Pod) bool {
	val := pod.Annotations[AnnotationNetworkSensitive]
	return strings.ToLower(val) == "true"
}

// getMinFabricTier extracts minimum required fabric tier from pod annotations.
func getMinFabricTier(pod *v1.Pod) FabricType {
	minTier := pod.Annotations[AnnotationMinFabricTier]
	switch strings.ToLower(minTier) {
	case "nvswitch":
		return FabricNVSwitch
	case "nvlink":
		return FabricNVLink
	case "infiniband", "ib":
		return FabricInfiniBand
	case "roce":
		return FabricRoCE
	case "ethernet":
		return FabricEthernet
	default:
		return FabricUnknown
	}
}

// meetsFabricTierRequirement checks if fabric meets minimum tier requirement.
func meetsFabricTierRequirement(actual, minimum FabricType) bool {
	tierOrder := map[FabricType]int{
		FabricNVSwitch:   5,
		FabricNVLink:     4,
		FabricInfiniBand: 3,
		FabricRoCE:       2,
		FabricEthernet:   1,
		FabricUnknown:    0,
	}
	return tierOrder[actual] >= tierOrder[minimum]
}

// New initializes a new NetworkFabricScore plugin.
func New(ctx context.Context, obj runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	podLister := handle.SharedInformerFactory().Core().V1().Pods().Lister()
	nodeLister := handle.SharedInformerFactory().Core().V1().Nodes().Lister()

	// Initialize DRA ResourceSlice lister for clique fallback discovery
	var resourceSliceLister resourcev1listers.ResourceSliceLister
	if informerFactory := handle.SharedInformerFactory(); informerFactory != nil {
		resourceSliceLister = informerFactory.Resource().V1().ResourceSlices().Lister()
		klog.V(3).InfoS("NetworkFabricScore: DRA ResourceSlice lister initialized for clique discovery")
	}

	return &NetworkFabricScore{
		handle:              handle,
		podLister:           podLister,
		nodeLister:          nodeLister,
		resourceSliceLister: resourceSliceLister,
	}, nil
}
