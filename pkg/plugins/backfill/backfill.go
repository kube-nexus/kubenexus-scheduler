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

// Package backfill implements backfilling scheduling plugin.
package backfill

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corelisters "k8s.io/client-go/listers/core/v1"
	klog "k8s.io/klog/v2"
	framework "k8s.io/kube-scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/plugins/profileclassifier"
)

// BackfillScoring implements opportunistic scheduling to maximize cluster utilization
// by allowing low-priority "backfill" pods to use idle resources that would otherwise
// be wasted.
//
// PROBLEM:
// Consider a cluster with 100 CPUs where a large ML job needs all 100 CPUs but won't
// arrive for another hour. Without backfill:
//   - 50 CPUs in use by regular workloads
//   - 50 CPUs sitting IDLE (wasted capacity!)
//
// SOLUTION:
// Backfill scheduling allows "interruptible" or "low-priority" pods to use those
// 50 idle CPUs. When the large ML job arrives, the GangPreemption plugin will
// evict these backfill pods to make room.
//
// BENEFITS:
//  1. Better resource utilization - no wasted capacity
//  2. Faster processing of low-priority batch jobs
//  3. No impact on high-priority workloads (they can preempt backfill pods)
//  4. Works seamlessly with existing GangPreemption plugin
//
// HOW IT WORKS:
//   - Identifies pods marked as "backfill eligible" (via PriorityClass or labels)
//   - Scores nodes with MORE idle resources HIGHER for backfill pods
//   - Scores nodes with LESS idle resources HIGHER for regular pods
//   - Result: Backfill pods naturally fill gaps in cluster capacity
//
// EXAMPLE:
//
//	Node A: 80% utilized (20% idle)
//	Node B: 20% utilized (80% idle)
//
//	Regular Pod:
//	  - Node A: score = 80 (prefer fuller nodes for co-location)
//	  - Node B: score = 20
//
//	Backfill Pod:
//	  - Node A: score = 20 (avoid disrupting existing workloads)
//	  - Node B: score = 80 (use the idle capacity!)
type BackfillScoring struct {
	handle    framework.Handle
	podLister corelisters.PodLister
}

var _ framework.ScorePlugin = &BackfillScoring{}

const (
	// Name is the name of the plugin used in the plugin registry and configurations.
	Name = "BackfillScoring"

	// BackfillPriorityThreshold defines the maximum priority for a pod to be considered
	// a backfill candidate. Pods with priority <= this value will be scored for backfill.
	// Standard K8s priorities:
	//   - System critical: 2000000000
	//   - User high priority: 1000
	//   - User normal: 0
	//   - Backfill/preemptible: 100 or lower
	BackfillPriorityThreshold = 100

	// BackfillLabelKey is the label key to explicitly mark a pod as backfill-eligible.
	// This provides an alternative to using PriorityClass.
	// Usage: scheduling.kubenexus.io/backfill: "true"
	BackfillLabelKey = "scheduling.kubenexus.io/backfill"

	// MaxNodeScore is the maximum score a node can receive.
	MaxNodeScore = framework.MaxNodeScore
)

// Name returns the name of the plugin.
func (b *BackfillScoring) Name() string {
	return Name
}

// Score invoked at the score extension point.
//
// Scoring strategy:
//   - Backfill pods: Prefer nodes with MORE idle resources (fill the gaps)
//   - Regular pods: Prefer nodes with LESS idle resources (pack efficiently)
//
// This creates a natural separation where backfill pods use underutilized nodes
// while regular workloads pack onto fewer nodes for better efficiency.
func (b *BackfillScoring) Score(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfo framework.NodeInfo) (int64, *framework.Status) {
	node := nodeInfo.Node()
	if node == nil {
		return 0, framework.NewStatus(framework.Error, "node is nil")
	}

	// Calculate node resource utilization
	allocatableCPU := float64(node.Status.Allocatable.Cpu().MilliValue())
	allocatableMemory := float64(node.Status.Allocatable.Memory().Value())

	if allocatableCPU == 0 || allocatableMemory == 0 {
		// Node has no allocatable resources, return neutral score
		return MaxNodeScore / 2, framework.NewStatus(framework.Success, "")
	}

	// Get currently requested resources on the node
	// Sum up all pod requests on this node
	allPods, err := b.podLister.List(nil)
	if err != nil {
		// On error, return neutral score
		return MaxNodeScore / 2, framework.NewStatus(framework.Success, "")
	}

	requestedCPU := float64(0)
	requestedMemory := float64(0)

	// Only sum pods that are scheduled on THIS specific node
	for _, podOnNode := range allPods {
		if podOnNode.Spec.NodeName == node.Name {
			for _, container := range podOnNode.Spec.Containers {
				requestedCPU += float64(container.Resources.Requests.Cpu().MilliValue())
				requestedMemory += float64(container.Resources.Requests.Memory().Value())
			}
		}
	}

	// Calculate utilization percentages (0-100)
	cpuUtilization := (requestedCPU / allocatableCPU) * 100.0
	memoryUtilization := (requestedMemory / allocatableMemory) * 100.0

	// Weighted average: 60% CPU, 40% Memory (CPU is typically more constrained)
	utilization := (cpuUtilization * 0.6) + (memoryUtilization * 0.4)

	// Cap at 100% to handle overcommitted nodes
	if utilization > 100.0 {
		utilization = 100.0
	}

	// Calculate idle percentage
	idlePercent := 100.0 - utilization

	// Determine if this is a backfill pod
	isBackfillPod := b.getPreemptibilityFromProfile(state, pod)

	var score int64

	if isBackfillPod {
		// BACKFILL POD STRATEGY: Prefer nodes with MORE idle resources
		// Score = idle% (0-100)
		//
		// Rationale:
		//   - These pods are interruptible/low-priority
		//   - They should use "wasted" capacity
		//   - Prefer underutilized nodes to avoid impacting regular workloads
		//   - When high-priority workloads arrive, they'll be preempted anyway
		//
		// Example:
		//   Node with 80% idle → score = 80 (high, preferred!)
		//   Node with 20% idle → score = 20 (low, avoid)
		score = int64(idlePercent)

		klog.V(5).Infof("BackfillScoring: backfill pod %s/%s on node %s (util=%.1f%%, idle=%.1f%%, score=%d)",
			pod.Namespace, pod.Name, node.Name, utilization, idlePercent, score)

	} else {
		// REGULAR POD STRATEGY: Prefer nodes with LESS idle resources (bin packing)
		// Score = utilization% (0-100)
		//
		// Rationale:
		//   - Regular pods should pack onto fewer nodes
		//   - Reduces network latency for co-located pods
		//   - Leaves empty/underutilized nodes for backfill workloads
		//   - Better resource efficiency
		//
		// Example:
		//   Node with 80% utilized → score = 80 (high, preferred!)
		//   Node with 20% utilized → score = 20 (low, avoid)
		score = int64(utilization)

		klog.V(5).Infof("BackfillScoring: regular pod %s/%s on node %s (util=%.1f%%, score=%d)",
			pod.Namespace, pod.Name, node.Name, utilization, score)
	}

	return score, framework.NewStatus(framework.Success, "")
}

