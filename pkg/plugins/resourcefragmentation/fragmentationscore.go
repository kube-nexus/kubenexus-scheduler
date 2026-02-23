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

// Package resourcefragmentation implements a scheduler plugin that minimizes resource fragmentation
// by scoring nodes based on how well they pack resources, preferring nodes that maintain contiguous
// resource blocks (islands) for better GPU interconnect performance.
package resourcefragmentation

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corelisters "k8s.io/client-go/listers/core/v1"
	klog "k8s.io/klog/v2"
	framework "k8s.io/kube-scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/plugins/profileclassifier"
)

const (
	Name = "ResourceFragmentationScore"

	ResourceGPU    = "nvidia.com/gpu"
	ResourceCPU    = v1.ResourceCPU
	ResourceMemory = v1.ResourceMemory

	LabelGPUTopology    = "gpu.kubenexus.io/topology"
	LabelGPUModel       = "gpu.kubenexus.io/model"
	LabelGPUIsPristine  = "gpu.kubenexus.io/is-pristine"
	LabelNodeTenantTier = "tenant.kubenexus.io/reserved-tier" // Node reserved for specific tenant tier

	IslandQualityNVSwitch = 100
	IslandQualityNVLink   = 80
	IslandQualityPCIe     = 50
	IslandQualityUnknown  = 30

	PenaltyFragmentPristineIsland = 0
	PenaltyFragmentLargeIsland    = 20
	PenaltyTenantMismatch         = 10 // Lower tier tenant trying to use higher tier island
	BonusCompleteIsland           = 100
	BonusPerfectFit               = 90

	LargeIslandThreshold  = 4
	SmallRequestThreshold = 2
)

type ResourceFragmentationScore struct {
	handle    framework.Handle
	podLister corelisters.PodLister
}

var _ framework.ScorePlugin = &ResourceFragmentationScore{}

type GPUIsland struct {
	NodeName      string
	TotalGPUs     int
	AvailableGPUs int
	AllocatedGPUs int
	Topology      string
	GPUModel      string
	Quality       int
	IsPristine    bool
	TenantTier    string // Reserved tenant tier (gold/silver/bronze)
}

func (rf *ResourceFragmentationScore) Name() string {
	return Name
}

func (rf *ResourceFragmentationScore) Score(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfo framework.NodeInfo) (int64, *framework.Status) {
	island := rf.detectGPUIsland(nodeInfo)
	if island == nil {
		return rf.scoreCPUMemoryFragmentation(pod, nodeInfo), framework.NewStatus(framework.Success)
	}

	requestedGPUs := getGPURequest(pod)
	if requestedGPUs == 0 {
		return rf.scoreCPUMemoryFragmentation(pod, nodeInfo), framework.NewStatus(framework.Success)
	}

	// Get pod's tenant tier from ProfileClassifier
	podTenantTier := rf.getPodTenantTier(state, pod)

	// TENANT-AWARE ISLAND PROTECTION:
	// Prevent lower-tier tenants from fragmenting higher-tier tenant islands
	if island.TenantTier != "" && island.TenantTier != "none" {
		if !rf.isTenantAllowed(podTenantTier, island.TenantTier) {
			klog.V(3).InfoS("Preventing tenant mismatch fragmentation",
				"pod", klog.KObj(pod),
				"podTenantTier", podTenantTier,
				"node", nodeInfo.Node().Name,
				"nodeTenantTier", island.TenantTier,
				"islandSize", island.TotalGPUs)
			return PenaltyTenantMismatch, framework.NewStatus(framework.Success)
		}
	}

	// PRISTINE ISLAND PROTECTION:
	// Prevent small requests from fragmenting large pristine islands
	if island.IsPristine && island.TotalGPUs >= LargeIslandThreshold && requestedGPUs <= SmallRequestThreshold {
		klog.V(4).InfoS("Preventing pristine island fragmentation",
			"pod", pod.Name,
			"node", nodeInfo.Node().Name,
			"islandSize", island.TotalGPUs,
			"requestSize", requestedGPUs)
		return PenaltyFragmentPristineIsland, framework.NewStatus(framework.Success)
	}

	if island.AvailableGPUs == requestedGPUs {
		klog.V(4).InfoS("Perfect fit bonus",
			"pod", pod.Name,
			"node", nodeInfo.Node().Name)
		return BonusPerfectFit, framework.NewStatus(framework.Success)
	}

	if !island.IsPristine && island.AvailableGPUs >= requestedGPUs {
		completionScore := BonusCompleteIsland - int64(island.AvailableGPUs-requestedGPUs)
		return completionScore, framework.NewStatus(framework.Success)
	}

	if island.TotalGPUs >= LargeIslandThreshold && requestedGPUs < island.TotalGPUs/2 {
		penalty := PenaltyFragmentLargeIsland + int64(island.Quality)/10
		return penalty, framework.NewStatus(framework.Success)
	}

	utilizationScore := (island.AllocatedGPUs * 100) / island.TotalGPUs
	return int64(utilizationScore), framework.NewStatus(framework.Success)
}

