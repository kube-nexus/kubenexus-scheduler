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

package vramscheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/kube-scheduler/framework"
)

const (
	// Name is the plugin name
	Name = "VRAMScheduler"

	// Pod annotations for VRAM requirements
	AnnotationVRAMRequest = "scheduling.kubenexus.io/vram-request" // e.g., "80Gi", "24Gi"
	AnnotationModelSize   = "scheduling.kubenexus.io/model-size"   // e.g., "70B", "7B" (informational)

	// Node labels for GPU VRAM capacity (per-GPU)
	LabelGPUVRAM      = "gpu.kubenexus.io/vram"       // e.g., "80Gi", "40Gi", "24Gi"
	LabelGPUModel     = "gpu.kubenexus.io/model"      // e.g., "H100", "A100-80GB", "L40S"
	LabelGPUCount     = "gpu.kubenexus.io/count"      // Total GPUs on node
	LabelGPUTopology  = "topology.kubenexus.io/gpu"   // e.g., "nvswitch", "nvlink"

	// NodeResourceTopology zones for per-GPU VRAM
	NRTZoneGPUPrefix = "gpu-"  // e.g., "gpu-0", "gpu-1"
	NRTResourceVRAM  = "nvidia.com/gpu-memory" // VRAM resource in NRT

	// Scoring constants
	ScorePerfectFit        = 100  // VRAM request matches GPU capacity exactly
	ScoreGoodFit           = 80   // VRAM request is 70-95% of GPU capacity
	ScoreAcceptableFit     = 60   // VRAM request is 50-70% of GPU capacity
	ScorePoorFit           = 30   // VRAM request is 30-50% of GPU capacity (stranding VRAM)
	ScoreInsufficientVRAM  = 0    // VRAM is insufficient
	
	// Fit thresholds (percentage of GPU VRAM)
	ThresholdPerfectFit    = 0.95  // 95-100% utilization
	ThresholdGoodFit       = 0.70  // 70-95% utilization
	ThresholdAcceptableFit = 0.50  // 50-70% utilization
	ThresholdPoorFit       = 0.30  // 30-50% utilization

	// Bonus/Penalty adjustments
	BonusHighEndGPU   = 10   // Bonus for scheduling on premium GPUs (H100, A100-80GB)
	PenaltyStrandVRAM = 20   // Penalty for stranding >50% of VRAM
)

// VRAMScheduler implements VRAM-aware scheduling to prevent OOM and optimize VRAM utilization
type VRAMScheduler struct {
	handle framework.Handle
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

	// Get GPU VRAM capacity from node
	gpuVRAM, gpuCount := getNodeGPUVRAM(node)
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
		score := calculateUtilizationScore(utilizationRatio)
		
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
	score := calculateUtilizationScore(utilizationRatio)

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

	// Get GPU VRAM capacity from node
	gpuVRAM, _ := getNodeGPUVRAM(node)
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

// calculateUtilizationScore calculates score based on VRAM utilization ratio
func calculateUtilizationScore(utilizationRatio float64) int64 {
	if utilizationRatio >= ThresholdPerfectFit {
		return ScorePerfectFit
	}
	if utilizationRatio >= ThresholdGoodFit {
		return ScoreGoodFit
	}
	if utilizationRatio >= ThresholdAcceptableFit {
		return ScoreAcceptableFit
	}
	if utilizationRatio >= ThresholdPoorFit {
		return ScorePoorFit
	}
	return ScoreInsufficientVRAM
}

// getVRAMRequest extracts VRAM request from pod annotations (in bytes)
func getVRAMRequest(pod *v1.Pod) int64 {
	if vramStr, ok := pod.Annotations[AnnotationVRAMRequest]; ok {
		// Parse quantity (e.g., "80Gi", "24GB")
		quantity, err := resource.ParseQuantity(vramStr)
		if err != nil {
			klog.V(4).InfoS("Failed to parse VRAM request annotation",
				"pod", pod.Name, "vramRequest", vramStr, "error", err)
			return 0
		}
		vramBytes := quantity.Value()
		klog.V(5).InfoS("Parsed VRAM request from pod",
			"pod", pod.Name, "vramRequest", vramStr, "bytes", vramBytes)
		return vramBytes
	}
	return 0
}

// getNodeGPUVRAM extracts GPU VRAM capacity from node labels (returns per-GPU VRAM in bytes and total GPU count)
func getNodeGPUVRAM(node *v1.Node) (int64, int) {
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
		{"L40S", 48 * 1024 * 1024 * 1024},  // Check L40S before L40
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
	klog.V(3).InfoS("Creating new VRAMScheduler plugin")
	return &VRAMScheduler{
		handle: handle,
	}, nil
}
