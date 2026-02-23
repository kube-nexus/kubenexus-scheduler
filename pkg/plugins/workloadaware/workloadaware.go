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

// Package workloadaware implements workload-aware scoring for intelligent pod placement.
package workloadaware

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corelisters "k8s.io/client-go/listers/core/v1"
	klog "k8s.io/klog/v2"
	framework "k8s.io/kube-scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/plugins/profileclassifier"
	"sigs.k8s.io/scheduler-plugins/pkg/workload"
)

// WorkloadAware implements workload-aware scoring:
// - Batch workloads: Bin packing (prefer fuller nodes for co-location)
// - Service workloads: Spreading (prefer emptier nodes for HA)
type WorkloadAware struct {
	handle    framework.Handle
	podLister corelisters.PodLister
}

var _ framework.ScorePlugin = &WorkloadAware{}

const (
	// Name is the name of the plugin used in the plugin registry and configurations.
	Name = "WorkloadAwareScoring"

	// MaxNodeScore is the maximum score a node can get.
	MaxNodeScore = framework.MaxNodeScore
)

// Name returns the name of the plugin.
func (w *WorkloadAware) Name() string {
	return Name
}

// Score invoked at the score extension point.
func (w *WorkloadAware) Score(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfo framework.NodeInfo) (int64, *framework.Status) {
	// Try to get workload type from ProfileClassifier (preferred)
	workloadType := w.getWorkloadTypeFromProfile(state, pod)

	// Calculate node utilization (0-100%)
	utilization := w.calculateNodeUtilization(nodeInfo)

	var score int64
	switch workloadType {
	case workload.TypeBatch, "batch", "training":
		// Batch/Training: Bin packing - prefer fuller nodes
		// Higher utilization = higher score
		// Goal: Co-locate batch pods on same nodes to:
		// 1. Reduce network latency (ML training, Spark shuffles)
		// 2. Leave empty nodes for services
		// 3. Maximize resource utilization
		score = int64(utilization)

	case workload.TypeService, "service", "inference":
		// Service/Inference: Spreading - prefer emptier nodes
		// Lower utilization = higher score
		// Goal: Distribute services across nodes for:
		// 1. High availability (no single point of failure)
		// 2. Fault tolerance (node failures)
		// 3. Even load distribution
		score = int64(100 - utilization)

	default:
		// Unknown workload type: default to spreading
		score = int64(100 - utilization)
	}

	return score, framework.NewStatus(framework.Success, "")
}

// getWorkloadTypeFromProfile tries to get workload type from ProfileClassifier,
// falls back to local classification if ProfileClassifier is not enabled
func (w *WorkloadAware) getWorkloadTypeFromProfile(state framework.CycleState, pod *v1.Pod) interface{} {
	// Try to get profile from ProfileClassifier (preferred)
	profile, err := profileclassifier.GetProfile(&state)
	if err == nil && profile != nil {
		// Map ProfileClassifier workload types to our string representation
		workloadTypeStr := string(profile.WorkloadType)
		klog.V(4).InfoS("Using workload classification from ProfileClassifier",
			"pod", klog.KObj(pod),
			"workloadType", profile.WorkloadType,
			"tenantTier", profile.TenantTier)
		return workloadTypeStr
	}

	// Fall back to local classification for backward compatibility
	klog.V(5).InfoS("ProfileClassifier not available, using local workload classification",
		"pod", klog.KObj(pod))
	return workload.ClassifyPod(pod)
}

// ScoreExtensions returns a ScoreExtensions interface if it implements one, or nil if not.
func (w *WorkloadAware) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

// calculateNodeUtilization returns the node's resource utilization as a percentage (0-100).
// Considers both CPU and memory, weighted equally.
func (w *WorkloadAware) calculateNodeUtilization(nodeInfo framework.NodeInfo) float64 {
	node := nodeInfo.Node()
	if node == nil {
		return 0
	}

	// Get node allocatable resources
	allocatableCPU := float64(node.Status.Allocatable.Cpu().MilliValue())
	allocatableMemory := float64(node.Status.Allocatable.Memory().Value())

	if allocatableCPU == 0 || allocatableMemory == 0 {
		return 0
	}

	// Get all pods from the lister and filter by this node
	allPods, err := w.podLister.List(nil)
	if err != nil {
		return 50.0 // Conservative default
	}

	requestedCPU := float64(0)
	requestedMemory := float64(0)

	// Only sum pods that are scheduled on THIS specific node
	for _, pod := range allPods {
		if pod.Spec.NodeName == node.Name {
			for _, container := range pod.Spec.Containers {
				requestedCPU += float64(container.Resources.Requests.Cpu().MilliValue())
				requestedMemory += float64(container.Resources.Requests.Memory().Value())
			}
		}
	}

	// Calculate utilization percentages
	cpuUtilization := (requestedCPU / allocatableCPU) * 100.0
	memoryUtilization := (requestedMemory / allocatableMemory) * 100.0

	// Return weighted average (50% CPU, 50% Memory)
	// Cap at 100% to handle overcommitted nodes
	utilization := (cpuUtilization + memoryUtilization) / 2.0
	if utilization > 100.0 {
		utilization = 100.0
	}

	return utilization
}

// New initializes a new plugin and returns it.
func New(_ context.Context, _ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	podLister := handle.SharedInformerFactory().Core().V1().Pods().Lister()

	return &WorkloadAware{
		handle:    handle,
		podLister: podLister,
	}, nil
}
