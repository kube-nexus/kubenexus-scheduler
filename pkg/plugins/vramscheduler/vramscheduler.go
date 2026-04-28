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

// Package vramscheduler implements VRAM-aware GPU scheduling to prevent OOM-on-Arrival crashes
// and optimize VRAM utilization by matching workload requirements to GPU memory capacity.
//
// # Kubernetes Version Compatibility
//
// VRAMScheduler supports all Kubernetes versions through a 3-tier fallback strategy:
//
//   - Kubernetes 1.26+ with DRA driver: Full topology awareness (VRAM, NUMA, PCIe, NVLink)
//   - Kubernetes 1.18+ with NFD DaemonSet: Auto-discovered GPU detection via PCI scanning
//   - Any Kubernetes version: Manual node labels (operator-managed fallback)
//
// # VRAM Requirements Detection (Pod-side)
//
//  1. DRA ResourceClaims (Kubernetes v1.26+):
//     Pods declare GPU memory requirements via ResourceClaims:
//     spec:
//     resourceClaims:
//     - name: gpu-claim
//     resourceClaimTemplateName: gpu-template
//     # Also add annotation as hint for scheduler
//     metadata:
//     annotations:
//     scheduling.kubenexus.io/vram-request: "80Gi"
//
//  2. Annotation-based (Any Kubernetes version):
//     Pods specify VRAM requirements via annotations:
//     metadata:
//     annotations:
//     scheduling.kubenexus.io/vram-request: "80Gi"
//     scheduling.kubenexus.io/model-size: "70B"  # informational
//
// # Node VRAM Capacity Discovery (Node-side)
//
//  1. DRA ResourceSlices (Kubernetes v1.26+, preferred):
//     Automatic discovery from kubelet via DRA driver.
//     Provides: Full GPU topology (VRAM, NUMA, PCIe, NVLink domains).
//     Requires: DRA driver installed (e.g., nvidia-dra-driver, amd-device-plugin with DRA).
//
//  2. NFD Labels (Kubernetes v1.18+, auto-discovery fallback):
//     Automatic discovery via NodeFeatureDiscovery PCI scanning.
//     Provides: GPU detection, VRAM inference from PCI device ID, NUMA node count.
//     Requires: NFD DaemonSet installed.
//     Labels created:
//     feature.node.kubernetes.io/pci-10de.device.2330.present=true  # H100 detected
//     feature.node.kubernetes.io/memory-numa.node_count=2           # 2 NUMA nodes
//
//  3. Manual node labels (any Kubernetes version, operator fallback):
//     Operators manually label nodes:
//     gpu.kubenexus.io/vram: "80Gi"
//     gpu.kubenexus.io/model: "H100"
//     gpu.kubenexus.io/count: "8"
//
// # Migration Path
//
// Operators can migrate incrementally:
//  1. Start with manual labels (works immediately on any K8s version)
//  2. Install NFD for auto-discovery (eliminates manual labeling)
//  3. Upgrade to K8s 1.26+ and install DRA for full topology awareness
//
// All three methods can coexist - VRAMScheduler always prefers the most detailed source available.
package vramscheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	resourcev1listers "k8s.io/client-go/listers/resource/v1"
	klog "k8s.io/klog/v2"
	"k8s.io/kube-scheduler/framework"

	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/profileclassifier"
	schedulermetrics "github.com/kube-nexus/kubenexus-scheduler/pkg/scheduler"
)

