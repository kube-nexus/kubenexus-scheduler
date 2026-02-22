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
// (racks, pods, availability zones), network performance degrades significantly:
//   - Same rack (NVSwitch): ~300-600 GB/s GPU-GPU bandwidth
//   - Cross-rack (InfiniBand/RoCE): ~100-200 GB/s bandwidth  
//   - Cross-AZ (backbone): ~10-25 GB/s bandwidth
//   - Performance impact: 3-10x slowdown in collective operations (all-reduce)
//
// THE SOLUTION:
// This plugin scores nodes based on network fabric topology to place gang members
// close together on the network, maximizing bandwidth and minimizing latency.
//
// NETWORK FABRIC TIERS (from best to worst):
//   1. NVSwitch Fabric: GPU-to-GPU direct, 900 GB/s, <1μs latency (DGX SuperPods)
//   2. NVLink Domain: GPU-to-GPU in single node, 600 GB/s, <2μs latency
//   3. InfiniBand EDR/HDR: High-speed RDMA, 100-200 GB/s, <10μs latency
//   4. RoCE v2 (100GbE): Ethernet-based RDMA, 12.5-25 GB/s, ~50μs latency  
//   5. Standard Ethernet: 1-10 GbE, 125 MB - 1.25 GB/s, >100μs latency
//
// SCORING STRATEGY:
//   - Co-locate gang members in same network domain when possible
//   - Prefer higher-tier fabrics for communication-intensive workloads
//   - Consider existing pod placement to avoid fragmenting network domains
//   - Balance with other constraints (GPU availability, NUMA, etc.)
//
// NODE LABELS (required for fabric detection):
//   network.kubenexus.io/fabric-type: "nvswitch|nvlink|infiniband|roce|ethernet"
//   network.kubenexus.io/fabric-id: "<unique-fabric-domain-id>"
//   network.kubenexus.io/rack-id: "<rack-identifier>"
//   network.kubenexus.io/az: "<availability-zone>"
//
// POD ANNOTATIONS (optional overrides):
//   scheduling.kubenexus.io/network-sensitive: "true|false"  # Boost scoring weight
//   scheduling.kubenexus.io/min-fabric-tier: "nvswitch|infiniband|roce"  # Minimum required
//   scheduling.kubenexus.io/co-locate: "strict|preferred|none"  # Gang locality requirement
//
// EXAMPLE TOPOLOGY:
//   Rack A (NVSwitch Fabric "nsw-fabric-01"):
//     Node 1, Node 2, Node 3, Node 4 (8 GPUs each) - 900 GB/s interconnect
//   Rack B (InfiniBand "ib-fabric-02"):  
//     Node 5, Node 6, Node 7, Node 8 (8 GPUs each) - 200 GB/s interconnect
//
//   Gang of 4 pods × 8 GPUs = 32 GPUs total
//   ✅ GOOD: All in Rack A (NVSwitch, same fabric domain)
//   ⚠️ OK: All in Rack B (InfiniBand, same fabric domain)
//   ❌ BAD: 2 in Rack A, 2 in Rack B (cross-rack, mixed fabrics)
//
// INTEGRATION WITH OTHER PLUGINS:
//   - Works with Coscheduling plugin for gang awareness
//   - Complements ResourceFragmentation for GPU placement
//   - Coordinates with NUMA topology for intra-node optimization
package networkfabric

import (
	"context"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
	framework "k8s.io/kube-scheduler/framework"
)

// NetworkFabricScore implements network topology-aware scoring for gang scheduling.
type NetworkFabricScore struct {
	handle     framework.Handle
	podLister  corelisters.PodLister
	nodeLister corelisters.NodeLister
}

var _ framework.ScorePlugin = &NetworkFabricScore{}

