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
// VRAM Requirements - Two Approaches:
//
//  1. DRA (Dynamic Resource Allocation) - Modern Kubernetes v1.26+:
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
//  2. Annotation-based - Legacy/Simple approach:
//     Pods specify VRAM requirements via annotations:
//     metadata:
//     annotations:
//     scheduling.kubenexus.io/vram-request: "80Gi"
//     scheduling.kubenexus.io/model-size: "70B"  # informational
//
// Node VRAM Capacity - Two Sources:
//
//  1. DRA ResourceSlices (preferred):
//     Automatic discovery from kubelet via DRA driver
//
//  2. Node labels (fallback):
//     Manual labeling:
//     gpu.kubenexus.io/vram: "80Gi"
//     gpu.kubenexus.io/model: "H100"
//     gpu.kubenexus.io/count: "8"
package vramscheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	klog "k8s.io/klog/v2"
	"k8s.io/kube-scheduler/framework"

	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/profileclassifier"
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
	handle    framework.Handle
	clientset kubernetes.Interface
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
	node := nodeInfo.Node()
	if node == nil {
		return 0, framework.NewStatus(framework.Error, "node not found")
	}

	// Check if pod requests VRAM
	vramRequest := getVRAMRequest(pod)
	if vramRequest == 0 {
		// No VRAM request - return neutral score
		klog.V(5).InfoS("Pod has no VRAM request, scoring neutrally",
			"pod", pod.Name, "namespace", pod.Namespace, "node", node.Name)
		return ScoreAcceptableFit, framework.NewStatus(framework.Success)
	}

	// Get GPU VRAM capacity and topology from node (now reading from DRA ResourceSlices)
	gpuVRAM, gpuCount := v.getNodeGPUVRAM(ctx, node)
	_, gpuDevices := v.getNodeGPUTopology(ctx, node)

	if gpuVRAM == 0 {
		// Node has no GPU or VRAM info
		klog.V(5).InfoS("Node has no GPU VRAM information",
			"node", node.Name, "pod", pod.Name)
		return ScoreInsufficientVRAM, framework.NewStatus(framework.Success)
	}

	// Calculate how many GPUs the pod needs based on GPU request
	gpusRequested := getGPURequest(pod)
	if gpusRequested == 0 {
		gpusRequested = 1 // Default to 1 GPU if not specified
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
		}
	}

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
	}

	// Check for NVLink connectivity: requested GPUs form connected island
	nvlinkConnectivity := v.checkNVLinkConnectivity(gpuDevices, gpusRequested)
	if nvlinkConnectivity {
		totalBonus += BonusNVLinkConnected
		klog.V(5).InfoS("NVLink connectivity bonus applied",
			"pod", pod.Name,
			"bonus", BonusNVLinkConnected)
	}

	// Check for PCIe locality: GPUs share same PCIe switch
	pcieLocality := v.checkPCIeLocality(gpuDevices, gpusRequested)
	if pcieLocality {
		totalBonus += BonusPCIeLocality
		klog.V(5).InfoS("PCIe locality bonus applied",
			"pod", pod.Name,
			"bonus", BonusPCIeLocality)
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

	// Check if pod requests VRAM
	vramRequest := getVRAMRequest(pod)
	if vramRequest == 0 {
		// No VRAM requirement - allow scheduling
		return framework.NewStatus(framework.Success)
	}

	// Get GPU VRAM capacity from node (now reading from DRA ResourceSlices)
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
	profile, err := profileclassifier.GetProfile(&state)
	if err == nil && profile != nil {
		return strings.ToLower(string(profile.TenantTier))
	}
	// Default to bronze (most permissive) if ProfileClassifier unavailable
	return "bronze"
}