const (
	// Name is the plugin name
	Name = "VRAMScheduler"

	// Pod annotations for VRAM requirements (fallback for non-DRA clusters or explicit hints)
	// Modern approach: Use DRA ResourceClaims with memory capacity
	// Legacy approach: Use these annotations
	AnnotationVRAMRequest = "scheduling.kubenexus.io/vram-request" // e.g., "80Gi", "24Gi"
	AnnotationModelSize   = "scheduling.kubenexus.io/model-size"   // e.g., "70B", "7B" (informational)

	// Node labels for GPU VRAM capacity (per-GPU) - fallback when DRA ResourceSlices unavailable
	LabelGPUVRAM     = "gpu.kubenexus.io/vram"     // e.g., "80Gi", "40Gi", "24Gi"
	LabelGPUModel    = "gpu.kubenexus.io/model"    // e.g., "H100", "A100-80GB", "L40S"
	LabelGPUCount    = "gpu.kubenexus.io/count"    // Total GPUs on node
	LabelGPUTopology = "topology.kubenexus.io/gpu" // e.g., "nvswitch", "nvlink"

	// NodeResourceTopology zones for per-GPU VRAM
	NRTZoneGPUPrefix = "gpu-"                  // e.g., "gpu-0", "gpu-1"
	NRTResourceVRAM  = "nvidia.com/gpu-memory" // VRAM resource in NRT

	// Scoring constants
	ScorePerfectFit       = 100 // VRAM request matches GPU capacity exactly
	ScoreGoodFit          = 80  // VRAM request is 70-95% of GPU capacity
	ScoreAcceptableFit    = 60  // VRAM request is 50-70% of GPU capacity
	ScorePoorFit          = 30  // VRAM request is 30-50% of GPU capacity (stranding VRAM)
	ScoreInsufficientVRAM = 0   // VRAM is insufficient

	// Fit thresholds (percentage of GPU VRAM)
	ThresholdPerfectFit    = 0.95 // 95-100% utilization
	ThresholdGoodFit       = 0.70 // 70-95% utilization
	ThresholdAcceptableFit = 0.50 // 50-70% utilization
	ThresholdPoorFit       = 0.30 // 30-50% utilization

	// Bonus/Penalty adjustments
	BonusHighEndGPU   = 10 // Bonus for scheduling on premium GPUs (H100, A100-80GB)
	PenaltyStrandVRAM = 20 // Penalty for stranding >50% of VRAM

	// Tenant-tier-specific VRAM thresholds
	// Gold tenants: Tighter thresholds (prevent waste of premium H100 VRAM)
	// Silver tenants: Standard thresholds
	// Bronze tenants: Looser thresholds (can tolerate lower utilization)

	// Gold tenant thresholds (90-100% preferred)
	GoldThresholdPerfectFit    = 0.98 // 98-100% utilization
	GoldThresholdGoodFit       = 0.85 // 85-98% utilization
	GoldThresholdAcceptableFit = 0.70 // 70-85% utilization
	GoldThresholdPoorFit       = 0.50 // 50-70% utilization

	// Silver tenant thresholds (same as default)
	SilverThresholdPerfectFit    = ThresholdPerfectFit    // 95-100%
	SilverThresholdGoodFit       = ThresholdGoodFit       // 70-95%
	SilverThresholdAcceptableFit = ThresholdAcceptableFit // 50-70%
	SilverThresholdPoorFit       = ThresholdPoorFit       // 30-50%

	// Bronze tenant thresholds (looser - can use underutilized GPUs)
	BronzeThresholdPerfectFit    = 0.90 // 90-100% utilization
	BronzeThresholdGoodFit       = 0.60 // 60-90% utilization
	BronzeThresholdAcceptableFit = 0.40 // 40-60% utilization
	BronzeThresholdPoorFit       = 0.20 // 20-40% utilization

	// GPU Topology scoring bonuses
	BonusGPUNUMALocality = 15 // Bonus when GPUs are on same NUMA node
	BonusNVLinkConnected = 25 // Bonus when GPUs have NVLink connectivity
	BonusPCIeLocality    = 10 // Bonus when GPUs share PCIe switch
)

// GPUDevice represents a GPU with its topology information
type GPUDevice struct {
	Name         string   // Device name from DRA (e.g., "gpu-0")
	VRAM         int64    // VRAM capacity in bytes
	NUMANode     int      // NUMA node ID (-1 if unknown)
	PCIeBusID    string   // PCIe bus ID (e.g., "0000:17:00.0")
	PCIeSwitch   string   // PCIe switch identifier
	NVLinkPeers  []string // List of GPU names with NVLink connections
	NVLinkDomain int      // NVLink domain/island ID (-1 if unknown)
}

// VRAMScheduler implements VRAM-aware scheduling to prevent OOM and optimize VRAM utilization
type VRAMScheduler struct {
	handle framework.Handle
	// Deprecated: clientset is kept for backward compatibility with older K8s versions
	// that may not have DRA informers. Use listers instead where possible.
	clientset kubernetes.Interface

	// DRA resource listers (initialized once in New())
	resourceSliceLister         resourcev1listers.ResourceSliceLister
	resourceClaimLister         resourcev1listers.ResourceClaimLister
	resourceClaimTemplateLister resourcev1listers.ResourceClaimTemplateLister
}

// Ensure VRAMScheduler implements required interfaces
var (
	_ framework.ScorePlugin  = &VRAMScheduler{}
	_ framework.FilterPlugin = &VRAMScheduler{}
)

// Name returns the plugin name
func (v *VRAMScheduler) Name() string {
	return Name
}

