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
// Package tenanthardware implements a scheduler plugin for tenant-hardware affinity scoring,
// matching tenant priority levels (high/medium/low) to hardware tiers (premium/standard/economy)
// to optimize cost-efficiency and prevent resource waste.// Package tenanthardware implements tenant-to-hardware affinity scoring for economic efficiency.
//
// THE ECONOMIC PROBLEM:
// In heterogeneous GPU clusters (H100, A100, L40), the native scheduler treats "a GPU is a GPU".
// Low-priority pods may consume expensive H100 nodes when cheaper A100 nodes would suffice,
// leaving no premium hardware for high-priority workloads when they arrive.
//
// THE SOLUTION:
// Match tenant priority classes to hardware tiers:
//   - High-priority tenants → Premium hardware (H100, H200)
//   - Medium-priority tenants → Standard hardware (A100, A100X)
//   - Low-priority tenants → Economy hardware (L40, T4)
//
// INTEGRATION WITH KUEUE:
// Can optionally watch Kueue LocalQueues to see which tenants are in which queues,
// enabling even smarter placement based on upcoming demand.

package tenanthardware

import (
	"context"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	klog "k8s.io/klog/v2"
	framework "k8s.io/kube-scheduler/framework"

	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/profileclassifier"
)

const (
	Name = "TenantHardwareAffinityScore"

	// Node labels for hardware tier classification
	LabelHardwareTier  = "hardware.kubenexus.io/tier"      // "premium", "standard", "economy"
	LabelGPUModel      = "gpu.kubenexus.io/model"          // "H100", "A100", "L40"
	LabelGPUGeneration = "hardware.kubenexus.io/gpu-gen"   // "hopper", "ampere", "ada"
	LabelCostPerHour   = "hardware.kubenexus.io/cost-hour" // "10.00", "5.00", "2.00"

	// Priority class tiers (standard K8s)
	PriorityHigh   = "high-priority"
	PriorityMedium = "medium-priority"
	PriorityLow    = "low-priority"

	// Hardware tiers
	TierPremium  = "premium"  // H100, H200, latest generation
	TierStandard = "standard" // A100, A100X, proven workhorses
	TierEconomy  = "economy"  // L40, T4, cost-effective

	// Scoring weights
	ScorePerfectMatch    = 100 // Tenant priority matches hardware tier exactly
	ScoreAcceptableMatch = 70  // Tenant can use this tier (not ideal but OK)
	ScoreMismatchPenalty = 20  // Heavy penalty for wrong tier
	ScoreNoHardwareInfo  = 50  // Neutral score when no tier info available
)

type TenantHardwareAffinity struct {
	handle framework.Handle
}

// HardwareTier represents a classification of hardware
type HardwareTier struct {
	Name       string
	GPUModels  []string
	CostFactor float64
}

var _ framework.ScorePlugin = &TenantHardwareAffinity{}

func (tha *TenantHardwareAffinity) Name() string {
	return Name
}

func (tha *TenantHardwareAffinity) Score(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfo framework.NodeInfo) (int64, *framework.Status) {
	node := nodeInfo.Node()
	if node == nil {
		return ScoreNoHardwareInfo, framework.NewStatus(framework.Success)
	}

	// 1. Determine pod's tenant priority
	// Try ProfileClassifier first (preferred), fall back to local classification
	tenantPriority := tha.getTenantPriorityFromProfile(state, pod)

	// 2. Determine node's hardware tier
	hardwareTier := tha.getHardwareTier(node)

	// 3. Calculate match score
	score := tha.calculateAffinityScore(tenantPriority, hardwareTier, node)

	klog.V(5).InfoS("Tenant-hardware affinity scoring",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"tenantPriority", tenantPriority,
		"node", node.Name,
		"hardwareTier", hardwareTier,
		"score", score)

	return score, framework.NewStatus(framework.Success)
}

func (tha *TenantHardwareAffinity) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

// getTenantPriorityFromProfile tries to get tenant classification from ProfileClassifier,
// falls back to local classification if ProfileClassifier is not enabled
func (tha *TenantHardwareAffinity) getTenantPriorityFromProfile(state framework.CycleState, pod *v1.Pod) string {
	// Try to get profile from ProfileClassifier (preferred)
	profile, err := profileclassifier.GetProfile(&state)
	if err == nil && profile != nil {
		// Map ProfileClassifier tenant tiers to our priority levels
		priority := tha.mapTenantTierToPriority(profile.TenantTier)
		klog.V(4).InfoS("Using tenant classification from ProfileClassifier",
			"pod", pod.Name,
			"namespace", pod.Namespace,
			"tenantTier", profile.TenantTier,
			"tenantName", profile.TenantName,
			"mappedPriority", priority)
		return priority
	}

	// Fall back to local classification for backward compatibility
	klog.V(5).InfoS("ProfileClassifier not available, using local tenant classification",
		"pod", pod.Name,
		"namespace", pod.Namespace)
	return tha.getTenantPriority(pod)
}