const (
	// Name is the name of the plugin used in the plugin registry and configurations.
	Name = "NetworkFabricScore"

	// Node labels for network fabric topology
	LabelFabricType = "network.kubenexus.io/fabric-type" // nvswitch|nvlink|infiniband|roce|ethernet
	LabelFabricID   = "network.kubenexus.io/fabric-id"   // Unique fabric domain identifier
	LabelRackID     = "network.kubenexus.io/rack-id"     // Rack identifier
	LabelAZ         = "network.kubenexus.io/az"          // Availability zone

	// Pod annotations
	AnnotationNetworkSensitive = "scheduling.kubenexus.io/network-sensitive" // true|false
	AnnotationMinFabricTier    = "scheduling.kubenexus.io/min-fabric-tier"   // nvswitch|infiniband|roce
	AnnotationCoLocate         = "scheduling.kubenexus.io/co-locate"         // strict|preferred|none
	AnnotationPodGroup         = "pod-group.scheduling.sigs.k8s.io/name"     // Gang group name

	// Fabric tier scores (higher is better)
	ScoreNVSwitch   = 100 // DGX SuperPod with NVSwitch fabric
	ScoreNVLink     = 90  // Single-node NVLink domain
	ScoreInfiniBand = 75  // InfiniBand EDR/HDR
	ScoreRoCE       = 60  // RoCE v2 over 100GbE
	ScoreEthernet   = 40  // Standard Ethernet (fallback)
	ScoreUnknown    = 50  // No fabric info, neutral score

	// Locality bonuses (added to base fabric score)
	BonusSameFabricDomain = 30 // All gang members in same fabric domain
	BonusSameRack         = 20 // All gang members in same rack
	BonusSameAZ           = 10 // All gang members in same AZ
	PenaltyCrossFabric    = 30 // Gang members split across fabric domains
	PenaltyCrossRack      = 20 // Gang members split across racks
	PenaltyCrossAZ        = 10 // Gang members split across AZs

	// Network sensitivity multiplier
	WeightNetworkSensitive = 1.5 // Boost scoring for network-intensive workloads
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
		klog.V(5).Infof("NetworkFabricScore: pod %s/%s (non-gang) on node %s, fabric=%s, score=%d",
			pod.Namespace, pod.Name, node.Name, fabricType, baseScore)
		return int64(baseScore), nil
	}

	// For gang members, analyze existing pod placements
	gangPods := nf.getGangMemberPods(pod.Namespace, podGroup)
	
	if len(gangPods) == 0 {
		// First pod in gang, return base fabric score
		klog.V(4).Infof("NetworkFabricScore: pod %s/%s (first in gang %s) on node %s, fabric=%s, score=%d",
			pod.Namespace, pod.Name, podGroup, node.Name, fabricType, baseScore)
		return int64(baseScore), nil
	}

	// Calculate locality bonuses/penalties based on gang member placement
	localityScore := calculateLocalityScore(gangPods, fabricID, rackID, az, nf.nodeLister)
	finalScore := baseScore + localityScore

	// Apply network sensitivity multiplier if specified
	if isNetworkSensitive(pod) {
		finalScore = int(float64(finalScore) * WeightNetworkSensitive)
	}

	// Check minimum fabric tier requirement
	if minTier := getMinFabricTier(pod); minTier != FabricUnknown {
		if !meetsFabricTierRequirement(fabricType, minTier) {
			klog.V(3).Infof("NetworkFabricScore: pod %s/%s on node %s, fabric %s below minimum %s, returning 0",
				pod.Namespace, pod.Name, node.Name, fabricType, minTier)
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

	klog.V(4).Infof("NetworkFabricScore: pod %s/%s (gang %s) on node %s, fabric=%s, base=%d, locality=%d, final=%d",
		pod.Namespace, pod.Name, podGroup, node.Name, fabricType, baseScore, localityScore, finalScore)

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
		klog.Errorf("NetworkFabricScore: failed to list pods in namespace %s: %v", namespace, err)
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

// calculateLocalityScore computes bonus/penalty based on gang member co-location.
func calculateLocalityScore(gangPods []*v1.Pod, candidateFabricID, candidateRackID, candidateAZ string, nodeLister corelisters.NodeLister) int {
	if len(gangPods) == 0 {
		return 0
	}

	// Get node info for all scheduled gang members
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
			klog.V(5).Infof("NetworkFabricScore: nodeLister nil, skipping node lookup for %s", nodeName)
			continue
		}

		// Get node from node lister
		node, err := nodeLister.Get(nodeName)
		if err != nil {
			klog.V(5).Infof("NetworkFabricScore: failed to get node %s: %v", nodeName, err)
			continue
		}

		// Extract topology info from node labels
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

	// Calculate locality bonus/penalty
	localityScore := 0

	// Same fabric domain: strong bonus (best for performance)
	if candidateFabricID != "" {
		if count := fabricDomains[candidateFabricID]; count > 0 {
			localityScore += BonusSameFabricDomain
			klog.V(5).Infof("NetworkFabricScore: fabric domain match %s (count=%d), bonus=%d",
				candidateFabricID, count, BonusSameFabricDomain)
		} else if len(fabricDomains) > 0 {
			// Would split gang across fabric domains
			localityScore -= PenaltyCrossFabric
			klog.V(5).Infof("NetworkFabricScore: cross-fabric placement, penalty=%d", PenaltyCrossFabric)
		}
	}

	// Same rack: moderate bonus
	if candidateRackID != "" {
		if count := racks[candidateRackID]; count > 0 {
			localityScore += BonusSameRack
		} else if len(racks) > 0 {
			localityScore -= PenaltyCrossRack
		}
	}

	// Same AZ: small bonus
	if candidateAZ != "" {
		if count := azs[candidateAZ]; count > 0 {
			localityScore += BonusSameAZ
		} else if len(azs) > 0 {
			localityScore -= PenaltyCrossAZ
		}
	}

	return localityScore
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

	return &NetworkFabricScore{
		handle:     handle,
		podLister:  podLister,
		nodeLister: nodeLister,
	}, nil
}