// Score calculates the score for scheduling a pod on a node based on VRAM requirements
func (v *VRAMScheduler) Score(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfo framework.NodeInfo) (int64, *framework.Status) {
	// Track plugin latency
	start := time.Now()
	defer func() {
		schedulermetrics.SchedulingLatency.WithLabelValues("VRAMScheduler", "Score").Observe(time.Since(start).Seconds())
	}()

	node := nodeInfo.Node()
	if node == nil {
		return 0, framework.NewStatus(framework.Error, "node not found")
	}

	// Determine workload type for metrics
	workloadType := "unknown"
	if profile, err := profileclassifier.GetProfile(state); err == nil && profile != nil {
		workloadType = string(profile.WorkloadType)
	}

	// Check if pod requests VRAM (using DRA-first fallback chain)
	vramRequest := v.getVRAMRequest(ctx, pod)
	if vramRequest == 0 {
		// No VRAM request - return neutral score
		klog.V(5).InfoS("Pod has no VRAM request, scoring neutrally",
			"pod", pod.Name, "namespace", pod.Namespace, "node", node.Name)
		schedulermetrics.VRAMPlacementDecisions.WithLabelValues("no_vram_request", workloadType, "none").Inc()
		return ScoreAcceptableFit, framework.NewStatus(framework.Success)
	}

	// Track VRAM requested
	schedulermetrics.VRAMRequestedBytes.WithLabelValues(pod.Namespace, workloadType).Observe(float64(vramRequest))

	// Get GPU VRAM capacity and topology from node (now reading from DRA ResourceSlices)
	gpuVRAM, gpuCount := v.getNodeGPUVRAM(ctx, node)
	_, gpuDevices := v.getNodeGPUTopology(ctx, node)

	if gpuVRAM == 0 {
		// Node has no GPU or VRAM info
		klog.V(5).InfoS("Node has no GPU VRAM information",
			"node", node.Name, "pod", pod.Name)
		schedulermetrics.VRAMPlacementDecisions.WithLabelValues("no_gpu_on_node", workloadType, "unknown").Inc()
		schedulermetrics.GPUAllocationFailures.WithLabelValues("no_gpu_info", workloadType).Inc()
		return ScoreInsufficientVRAM, framework.NewStatus(framework.Success)
	}

	// Calculate how many GPUs the pod needs based on GPU request
	gpusRequested := getGPURequest(pod)
	if gpusRequested == 0 && vramRequest > 0 {
		// Only default to 1 GPU if VRAM was explicitly requested
		gpusRequested = 1
	}

	// Calculate total VRAM needed and available
	totalVRAMNeeded := vramRequest
	totalVRAMPerGPU := gpuVRAM // VRAM per single GPU

	// Check if single GPU has enough VRAM
	if totalVRAMNeeded > totalVRAMPerGPU {
		// Need to check if multi-GPU setup can handle it
		totalAvailableVRAM := totalVRAMPerGPU * int64(gpusRequested)
		if totalVRAMNeeded > totalAvailableVRAM {
			klog.V(4).InfoS("Insufficient VRAM for pod",
				"pod", pod.Name,
				"node", node.Name,
				"vramNeeded", formatBytes(totalVRAMNeeded),
				"vramPerGPU", formatBytes(totalVRAMPerGPU),
				"gpusRequested", gpusRequested,
				"totalAvailable", formatBytes(totalAvailableVRAM))
			schedulermetrics.VRAMPlacementDecisions.WithLabelValues("insufficient_vram", workloadType, "unknown").Inc()
			schedulermetrics.GPUAllocationFailures.WithLabelValues("insufficient_vram", workloadType).Inc()
			return ScoreInsufficientVRAM, framework.NewStatus(framework.Success)
		}
		// Multi-GPU case: calculate utilization across all GPUs
		utilizationRatio := float64(totalVRAMNeeded) / float64(totalAvailableVRAM)
		tenantTier := v.getTenantTierFromProfile(state, pod)
		score := calculateUtilizationScore(utilizationRatio, tenantTier)

		klog.V(4).InfoS("Multi-GPU VRAM scheduling",
			"pod", pod.Name,
			"node", node.Name,
			"vramNeeded", formatBytes(totalVRAMNeeded),
			"gpusRequested", gpusRequested,
			"totalAvailable", formatBytes(totalAvailableVRAM),
			"utilization", fmt.Sprintf("%.1f%%", utilizationRatio*100),
			"score", score)

		return score, framework.NewStatus(framework.Success)
	}

	// Single GPU case: calculate utilization
	utilizationRatio := float64(totalVRAMNeeded) / float64(totalVRAMPerGPU)

	// Get tenant tier for threshold adjustment
	tenantTier := v.getTenantTierFromProfile(state, pod)
	score := calculateUtilizationScore(utilizationRatio, tenantTier)

	// Apply bonus for high-end GPUs (better for large models)
	if isHighEndGPU(node) && utilizationRatio >= ThresholdGoodFit {
		score += BonusHighEndGPU
		if score > 100 {
			score = 100
		}
		klog.V(5).InfoS("Applied high-end GPU bonus",
			"pod", pod.Name, "node", node.Name, "finalScore", score)
	}

	// Apply penalty for stranding VRAM (poor utilization)
	if utilizationRatio < ThresholdPoorFit {
		score -= PenaltyStrandVRAM
		if score < 0 {
			score = 0
		}
		klog.V(4).InfoS("Applied VRAM stranding penalty",
			"pod", pod.Name,
			"node", node.Name,
			"utilization", fmt.Sprintf("%.1f%%", utilizationRatio*100),
			"strandedVRAM", formatBytes(totalVRAMPerGPU-totalVRAMNeeded),
			"finalScore", score)
	}

	// Apply GPU topology bonuses for multi-GPU workloads
	if gpusRequested > 1 && len(gpuDevices) >= gpusRequested {
		topologyBonus := v.calculateGPUTopologyBonus(gpuDevices, gpusRequested, pod)
		score += topologyBonus
		if score > 100 {
			score = 100
		}
		if topologyBonus > 0 {
			klog.V(4).InfoS("Applied GPU topology bonus",
				"pod", pod.Name,
				"node", node.Name,
				"gpusRequested", gpusRequested,
				"topologyBonus", topologyBonus,
				"finalScore", score)
			schedulermetrics.TopologyQualityScore.WithLabelValues("multi_gpu").Observe(float64(score))
		}
	}

	// Track successful placement decision
	outcome := "success"
	if score >= ScoreGoodFit {
		outcome = "good_fit"
		schedulermetrics.GPUAllocationSuccess.WithLabelValues(workloadType, fmt.Sprintf("%d", gpusRequested), "true").Inc()
	} else if score >= ScoreAcceptableFit {
		outcome = "acceptable_fit"
		schedulermetrics.GPUAllocationSuccess.WithLabelValues(workloadType, fmt.Sprintf("%d", gpusRequested), "false").Inc()
	} else if score == ScorePoorFit {
		outcome = "poor_fit"
		schedulermetrics.FragmentationEvents.WithLabelValues("poor_utilization", "false").Inc()
	}
	schedulermetrics.VRAMPlacementDecisions.WithLabelValues(outcome, workloadType, "legacy").Inc()

	// Track VRAM utilization on this node
	gpuModel := node.Labels[LabelGPUModel]
	if gpuModel == "" {
		gpuModel = "unknown"
	}
	schedulermetrics.VRAMNodeUtilization.WithLabelValues(node.Name, gpuModel).Set(utilizationRatio * 100)

	klog.V(4).InfoS("VRAM-aware scheduling score",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"node", node.Name,
		"vramRequest", formatBytes(totalVRAMNeeded),
		"gpuVRAM", formatBytes(totalVRAMPerGPU),
		"gpuCount", gpuCount,
		"gpusRequested", gpusRequested,
		"utilization", fmt.Sprintf("%.1f%%", utilizationRatio*100),
		"score", score)

	return score, framework.NewStatus(framework.Success)
}