// getPodTenantTier gets the pod's tenant tier from ProfileClassifier or defaults to unknown
func (rf *ResourceFragmentationScore) getPodTenantTier(state framework.CycleState, pod *v1.Pod) string {
	profile, err := profileclassifier.GetProfile(&state)
	if err == nil && profile != nil {
		return string(profile.TenantTier)
	}
	// Default to bronze (lowest tier) if ProfileClassifier not available
	return "bronze"
}

// isTenantAllowed checks if a pod's tenant tier can use a node reserved for a specific tier
// Tenant hierarchy: gold > silver > bronze
// Gold tenants can use any island, silver can use silver/bronze, bronze only bronze
func (rf *ResourceFragmentationScore) isTenantAllowed(podTier string, nodeTier string) bool {
	// Map tier priority: gold=3, silver=2, bronze=1, unknown=1
	tierPriority := map[string]int{
		"gold":    3,
		"silver":  2,
		"bronze":  1,
		"unknown": 1,
	}

	podPriority := tierPriority[podTier]
	nodePriority := tierPriority[nodeTier]

	// Pod can use node if its tier is >= node's tier
	return podPriority >= nodePriority
}

func (rf *ResourceFragmentationScore) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

func (rf *ResourceFragmentationScore) detectGPUIsland(nodeInfo framework.NodeInfo) *GPUIsland {
	node := nodeInfo.Node()
	if node == nil {
		return nil
	}

	totalGPUs, hasGPU := node.Status.Capacity[ResourceGPU]
	if !hasGPU || totalGPUs.IsZero() {
		return nil
	}

	allocatedGPUs := int64(0)
	// podLister might be nil in test scenarios
	if rf.podLister != nil {
		allPods, err := rf.podLister.List(nil)
		if err == nil {
			for _, pod := range allPods {
				if pod.Spec.NodeName == node.Name {
					for _, container := range pod.Spec.Containers {
						if gpuReq, ok := container.Resources.Requests[ResourceGPU]; ok {
							allocatedGPUs += gpuReq.Value()
						}
					}
				}
			}
		}
	}

	totalGPUCount := int(totalGPUs.Value())
	allocatedGPUCount := int(allocatedGPUs)
	availableGPUCount := totalGPUCount - allocatedGPUCount

	topology := "unknown"
	if val, ok := node.Labels[LabelGPUTopology]; ok {
		topology = val
	}

	gpuModel := "unknown"
	if val, ok := node.Labels[LabelGPUModel]; ok {
		gpuModel = val
	}

	quality := IslandQualityUnknown
	switch topology {
	case "nvswitch":
		quality = IslandQualityNVSwitch
	case "nvlink":
		quality = IslandQualityNVLink
	case "pcie":
		quality = IslandQualityPCIe
	}

	isPristine := allocatedGPUCount == 0
	if val, ok := node.Labels[LabelGPUIsPristine]; ok && val == "true" {
		isPristine = true
	}

	// Check if node is reserved for a specific tenant tier
	tenantTier := ""
	if val, ok := node.Labels[LabelNodeTenantTier]; ok {
		tenantTier = val
	}

	return &GPUIsland{
		NodeName:      node.Name,
		TotalGPUs:     totalGPUCount,
		AvailableGPUs: availableGPUCount,
		AllocatedGPUs: allocatedGPUCount,
		Topology:      topology,
		GPUModel:      gpuModel,
		Quality:       quality,
		IsPristine:    isPristine,
		TenantTier:    tenantTier,
	}
}

func (rf *ResourceFragmentationScore) scoreCPUMemoryFragmentation(pod *v1.Pod, nodeInfo framework.NodeInfo) int64 {
	node := nodeInfo.Node()
	if node == nil {
		return 50
	}

	allocatableCPU := float64(node.Status.Allocatable.Cpu().MilliValue())
	requestedCPU := float64(0)

	// podLister might be nil in test scenarios
	if rf.podLister != nil {
		allPods, err := rf.podLister.List(nil)
		if err == nil {
			for _, p := range allPods {
				if p.Spec.NodeName == node.Name {
					for _, container := range p.Spec.Containers {
						requestedCPU += float64(container.Resources.Requests.Cpu().MilliValue())
					}
				}
			}
		}
	}

	cpuUtilization := 0.0
	if allocatableCPU > 0 {
		cpuUtilization = (requestedCPU / allocatableCPU) * 100
	}

	return int64(cpuUtilization)
}

func getGPURequest(pod *v1.Pod) int {
	totalGPUs := 0
	for _, container := range pod.Spec.Containers {
		if gpuReq, ok := container.Resources.Requests[ResourceGPU]; ok {
			totalGPUs += int(gpuReq.Value())
		}
	}
	return totalGPUs
}

func New(_ context.Context, _ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	podLister := handle.SharedInformerFactory().Core().V1().Pods().Lister()

	return &ResourceFragmentationScore{
		handle:    handle,
		podLister: podLister,
	}, nil
}