// ScoreExtensions returns a ScoreExtensions interface if it implements one, or nil if not.
func (b *BackfillScoring) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

// getPreemptibilityFromProfile determines if a pod is preemptible (backfill-eligible)
// using ProfileClassifier first, then falling back to local detection.
//
// Integration with ProfileClassifier:
//   - ProfileClassifier's IsPreemptible flag provides centralized classification
//   - Falls back to isBackfillEligible() if ProfileClassifier is unavailable
//   - Maintains backward compatibility with existing deployments
func (b *BackfillScoring) getPreemptibilityFromProfile(state framework.CycleState, pod *v1.Pod) bool {
	// Try ProfileClassifier first
	profile, err := profileclassifier.GetProfile(&state)
	if err == nil && profile != nil {
		klog.V(4).Infof("BackfillScoring: pod %s/%s preemptibility from ProfileClassifier: %v",
			pod.Namespace, pod.Name, profile.IsPreemptible)
		return profile.IsPreemptible
	}

	// Fallback to local classification
	klog.V(5).Infof("BackfillScoring: ProfileClassifier unavailable for pod %s/%s, using local classification",
		pod.Namespace, pod.Name)
	return b.isBackfillEligible(pod)
}

// isBackfillEligible determines if a pod is eligible for backfill scheduling.
//
// A pod is considered backfill-eligible if:
//  1. ProfileClassifier marks it as preemptible (profile.IsPreemptible)
//     OR (fallback if ProfileClassifier unavailable)
//  2. It has an explicit backfill label: scheduling.kubenexus.io/backfill: "true"
//     OR
//  3. It has a low priority (priority <= BackfillPriorityThreshold)
//
// Backfill-eligible pods are interruptible and can be preempted by higher-priority
// workloads via the GangPreemption plugin.
//
// CONFIGURATION EXAMPLE:
//
// Method 1: Using PriorityClass
//
//	apiVersion: scheduling.k8s.io/v1
//	kind: PriorityClass
//	metadata:
//	  name: backfill
//	value: 100
//	preemptionPolicy: PreemptLowerPriority
//	description: "Low priority for backfill/interruptible workloads"
//
// Method 2: Using Label
//
//	apiVersion: v1
//	kind: Pod
//	metadata:
//	  labels:
//	    scheduling.kubenexus.io/backfill: "true"
func (b *BackfillScoring) isBackfillEligible(pod *v1.Pod) bool {
	// Check explicit backfill label first (takes precedence)
	if backfillLabel, exists := pod.Labels[BackfillLabelKey]; exists && backfillLabel == "true" {
		klog.V(4).Infof("BackfillScoring: pod %s/%s marked as backfill via label", pod.Namespace, pod.Name)
		return true
	}

	// Check pod priority
	if pod.Spec.Priority != nil && *pod.Spec.Priority <= BackfillPriorityThreshold {
		klog.V(4).Infof("BackfillScoring: pod %s/%s eligible for backfill (priority=%d <= %d)",
			pod.Namespace, pod.Name, *pod.Spec.Priority, BackfillPriorityThreshold)
		return true
	}

	// Default: treat pods without priority as regular (not backfill)
	return false
}

// New initializes a new BackfillScoring plugin and returns it.
func New(_ context.Context, _ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	podLister := handle.SharedInformerFactory().Core().V1().Pods().Lister()

	klog.V(3).Infof("BackfillScoring plugin initialized")
	return &BackfillScoring{
		handle:    handle,
		podLister: podLister,
	}, nil
}