// ScoreExtensions returns nil (no normalization needed)
func (v *VRAMScheduler) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

// calculateGPUTopologyBonus computes bonus score based on GPU topology characteristics:
// - GPU-to-NUMA locality (all GPUs on same NUMA node)
// - NVLink connectivity (GPUs form a connected island)
// - PCIe locality (GPUs share same PCIe switch)
func (v *VRAMScheduler) calculateGPUTopologyBonus(gpuDevices []GPUDevice, gpusRequested int, pod *v1.Pod) int64 {
	if len(gpuDevices) < gpusRequested {
		return 0 // Cannot satisfy request
	}

	// Topology bonuses only apply to multi-GPU workloads
	if gpusRequested <= 1 {
		return 0
	}

	var totalBonus int64

	// Check for NUMA locality: all requested GPUs on same NUMA node
	numaLocality := v.checkGPUNUMALocality(gpuDevices, gpusRequested)
	if numaLocality {
		totalBonus += BonusGPUNUMALocality
		klog.V(5).InfoS("GPU-NUMA locality bonus applied",
			"pod", pod.Name,
			"bonus", BonusGPUNUMALocality)
		schedulermetrics.TopologyDecisions.WithLabelValues("NUMA", "true", "multi_gpu").Inc()
	} else {
		schedulermetrics.TopologyDecisions.WithLabelValues("NUMA", "false", "multi_gpu").Inc()
	}

	// Check for NVLink connectivity: requested GPUs form connected island
	nvlinkConnectivity := v.checkNVLinkConnectivity(gpuDevices, gpusRequested)
	if nvlinkConnectivity {
		totalBonus += BonusNVLinkConnected
		klog.V(5).InfoS("NVLink connectivity bonus applied",
			"pod", pod.Name,
			"bonus", BonusNVLinkConnected)
		schedulermetrics.TopologyDecisions.WithLabelValues("NVLink", "true", "multi_gpu").Inc()
	} else {
		schedulermetrics.TopologyDecisions.WithLabelValues("NVLink", "false", "multi_gpu").Inc()
	}

	// Check for PCIe locality: GPUs share same PCIe switch
	pcieLocality := v.checkPCIeLocality(gpuDevices, gpusRequested)
	if pcieLocality {
		totalBonus += BonusPCIeLocality
		klog.V(5).InfoS("PCIe locality bonus applied",
			"pod", pod.Name,
			"bonus", BonusPCIeLocality)
		schedulermetrics.TopologyDecisions.WithLabelValues("PCIe", "true", "multi_gpu").Inc()
	} else {
		schedulermetrics.TopologyDecisions.WithLabelValues("PCIe", "false", "multi_gpu").Inc()
	}

	return totalBonus
}

// checkGPUNUMALocality checks if gpusRequested GPUs can be found on same NUMA node
func (v *VRAMScheduler) checkGPUNUMALocality(gpuDevices []GPUDevice, gpusRequested int) bool {
	// Group GPUs by NUMA node
	numaGroups := make(map[int]int)
	for _, gpu := range gpuDevices {
		if gpu.NUMANode >= 0 {
			numaGroups[gpu.NUMANode]++
		}
	}

	// Check if any NUMA node has enough GPUs
	for _, count := range numaGroups {
		if count >= gpusRequested {
			return true
		}
	}
	return false
}

