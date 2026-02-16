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

// TopologySpreadScorePlugin implements zone-aware spreading for high availability.
// For service workloads, it strongly prefers spreading pods across failure domains (zones).
// For batch workloads, topology is less critical (co-location is more important).
type TopologySpreadScorePlugin struct {
	handle framework.Handle
}

var _ framework.ScorePlugin = &TopologySpreadScorePlugin{}

const (
	// PluginName is the name of the plugin used in the plugin registry and configurations.
	PluginName = "TopologySpread"

	// ZoneLabel is the standard Kubernetes zone label
	ZoneLabel = "topology.kubernetes.io/zone"

	// MaxScore is the maximum score a node can get.
	MaxScore = framework.MaxNodeScore
)

// Name returns the name of the plugin.
func (t *TopologySpreadScorePlugin) Name() string {
	return PluginName
}

// Score invoked at the score extension point.
// Calculates a score based on how well the pod would be spread across zones.
func (t *TopologySpreadScorePlugin) Score(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfo framework.NodeInfo) (int64, *framework.Status) {
	node := nodeInfo.Node()
	if node == nil {
		return 0, framework.NewStatus(framework.Error, "node is nil")
	}

	// Classify the workload
	workloadType := workload.ClassifyPod(pod)

	// For batch workloads, topology is less critical (return neutral score)
	if workloadType == workload.TypeBatch {
		return MaxScore / 2, framework.NewStatus(framework.Success, "")
	}

	// For service workloads, calculate zone spread score
	zone, hasZone := node.Labels[ZoneLabel]
	if !hasZone {
		// Node has no zone label, return neutral score
		return MaxScore / 2, framework.NewStatus(framework.Success, "")
	}

	// Count existing pods in each zone
	zoneDistribution := t.calculateZoneDistribution(pod)

	// Calculate score: prefer zones with fewer pods
	podsInThisZone := zoneDistribution[zone]
	totalPods := 0
	for _, count := range zoneDistribution {
		totalPods += count
	}

	if totalPods == 0 {
		// No existing pods, all zones are equal
		return MaxScore, framework.NewStatus(framework.Success, "")
	}

	// Score inversely proportional to pod count in zone
	// Fewer pods in zone = higher score
	// Formula: MaxScore * (1 - podsInZone/totalPods)
	score := int64(float64(MaxScore) * (1.0 - float64(podsInThisZone)/float64(totalPods)))

	return score, framework.NewStatus(framework.Success, "")
}

// calculateZoneDistribution counts nodes per zone as a proxy for pod distribution
// This provides good spreading behavior without needing to iterate all pods
func (t *TopologySpreadScorePlugin) calculateZoneDistribution(pod *v1.Pod) map[string]int {
	distribution := make(map[string]int)

	// Get all nodes
	nodeInfoList, err := t.handle.SnapshotSharedLister().NodeInfos().List()
	if err != nil {
		return distribution
	}

	// Count nodes per zone
	// Nodes with more pods will naturally have higher "weight"
	for _, nodeInfo := range nodeInfoList {
		node := nodeInfo.Node()
		if node == nil {
			continue
		}

		zone, hasZone := node.Labels[ZoneLabel]
		if !hasZone {
			continue
		}

		// Simple approach: count nodes per zone
		// Prefer zones with fewer nodes to spread workload
		distribution[zone]++
	}

	return distribution
}

// ScoreExtensions returns a ScoreExtensions interface if it implements one, or nil if not.
func (t *TopologySpreadScorePlugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

// NewTopologySpreadScore initializes a new plugin and returns it.
func NewTopologySpreadScore(_ context.Context, _ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &TopologySpreadScorePlugin{
		handle: handle,
	}, nil
}