// getVRAMRequest extracts VRAM request from pod ResourceClaims (DRA) or annotations (fallback)
// Returns VRAM requirement in bytes
//
// DRA-first approach:
//  1. Check pod.spec.resourceClaims for GPU memory requirements
//  2. Fall back to annotation for non-DRA clusters or explicit overrides
//
// This supports both:
//   - Modern DRA-native pods: spec.resourceClaims with memory capacity
//   - Legacy/simple pods: scheduling.kubenexus.io/vram-request annotation
func getVRAMRequest(pod *v1.Pod) int64 {
	// Priority 1: Check DRA ResourceClaims
	if len(pod.Spec.ResourceClaims) > 0 {
		for _, claim := range pod.Spec.ResourceClaims {
			// Check if this is a GPU resource claim
			// Note: The actual VRAM requirement would be in the ResourceClaimTemplate spec
			// For now, we'll check the claim name pattern and extract from template

			// Common patterns: gpu-claim, nvidia-gpu, accelerator
			claimName := claim.Name
			if strings.Contains(strings.ToLower(claimName), "gpu") ||
				strings.Contains(strings.ToLower(claimName), "accelerator") ||
				strings.Contains(strings.ToLower(claimName), "nvidia") {

				// TODO: In full DRA integration, we would:
				// 1. Fetch the ResourceClaim object
				// 2. Get the ResourceClaimTemplate
				// 3. Extract memory requirements from template spec
				//
				// For now, DRA users should also set annotation as a hint
				// This will be enhanced in future versions

				klog.V(4).InfoS("Pod has DRA ResourceClaim, checking annotation for VRAM hint",
					"pod", pod.Name,
					"claim", claimName)
			}
		}
	}

	// Priority 2: Check annotation (fallback and DRA hint)
	if vramStr, ok := pod.Annotations[AnnotationVRAMRequest]; ok {
		// Parse quantity (e.g., "80Gi", "24GB")
		quantity, err := resource.ParseQuantity(vramStr)
		if err != nil {
			klog.V(4).InfoS("Failed to parse VRAM request annotation",
				"pod", pod.Name, "vramRequest", vramStr, "error", err)
			return 0
		}
		vramBytes := quantity.Value()
		klog.V(5).InfoS("Parsed VRAM request from pod annotation",
			"pod", pod.Name, "vramRequest", vramStr, "bytes", vramBytes)
		return vramBytes
	}

	// No VRAM requirement specified
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

// getNodeGPUTopology extracts full GPU topology information from DRA ResourceSlices
// Returns (vramPerGPU, []GPUDevice with full topology)
func (v *VRAMScheduler) getNodeGPUTopology(ctx context.Context, node *v1.Node) (int64, []GPUDevice) {
	if v.clientset == nil {
		klog.V(4).InfoS("No clientset available, falling back to node labels",
			"node", node.Name)
		vramPerGPU, gpuCount := getNodeGPUVRAMFromLabels(node)
		devices := make([]GPUDevice, gpuCount)
		for i := range devices {
			devices[i] = GPUDevice{VRAM: vramPerGPU, NUMANode: -1, NVLinkDomain: -1}
		}
		return vramPerGPU, devices
	}

	// Query ResourceSlices for this node
	resourceSlices, err := v.clientset.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", node.Name),
	})

	if err != nil {
		klog.V(4).InfoS("Failed to list ResourceSlices for node, falling back to labels",
			"node", node.Name, "error", err)
		vramPerGPU, gpuCount := getNodeGPUVRAMFromLabels(node)
		// Return empty GPU devices with VRAM information
		devices := make([]GPUDevice, gpuCount)
		for i := range devices {
			devices[i] = GPUDevice{VRAM: vramPerGPU, NUMANode: -1, NVLinkDomain: -1}
		}
		return vramPerGPU, devices
	}

	if len(resourceSlices.Items) == 0 {
		klog.V(5).InfoS("No ResourceSlices found for node, falling back to labels",
			"node", node.Name)
		vramPerGPU, gpuCount := getNodeGPUVRAMFromLabels(node)
		devices := make([]GPUDevice, gpuCount)
		for i := range devices {
			devices[i] = GPUDevice{VRAM: vramPerGPU, NUMANode: -1, NVLinkDomain: -1}
		}
		return vramPerGPU, devices
	}

	// Extract VRAM and topology from ResourceSlice devices
	var gpuDevices []GPUDevice
	var vramPerGPU int64

	for _, slice := range resourceSlices.Items {
		// Check if this is a GPU driver
		if !isGPUDriver(slice.Spec.Driver) {
			continue
		}

		for _, device := range slice.Spec.Devices {
			gpu := GPUDevice{
				Name:         device.Name,
				NUMANode:     -1, // Default: unknown
				NVLinkDomain: -1,
			}

			// Extract VRAM capacity
			if device.Capacity != nil {
				for resourceName, deviceCapacity := range device.Capacity {
					if strings.Contains(strings.ToLower(string(resourceName)), "memory") {
						gpu.VRAM = deviceCapacity.Value.Value()
						if vramPerGPU == 0 || gpu.VRAM < vramPerGPU {
							vramPerGPU = gpu.VRAM
						}
						break
					}
				}
			}

			// Extract topology attributes from DRA
			if device.Attributes != nil {
				// GPU-to-NUMA affinity
				if numaAttr, exists := device.Attributes["numa-node"]; exists && numaAttr.IntValue != nil {
					gpu.NUMANode = int(*numaAttr.IntValue)
				}

				// PCIe topology
				if pcieAttr, exists := device.Attributes["pcie-bus-id"]; exists && pcieAttr.StringValue != nil {
					gpu.PCIeBusID = *pcieAttr.StringValue
				}
				if switchAttr, exists := device.Attributes["pcie-switch"]; exists && switchAttr.StringValue != nil {
					gpu.PCIeSwitch = *switchAttr.StringValue
				}

				// NVLink topology
				if peersAttr, exists := device.Attributes["nvlink-peers"]; exists && peersAttr.StringValue != nil {
					peerList := strings.Split(*peersAttr.StringValue, ",")
					for _, peer := range peerList {
						peer = strings.TrimSpace(peer)
						if peer != "" {
							gpu.NVLinkPeers = append(gpu.NVLinkPeers, peer)
						}
					}
				}
				if domainAttr, exists := device.Attributes["nvlink-domain"]; exists && domainAttr.IntValue != nil {
					gpu.NVLinkDomain = int(*domainAttr.IntValue)
				}
			}

			if gpu.VRAM > 0 {
				gpuDevices = append(gpuDevices, gpu)
				klog.V(5).InfoS("Discovered GPU with topology",
					"node", node.Name,
					"gpu", gpu.Name,
					"vram", formatBytes(gpu.VRAM),
					"numaNode", gpu.NUMANode,
					"nvlinkPeers", len(gpu.NVLinkPeers),
					"pcieSwitch", gpu.PCIeSwitch)
			}
		}
	}

	if len(gpuDevices) > 0 {
		klog.V(4).InfoS("Extracted GPU topology from DRA ResourceSlices",
			"node", node.Name,
			"gpuCount", len(gpuDevices),
			"vramPerGPU", formatBytes(vramPerGPU))
		return vramPerGPU, gpuDevices
	}

	// No GPU info in ResourceSlices, fall back to node labels
	klog.V(5).InfoS("No GPU topology found in ResourceSlices, falling back to labels",
		"node", node.Name)
	vramPerGPU, gpuCount := getNodeGPUVRAMFromLabels(node)
	devices := make([]GPUDevice, gpuCount)
	for i := range devices {
		devices[i] = GPUDevice{VRAM: vramPerGPU, NUMANode: -1, NVLinkDomain: -1}
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

	return &VRAMScheduler{
		handle:    handle,
		clientset: clientset,
	}, nil
}