// checkNVLinkConnectivity checks if gpusRequested GPUs have NVLink connections forming a connected island
func (v *VRAMScheduler) checkNVLinkConnectivity(gpuDevices []GPUDevice, gpusRequested int) bool {
	// Option 1: All GPUs in same NVLink domain (pre-computed by DRA driver)
	if gpusRequested <= 1 {
		return false // Single GPU doesn't need NVLink
	}

	domainGroups := make(map[int]int)
	for _, gpu := range gpuDevices {
		if gpu.NVLinkDomain >= 0 {
			domainGroups[gpu.NVLinkDomain]++
		}
	}

	for _, count := range domainGroups {
		if count >= gpusRequested {
			return true
		}
	}

	// Option 2: Check peer-to-peer connectivity graph
	// Find largest connected component with NVLink peers
	// (simplified: check if any GPU has enough peers - full graph analysis would be more complex)
	for _, gpu := range gpuDevices {
		if len(gpu.NVLinkPeers) >= gpusRequested-1 {
			// This GPU is connected to enough peers (might form island)
			return true
		}
	}

	return false
}

// checkPCIeLocality checks if gpusRequested GPUs share same PCIe switch
func (v *VRAMScheduler) checkPCIeLocality(gpuDevices []GPUDevice, gpusRequested int) bool {
	// Group GPUs by PCIe switch
	pcieGroups := make(map[string]int)
	for _, gpu := range gpuDevices {
		if gpu.PCIeSwitch != "" {
			pcieGroups[gpu.PCIeSwitch]++
		}
	}

	// Check if any PCIe switch has enough GPUs
	for _, count := range pcieGroups {
		if count >= gpusRequested {
			return true
		}
	}
	return false
}

// Filter filters out nodes that don't have sufficient VRAM
func (v *VRAMScheduler) Filter(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfo framework.NodeInfo) *framework.Status {
	node := nodeInfo.Node()
	if node == nil {
		return framework.NewStatus(framework.Error, "node not found")
	}

	// Check if pod requests VRAM (using DRA-first fallback chain)
	vramRequest := v.getVRAMRequest(ctx, pod)
	if vramRequest == 0 {
		// No VRAM requirement - allow scheduling
		return framework.NewStatus(framework.Success)
	}

	// Get GPU VRAM capacity from node (using DRA-first fallback chain)
	gpuVRAM, _ := v.getNodeGPUVRAM(ctx, node)
	if gpuVRAM == 0 {
		// Node has no GPU VRAM info - filter out
		return framework.NewStatus(framework.UnschedulableAndUnresolvable,
			fmt.Sprintf("node %s has no GPU VRAM information", node.Name))
	}

	// Get number of GPUs requested
	gpusRequested := getGPURequest(pod)
	if gpusRequested == 0 {
		gpusRequested = 1
	}

	// Calculate total available VRAM
	totalAvailableVRAM := gpuVRAM * int64(gpusRequested)

	// Filter out if insufficient VRAM
	if vramRequest > totalAvailableVRAM {
		return framework.NewStatus(framework.Unschedulable,
			fmt.Sprintf("insufficient VRAM: need %s, available %s (%d GPUs × %s)",
				formatBytes(vramRequest),
				formatBytes(totalAvailableVRAM),
				gpusRequested,
				formatBytes(gpuVRAM)))
	}

	klog.V(5).InfoS("Node passes VRAM filter",
		"pod", pod.Name,
		"node", node.Name,
		"vramRequest", formatBytes(vramRequest),
		"availableVRAM", formatBytes(totalAvailableVRAM))

	return framework.NewStatus(framework.Success)
}

// calculateUtilizationScore calculates score based on VRAM utilization ratio and tenant tier.
//
// TENANT-TIER-AWARE THRESHOLDS:
//   - Gold tenants: Tighter thresholds (prevent wasting premium H100 VRAM)
//   - Silver tenants: Standard thresholds
//   - Bronze tenants: Looser thresholds (can tolerate lower utilization, use underutilized GPUs)
//
// This creates economic incentives:
//   - Gold tenants must efficiently use expensive H100 GPUs
//   - Bronze tenants can "backfill" underutilized GPUs
func calculateUtilizationScore(utilizationRatio float64, tenantTier string) int64 {
	// Select thresholds based on tenant tier
	var perfectFit, goodFit, acceptableFit, poorFit float64

	switch strings.ToLower(tenantTier) {
	case "gold":
		perfectFit = GoldThresholdPerfectFit
		goodFit = GoldThresholdGoodFit
		acceptableFit = GoldThresholdAcceptableFit
		poorFit = GoldThresholdPoorFit
	case "silver":
		perfectFit = SilverThresholdPerfectFit
		goodFit = SilverThresholdGoodFit
		acceptableFit = SilverThresholdAcceptableFit
		poorFit = SilverThresholdPoorFit
	case "bronze":
		perfectFit = BronzeThresholdPerfectFit
		goodFit = BronzeThresholdGoodFit
		acceptableFit = BronzeThresholdAcceptableFit
		poorFit = BronzeThresholdPoorFit
	default:
		// Unknown tenant, use silver (standard) thresholds
		perfectFit = SilverThresholdPerfectFit
		goodFit = SilverThresholdGoodFit
		acceptableFit = SilverThresholdAcceptableFit
		poorFit = SilverThresholdPoorFit
	}

	// Calculate score based on tenant-specific thresholds
	if utilizationRatio >= perfectFit {
		return ScorePerfectFit
	}
	if utilizationRatio >= goodFit {
		return ScoreGoodFit
	}
	if utilizationRatio >= acceptableFit {
		return ScoreAcceptableFit
	}
	if utilizationRatio >= poorFit {
		return ScorePoorFit
	}
	return ScoreInsufficientVRAM
}

