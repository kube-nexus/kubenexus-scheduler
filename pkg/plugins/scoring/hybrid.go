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

package scoring

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	framework "k8s.io/kube-scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/workload"
)

// HybridScorePlugin implements workload-aware scoring:
// - Batch workloads: Bin packing (prefer fuller nodes for co-location)
// - Service workloads: Spreading (prefer emptier nodes for HA)
type HybridScorePlugin struct {
	handle framework.Handle
}

var _ framework.ScorePlugin = &HybridScorePlugin{}

const (
	// Name is the name of the plugin used in the plugin registry and configurations.
	Name = "HybridScore"

	// MaxNodeScore is the maximum score a node can get.
	MaxNodeScore = framework.MaxNodeScore
)

// Name returns the name of the plugin.
func (h *HybridScorePlugin) Name() string {
	return Name
}

// Score invoked at the score extension point.
func (h *HybridScorePlugin) Score(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfo framework.NodeInfo) (int64, *framework.Status) {
	// Classify the workload
	workloadType := workload.ClassifyPod(pod)

	// Calculate node utilization (0-100%)
	utilization := calculateNodeUtilization(nodeInfo)

	var score int64
	switch workloadType {
	case workload.TypeBatch:
		// Batch: Bin packing - prefer fuller nodes
		// Higher utilization = higher score
		// Goal: Co-locate batch pods on same nodes to:
		// 1. Reduce network latency (ML training, Spark shuffles)
		// 2. Leave empty nodes for services
		// 3. Maximize resource utilization
		score = int64(utilization)

	case workload.TypeService:
		// Service: Spreading - prefer emptier nodes
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

// ScoreExtensions returns a ScoreExtensions interface if it implements one, or nil if not.
func (h *HybridScorePlugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

// calculateNodeUtilization returns the node's resource utilization as a percentage (0-100).
// Considers both CPU and memory, weighted equally.
func calculateNodeUtilization(nodeInfo framework.NodeInfo) float64 {
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

	// TODO: Calculate requested resources from NodeInfo
	// For now, return a conservative estimate based on node capacity
	// This will be improved in a future iteration when we can access pod resources
	
	// Placeholder: return 50% utilization as neutral
	return 50.0
}

// New initializes a new plugin and returns it.
func New(_ context.Context, _ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &HybridScorePlugin{
		handle: handle,
	}, nil
}