// mapTenantTierToPriority maps ProfileClassifier tenant tiers to priority levels
func (tha *TenantHardwareAffinity) mapTenantTierToPriority(tier profileclassifier.TenantTier) string {
	switch tier {
	case profileclassifier.TierGold:
		return PriorityHigh
	case profileclassifier.TierSilver:
		return PriorityMedium
	case profileclassifier.TierBronze:
		return PriorityLow
	default:
		return PriorityMedium
	}
}

// getTenantPriority determines the priority tier of a pod/tenant (fallback method)
func (tha *TenantHardwareAffinity) getTenantPriority(pod *v1.Pod) string {
	// Check pod annotations for explicit priority override (highest priority)
	if priority, ok := pod.Annotations["scheduling.kubenexus.io/priority-tier"]; ok {
		return priority
	}

	// Check explicit priority class
	if pod.Spec.PriorityClassName != "" {
		// Map common priority class patterns
		priorityClass := strings.ToLower(pod.Spec.PriorityClassName)
		if strings.Contains(priorityClass, "high") || strings.Contains(priorityClass, "critical") {
			return PriorityHigh
		}
		if strings.Contains(priorityClass, "medium") || strings.Contains(priorityClass, "normal") {
			return PriorityMedium
		}
		if strings.Contains(priorityClass, "low") || strings.Contains(priorityClass, "best-effort") {
			return PriorityLow
		}
	}

	// Default to medium priority if no explicit classification found
	// Cluster admins should use annotations or priority classes for explicit control
	return PriorityMedium
}

// getHardwareTier determines the hardware tier of a node
func (tha *TenantHardwareAffinity) getHardwareTier(node *v1.Node) string {
	// Check explicit tier label
	if tier, ok := node.Labels[LabelHardwareTier]; ok {
		return tier
	}

	// Infer from GPU model
	if gpuModel, ok := node.Labels[LabelGPUModel]; ok {
		return tha.inferTierFromGPUModel(gpuModel)
	}

	// No tier information
	return ""
}

// inferTierFromGPUModel infers hardware tier from GPU model name
func (tha *TenantHardwareAffinity) inferTierFromGPUModel(gpuModel string) string {
	model := strings.ToUpper(gpuModel)

	// Premium tier: Latest generation high-end GPUs
	premiumModels := []string{"H100", "H200", "A100-80GB", "MI300"}
	for _, premium := range premiumModels {
		if strings.Contains(model, premium) {
			return TierPremium
		}
	}

	// Standard tier: Previous generation high-end or current mid-range
	standardModels := []string{"A100", "A40", "A6000", "MI250"}
	for _, standard := range standardModels {
		if strings.Contains(model, standard) {
			return TierStandard
		}
	}

	// Economy tier: Cost-effective GPUs
	economyModels := []string{"L40", "L4", "T4", "A10", "A16"}
	for _, economy := range economyModels {
		if strings.Contains(model, economy) {
			return TierEconomy
		}
	}

	// Unknown GPU model
	return ""
}

// calculateAffinityScore calculates the affinity score based on tenant-hardware match
func (tha *TenantHardwareAffinity) calculateAffinityScore(tenantPriority, hardwareTier string, node *v1.Node) int64 {
	// If no hardware tier info, return neutral score
	if hardwareTier == "" {
		return ScoreNoHardwareInfo
	}

	// Perfect match matrix
	perfectMatches := map[string]string{
		PriorityHigh:   TierPremium,
		PriorityMedium: TierStandard,
		PriorityLow:    TierEconomy,
	}

	// Check for perfect match
	if perfectMatches[tenantPriority] == hardwareTier {
		klog.V(4).InfoS("Perfect tenant-hardware match",
			"tenantPriority", tenantPriority,
			"hardwareTier", hardwareTier,
			"node", node.Name)
		return ScorePerfectMatch
	}

	// Acceptable matches (can use but not ideal)

	// High-priority can use any tier (but prefers premium)
	if tenantPriority == PriorityHigh {
		if hardwareTier == TierStandard {
			return ScoreAcceptableMatch
		}
		if hardwareTier == TierEconomy {
			return ScoreAcceptableMatch - 10 // Slightly worse
		}
	}

	// Medium-priority can use standard or economy (but not premium)
	if tenantPriority == PriorityMedium {
		if hardwareTier == TierEconomy {
			return ScoreAcceptableMatch
		}
		// Heavy penalty: Don't waste premium on medium-priority
		if hardwareTier == TierPremium {
			klog.V(4).InfoS("Penalizing premium hardware for medium-priority tenant",
				"tenantPriority", tenantPriority,
				"hardwareTier", hardwareTier,
				"node", node.Name)
			return ScoreMismatchPenalty
		}
	}

	// Low-priority should NOT use premium or standard
	if tenantPriority == PriorityLow {
		// Very heavy penalty: Preserve better hardware for higher-priority work
		if hardwareTier == TierPremium || hardwareTier == TierStandard {
			klog.V(4).InfoS("Penalizing premium/standard hardware for low-priority tenant",
				"tenantPriority", tenantPriority,
				"hardwareTier", hardwareTier,
				"node", node.Name)
			return ScoreMismatchPenalty
		}
	}

	// Default: acceptable but not perfect
	return ScoreAcceptableMatch
}

func New(_ context.Context, _ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &TenantHardwareAffinity{
		handle: handle,
	}, nil
}