// getTenantTierFromProfile gets tenant tier using ProfileClassifier.
//
// Integration with ProfileClassifier:
//   - Uses profile.TenantTier for centralized classification
//   - Returns "gold", "silver", or "bronze"
//   - Defaults to "bronze" if ProfileClassifier unavailable (most permissive)
func (v *VRAMScheduler) getTenantTierFromProfile(state framework.CycleState, pod *v1.Pod) string {
	profile, err := profileclassifier.GetProfile(state)
	if err == nil && profile != nil {
		return strings.ToLower(string(profile.TenantTier))
	}
	// Default to bronze (most permissive) if ProfileClassifier unavailable
	return "bronze"
}

// getVRAMRequest extracts VRAM request from pod using priority-based fallback chain.
//
// 2-Tier Fallback Strategy:
//  1. DRA ResourceClaims (K8s 1.26+, preferred method)
//  2. Annotation scheduling.kubenexus.io/vram-request (any K8s version, manual or operator-set)
//
// Returns VRAM requirement in bytes, or 0 if not specified.
func (v *VRAMScheduler) getVRAMRequest(ctx context.Context, pod *v1.Pod) int64 {
	// PRIORITY 1: DRA ResourceClaims (Kubernetes 1.26+)
	if len(pod.Spec.ResourceClaims) > 0 {
		vram, err := v.getVRAMFromResourceClaim(ctx, pod)
		if err == nil && vram > 0 {
			klog.V(4).InfoS("✅ Using VRAM requirement from DRA ResourceClaim",
				"pod", klog.KObj(pod),
				"source", "DRA",
				"vram", formatBytes(vram))
			return vram
		}
		klog.V(5).InfoS("DRA ResourceClaims present but couldn't extract VRAM, trying annotation",
			"pod", klog.KObj(pod),
			"reason", err)
	}

	// PRIORITY 2: Annotation (any Kubernetes version)
	// Users or operators can set: scheduling.kubenexus.io/vram-request: "80Gi"
	if vramStr, ok := pod.Annotations[AnnotationVRAMRequest]; ok {
		quantity, err := resource.ParseQuantity(vramStr)
		if err != nil {
			klog.V(4).InfoS("Failed to parse VRAM request annotation",
				"pod", klog.KObj(pod),
				"vramRequest", vramStr,
				"error", err)
			return 0
		}
		vramBytes := quantity.Value()
		klog.V(4).InfoS("✅ Using VRAM request from pod annotation",
			"pod", klog.KObj(pod),
			"source", "annotation",
			"vramRequest", vramStr,
			"bytes", vramBytes)
		return vramBytes
	}

	// No VRAM requirement specified - pod will be scored based on GPU count only
	klog.V(6).InfoS("No VRAM requirement specified for pod",
		"pod", klog.KObj(pod),
		"hint", "Set annotation scheduling.kubenexus.io/vram-request for VRAM-aware scheduling")
	return 0
}

// getNodeGPUVRAM extracts GPU VRAM capacity from DRA Resource Slices (returns per-GPU VRAM in bytes and total GPU count)
// This function now queries the Kubernetes API for ResourceSlices associated with the node
func (v *VRAMScheduler) getNodeGPUVRAM(ctx context.Context, node *v1.Node) (int64, int) {
	// New: also return GPU topology information
	_, gpuDevices := v.getNodeGPUTopology(ctx, node)

	if len(gpuDevices) == 0 {
		// Fallback to labels
		return getNodeGPUVRAMFromLabels(node)
	}

	// Use minimum VRAM for heterogeneous GPUs
	var vramPerGPU int64
	for _, gpu := range gpuDevices {
		if vramPerGPU == 0 || gpu.VRAM < vramPerGPU {
			vramPerGPU = gpu.VRAM
		}
	}

	return vramPerGPU, len(gpuDevices)
}

// getNodeGPUTopology extracts full GPU topology information using priority-based fallback chain.
//
// 3-Tier Fallback Strategy (for Kubernetes version compatibility):
//  1. DRA ResourceSlices (K8s 1.26+, most detailed, dynamic)
//  2. NFD labels (any K8s version with NFD installed, auto-discovered)
//  3. Manual node labels (any K8s version, operator-managed fallback)
//
// Returns (vramPerGPU, []GPUDevice with full topology)
func (v *VRAMScheduler) getNodeGPUTopology(ctx context.Context, node *v1.Node) (int64, []GPUDevice) {
	// PRIORITY 1: DRA ResourceSlices (Kubernetes 1.34+)
	// Provides: Full topology (VRAM, NUMA, PCIe, NVLink), dynamic updates
	if v.resourceSliceLister != nil {
		// Add 5-second timeout to DRA queries to prevent scheduler stalls
		draCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		vramPerGPU, devices, err := v.getGPUTopologyFromDRA(draCtx, node)
		cancel()

		if err == nil && len(devices) > 0 {
			klog.V(4).InfoS("✅ Using GPU topology from DRA ResourceSlices",
				"node", node.Name,
				"source", "DRA",
				"gpuCount", len(devices),
				"vramPerGPU", formatBytes(vramPerGPU))
			schedulermetrics.DataSourceUsage.WithLabelValues("DRA").Inc()
			return vramPerGPU, devices
		}
		if err == context.DeadlineExceeded {
			klog.V(3).InfoS("DRA query timeout, falling back to NFD",
				"node", node.Name)
		} else {
			klog.V(5).InfoS("DRA ResourceSlices not available, trying NFD labels",
				"node", node.Name,
				"reason", err)
		}
	} else {
		klog.V(5).InfoS("DRA not available (lister nil), trying NFD labels",
			"node", node.Name)
	}

	// PRIORITY 2: NFD (Node Feature Discovery) labels (K8s 1.18+)
	// Provides: GPU detection via PCI scanning, NUMA node count, partial topology
	// Requires: NFD DaemonSet installed on cluster
	vramPerGPU, devices, err := v.getTopologyFromNFD(node)
	if err == nil && len(devices) > 0 {
		klog.V(4).InfoS("✅ Using GPU topology from NFD labels (auto-discovered)",
			"node", node.Name,
			"source", "NFD",
			"gpuCount", len(devices),
			"vramPerGPU", formatBytes(vramPerGPU))
		schedulermetrics.DataSourceUsage.WithLabelValues("NFD").Inc()
		return vramPerGPU, devices
	}
	klog.V(5).InfoS("NFD labels not found, falling back to manual labels",
		"node", node.Name,
		"reason", err)

	// PRIORITY 3: Manual node labels (any Kubernetes version)
	// Provides: Basic VRAM and GPU count only
	// Requires: Operators to manually label nodes
	klog.V(4).InfoS("⚠️  Using GPU topology from manual node labels (operator-managed)",
		"node", node.Name,
		"source", "manual-labels")
	schedulermetrics.DataSourceUsage.WithLabelValues("manual_labels").Inc()
	vramPerGPU, gpuCount := getNodeGPUVRAMFromLabels(node)
	devices = make([]GPUDevice, gpuCount)
	for i := range devices {
		devices[i] = GPUDevice{
			Name:         fmt.Sprintf("gpu-%d", i),
			VRAM:         vramPerGPU,
			NUMANode:     -1, // Unknown from manual labels
			NVLinkDomain: -1, // Unknown from manual labels
		}
	}
	return vramPerGPU, devices
}

// isGPUDriver checks if the driver name indicates a GPU resource driver
func isGPUDriver(driver string) bool {
	driverLower := strings.ToLower(driver)
	gpuPatterns := []string{"gpu", "nvidia", "amd", "intel.com/gpu", "accelerator"}
	for _, pattern := range gpuPatterns {
		if strings.Contains(driverLower, pattern) {
			return true
		}
	}
	return false
}

// getNodeGPUVRAMFromLabels extracts GPU VRAM capacity from node labels (fallback method)
// Returns per-GPU VRAM in bytes and total GPU count
func getNodeGPUVRAMFromLabels(node *v1.Node) (int64, int) {
	// Try to get VRAM from label
	if vramStr, ok := node.Labels[LabelGPUVRAM]; ok {
		quantity, err := resource.ParseQuantity(vramStr)
		if err != nil {
			klog.V(4).InfoS("Failed to parse GPU VRAM label",
				"node", node.Name, "vram", vramStr, "error", err)
			return 0, 0
		}
		vramBytes := quantity.Value()

		// Get GPU count
		gpuCount := 1
		if countStr, ok := node.Labels[LabelGPUCount]; ok {
			if count, err := strconv.Atoi(countStr); err == nil {
				gpuCount = count
			}
		}

		klog.V(5).InfoS("Parsed VRAM from node label",
			"node", node.Name, "vramPerGPU", formatBytes(vramBytes), "gpuCount", gpuCount)
		return vramBytes, gpuCount
	}

	// Fallback: Infer from GPU model label
	if gpuModel, ok := node.Labels[LabelGPUModel]; ok {
		vramBytes := inferVRAMFromModel(gpuModel)
		if vramBytes > 0 {
			// Get GPU count
			gpuCount := 1
			if countStr, ok := node.Labels[LabelGPUCount]; ok {
				if count, err := strconv.Atoi(countStr); err == nil {
					gpuCount = count
				}
			}

			klog.V(5).InfoS("Inferred VRAM from GPU model",
				"node", node.Name, "gpuModel", gpuModel, "vramPerGPU", formatBytes(vramBytes), "gpuCount", gpuCount)
			return vramBytes, gpuCount
		}
	}

	// No VRAM information available
	return 0, 0
}

// inferVRAMFromModel infers VRAM capacity from GPU model name
func inferVRAMFromModel(gpuModel string) int64 {
	model := strings.ToUpper(gpuModel)

	// Map of known GPU models to VRAM capacity (in bytes)
	// Check specific models first, then fall back to base models
	vramMap := []struct {
		modelKey string
		vram     int64
	}{
		// Specific models first (longest match wins)
		{"H100-80GB", 80 * 1024 * 1024 * 1024},
		{"H200", 141 * 1024 * 1024 * 1024},
		{"H100", 80 * 1024 * 1024 * 1024},
		{"A100-80GB", 80 * 1024 * 1024 * 1024},
		{"A100-40GB", 40 * 1024 * 1024 * 1024},
		{"A100", 40 * 1024 * 1024 * 1024},
		{"L40S", 48 * 1024 * 1024 * 1024}, // Check L40S before L40
		{"L40", 48 * 1024 * 1024 * 1024},
		{"L4", 24 * 1024 * 1024 * 1024},
		{"A40", 48 * 1024 * 1024 * 1024},
		{"A30", 24 * 1024 * 1024 * 1024},
		{"T4", 16 * 1024 * 1024 * 1024},
		{"V100-32GB", 32 * 1024 * 1024 * 1024},
		{"V100-16GB", 16 * 1024 * 1024 * 1024},
		{"V100", 32 * 1024 * 1024 * 1024},
		{"RTX8000", 48 * 1024 * 1024 * 1024},
		{"RTX6000", 24 * 1024 * 1024 * 1024},
		{"MI300", 192 * 1024 * 1024 * 1024}, // AMD MI300X
	}

	for _, entry := range vramMap {
		if strings.Contains(model, entry.modelKey) {
			return entry.vram
		}
	}

	return 0 // Unknown model
}

// isHighEndGPU checks if node has high-end GPUs (H100, H200, A100-80GB)
func isHighEndGPU(node *v1.Node) bool {
	if gpuModel, ok := node.Labels[LabelGPUModel]; ok {
		model := strings.ToUpper(gpuModel)
		highEndModels := []string{"H100", "H200", "A100-80GB", "MI300"}
		for _, highEnd := range highEndModels {
			if strings.Contains(model, highEnd) {
				return true
			}
		}
	}
	return false
}

// getGPURequest returns the number of GPUs requested by the pod
func getGPURequest(pod *v1.Pod) int {
	totalGPUs := int64(0)
	for _, container := range pod.Spec.Containers {
		if gpuQuantity, ok := container.Resources.Requests[v1.ResourceName("nvidia.com/gpu")]; ok {
			totalGPUs += gpuQuantity.Value()
		}
	}
	return int(totalGPUs)
}

// formatBytes formats bytes into human-readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// New creates a new VRAMScheduler plugin
func New(_ context.Context, _ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	klog.V(3).InfoS("Creating new VRAMScheduler plugin with DRA ResourceSlice support")

	// Get clientset from scheduler handle for ResourceSlice queries
	var clientset kubernetes.Interface
	kubeConfig := handle.KubeConfig()
	if kubeConfig != nil {
		var err error
		clientset, err = kubernetes.NewForConfig(kubeConfig)
		if err != nil {
			klog.ErrorS(err, "Failed to create clientset for VRAMScheduler, will use label fallback only")
			clientset = nil
		}
	} else {
		klog.V(4).InfoS("No KubeConfig available (likely in test mode), will use label fallback only")
		clientset = nil
	}

	// Initialize DRA resource listers from shared informer factory (K8s 1.34+)
	// These are initialized once and reused for efficiency
	var resourceSliceLister resourcev1listers.ResourceSliceLister
	var resourceClaimLister resourcev1listers.ResourceClaimLister
	var resourceClaimTemplateLister resourcev1listers.ResourceClaimTemplateLister

	informerFactory := handle.SharedInformerFactory()
	if informerFactory != nil {
		resourceSliceLister = informerFactory.Resource().V1().ResourceSlices().Lister()
		resourceClaimLister = informerFactory.Resource().V1().ResourceClaims().Lister()
		resourceClaimTemplateLister = informerFactory.Resource().V1().ResourceClaimTemplates().Lister()
		klog.V(3).InfoS("DRA listers initialized successfully")
	} else {
		klog.V(4).InfoS("SharedInformerFactory not available, DRA support will be limited")
	}

	return &VRAMScheduler{
		handle:                      handle,
		clientset:                   clientset,
		resourceSliceLister:         resourceSliceLister,
		resourceClaimLister:         resourceClaimLister,
		resourceClaimTemplateLister: resourceClaimTemplateLister,
	}, nil
}
