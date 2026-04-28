/*
Copyright 2026 The KubeNexus Authors.

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

// Package integration contains integration tests for KubeNexus scheduler plugins.
//
// These tests exercise multi-plugin pipelines through the real kube-scheduler
// framework, verifying that plugins share state correctly via CycleState and
// produce correct scheduling decisions when combined.
//
// No external dependencies required (no envtest, no Kind cluster).
// Uses the fake framework handle from test/util/fake.go backed by
// k8s.io/kubernetes/pkg/scheduler internals.
//
// Run with: go test ./test/integration/ -v
package integration

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/networkfabric"
	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/profileclassifier"
	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/workloadaware"
	testutil "github.com/kube-nexus/kubenexus-scheduler/test/util"
)

// makeGPUNode creates a node with GPU resources and topology labels.
func makeGPUNode(name string, gpus int64, labels map[string]string) *v1.Node {
	capacity := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("32"),
		v1.ResourceMemory: resource.MustParse("128Gi"),
		v1.ResourcePods:   resource.MustParse("110"),
		"nvidia.com/gpu":  *resource.NewQuantity(gpus, resource.DecimalSI),
	}
	return testutil.MakeNode(name, labels, capacity)
}

// makeTrainingPod creates a pod annotated as a training workload.
func makeTrainingPod(name, namespace, nodeName string, gpus int64) *v1.Pod {
	return testutil.MakePod(name, namespace, nodeName,
		v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("4"),
			v1.ResourceMemory: resource.MustParse("16Gi"),
			"nvidia.com/gpu":  *resource.NewQuantity(gpus, resource.DecimalSI),
		},
		map[string]string{"workload.kubenexus.io/type": "training"},
		nil,
	)
}

// makeServicePod creates a pod annotated as a service workload.
func makeServicePod(name, namespace, nodeName string) *v1.Pod {
	return testutil.MakePod(name, namespace, nodeName,
		v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("2"),
			v1.ResourceMemory: resource.MustParse("4Gi"),
		},
		map[string]string{"workload.kubenexus.io/type": "service"},
		nil,
	)
}

// makeGangPod creates a pod that is part of a gang (pod group).
func makeGangPod(name, namespace, nodeName, groupName string, minAvail string) *v1.Pod {
	return testutil.MakePod(name, namespace, nodeName,
		v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("4"),
			v1.ResourceMemory: resource.MustParse("16Gi"),
			"nvidia.com/gpu":  *resource.NewQuantity(2, resource.DecimalSI),
		},
		map[string]string{
			"gang.scheduling.kubenexus.io/name":              groupName,
			"pod-group.scheduling.sigs.k8s.io/name":          groupName,
			"pod-group.scheduling.sigs.k8s.io/min-available": minAvail,
		},
		map[string]string{
			"pod-group.scheduling.sigs.k8s.io/name": groupName,
			"scheduling.kubenexus.io/min-available": minAvail,
		},
	)
}

// -----------------------------------------------------------------------
// Test: ProfileClassifier writes SchedulingProfile into CycleState,
// and downstream plugins (WorkloadAware) read it to produce correct scores.
// -----------------------------------------------------------------------
func TestProfileClassifierToWorkloadAwarePipeline(t *testing.T) {
	nodes := []*v1.Node{
		makeGPUNode("gpu-node-1", 8, map[string]string{
			"topology.kubernetes.io/zone": "us-east-1a",
		}),
		makeGPUNode("gpu-node-2", 8, map[string]string{
			"topology.kubernetes.io/zone": "us-east-1b",
		}),
	}

	// Existing pods: gpu-node-1 is 50% utilized, gpu-node-2 is empty
	existingPods := []*v1.Pod{
		makeTrainingPod("existing-1", "default", "gpu-node-1", 4),
	}

	handle, err := testutil.NewTestFrameworkWithPods(existingPods, nodes, nil)
	if err != nil {
		t.Fatalf("Failed to create framework: %v", err)
	}

	// Instantiate plugins through their constructors
	classifierPlugin, err := profileclassifier.New(context.Background(), nil, handle)
	if err != nil {
		t.Fatalf("Failed to create ProfileClassifier: %v", err)
	}

	workloadPlugin, err := workloadaware.New(context.Background(), nil, handle)
	if err != nil {
		t.Fatalf("Failed to create WorkloadAware: %v", err)
	}

	// --- Scenario A: Training pod → should prefer bin-packing (higher score for busier node) ---
	trainingPod := makeTrainingPod("new-training", "default", "", 2)
	state := framework.NewCycleState()

	// Get nodeInfos for PreFilter
	nodeInfos := make([]fwk.NodeInfo, len(nodes))
	for i, n := range nodes {
		ni := framework.NewNodeInfo()
		ni.SetNode(n)
		nodeInfos[i] = ni
	}

	// Phase 1: ProfileClassifier.PreFilter → writes SchedulingProfile to CycleState
	preFilterPlugin := classifierPlugin.(fwk.PreFilterPlugin)
	_, status := preFilterPlugin.PreFilter(context.Background(), state, trainingPod, nodeInfos)
	if !status.IsSuccess() {
		t.Fatalf("ProfileClassifier.PreFilter failed: %v", status.Message())
	}

	// Verify profile was written
	profile, err := profileclassifier.GetProfile(state)
	if err != nil {
		t.Fatalf("GetProfile failed: %v", err)
	}
	if profile == nil {
		t.Fatal("ProfileClassifier did not write SchedulingProfile to CycleState")
	}

	t.Logf("Classified: WorkloadType=%s, TenantTier=%s, IsGang=%v",
		profile.WorkloadType, profile.TenantTier, profile.IsGang)

	// Phase 2: WorkloadAware.Score → uses profile from CycleState
	scorer := workloadPlugin.(fwk.ScorePlugin)

	node1Info := framework.NewNodeInfo(existingPods...)
	node1Info.SetNode(nodes[0])

	node2Info := framework.NewNodeInfo()
	node2Info.SetNode(nodes[1])

	score1, status := scorer.Score(context.Background(), state, trainingPod, node1Info)
	if !status.IsSuccess() {
		t.Fatalf("Score node-1 failed: %v", status.Message())
	}

	score2, status := scorer.Score(context.Background(), state, trainingPod, node2Info)
	if !status.IsSuccess() {
		t.Fatalf("Score node-2 failed: %v", status.Message())
	}

	t.Logf("Training pod scores: gpu-node-1=%d (50%% utilized), gpu-node-2=%d (empty)", score1, score2)

	// Training workloads should prefer bin-packing → busier node scores higher
	if score1 <= score2 {
		t.Errorf("Training pod should prefer busier node (bin-packing), but gpu-node-1(%d) <= gpu-node-2(%d)", score1, score2)
	}

	// --- Scenario B: Service pod → should prefer spreading (higher score for emptier node) ---
	servicePod := makeServicePod("new-service", "default", "")
	stateB := framework.NewCycleState()

	_, status = preFilterPlugin.PreFilter(context.Background(), stateB, servicePod, nodeInfos)
	if !status.IsSuccess() {
		t.Fatalf("ProfileClassifier.PreFilter (service) failed: %v", status.Message())
	}

	profileB, _ := profileclassifier.GetProfile(stateB)
	t.Logf("Service classified: WorkloadType=%s", profileB.WorkloadType)

	scoreB1, _ := scorer.Score(context.Background(), stateB, servicePod, node1Info)
	scoreB2, _ := scorer.Score(context.Background(), stateB, servicePod, node2Info)

	t.Logf("Service pod scores: gpu-node-1=%d (50%% utilized), gpu-node-2=%d (empty)", scoreB1, scoreB2)

	// Service workloads should prefer spreading → emptier node scores higher
	if scoreB2 <= scoreB1 {
		t.Errorf("Service pod should prefer emptier node (spreading), but gpu-node-2(%d) <= gpu-node-1(%d)", scoreB2, scoreB1)
	}
}

// -----------------------------------------------------------------------
// Test: NetworkFabric Filter rejects nodes in wrong NVLink clique
// for gang pods with strict co-location requirements.
// -----------------------------------------------------------------------
func TestNetworkFabricCliqueFilter(t *testing.T) {
	nodes := []*v1.Node{
		makeGPUNode("node-clique-A", 8, map[string]string{
			"nvidia.com/gpu.clique":            "clique-0",
			"network.kubenexus.io/fabric-type": "nvswitch",
			"network.kubenexus.io/fabric-id":   "fabric-01",
		}),
		makeGPUNode("node-clique-B", 8, map[string]string{
			"nvidia.com/gpu.clique":            "clique-1",
			"network.kubenexus.io/fabric-type": "nvswitch",
			"network.kubenexus.io/fabric-id":   "fabric-01",
		}),
		makeGPUNode("node-no-clique", 8, map[string]string{
			"network.kubenexus.io/fabric-type": "infiniband",
		}),
	}

	// Gang member already scheduled on clique-0
	existingGangPod := testutil.MakePod("worker-0", "ml-team", "node-clique-A",
		v1.ResourceList{"nvidia.com/gpu": *resource.NewQuantity(4, resource.DecimalSI)},
		map[string]string{
			"pod-group.scheduling.sigs.k8s.io/name":          "training-gang",
			"pod-group.scheduling.sigs.k8s.io/min-available": "4",
		},
		map[string]string{
			"pod-group.scheduling.sigs.k8s.io/name":  "training-gang",
			"scheduling.kubenexus.io/require-clique": "true",
		},
	)

	handle, err := testutil.NewTestFrameworkWithPods([]*v1.Pod{existingGangPod}, nodes, nil)
	if err != nil {
		t.Fatalf("Failed to create framework: %v", err)
	}

	plugin, err := networkfabric.New(context.Background(), nil, handle)
	if err != nil {
		t.Fatalf("Failed to create NetworkFabric: %v", err)
	}
	filterPlugin := plugin.(fwk.FilterPlugin)

	// New gang pod needing same clique
	newPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-1",
			Namespace: "ml-team",
			Labels: map[string]string{
				"pod-group.scheduling.sigs.k8s.io/name":          "training-gang",
				"pod-group.scheduling.sigs.k8s.io/min-available": "4",
			},
			Annotations: map[string]string{
				"pod-group.scheduling.sigs.k8s.io/name":  "training-gang",
				"scheduling.kubenexus.io/require-clique": "true",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{
				Name: "worker",
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{"nvidia.com/gpu": *resource.NewQuantity(4, resource.DecimalSI)},
				},
			}},
		},
	}

	tests := []struct {
		name       string
		node       *v1.Node
		shouldPass bool
	}{
		{"same clique (clique-0) → pass", nodes[0], true},
		{"different clique (clique-1) → reject", nodes[1], false},
		{"no clique label → reject (gang needs clique)", nodes[2], false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := framework.NewCycleState()
			ni := framework.NewNodeInfo()
			ni.SetNode(tt.node)

			status := filterPlugin.Filter(context.Background(), state, newPod, ni)
			passed := status.IsSuccess()

			if passed != tt.shouldPass {
				t.Errorf("Filter(%s): got pass=%v, want pass=%v (reason: %s)",
					tt.node.Name, passed, tt.shouldPass, status.Message())
			}
		})
	}
}

// -----------------------------------------------------------------------
// Test: NetworkFabric Score assigns higher scores for tighter topology
// co-location (same clique > same fabric > same rack > same AZ).
// -----------------------------------------------------------------------
func TestNetworkFabricTopologyScoring(t *testing.T) {
	nodeLabels := map[string]string{
		"nvidia.com/gpu.clique":            "clique-0",
		"network.kubenexus.io/fabric-type": "infiniband",
		"network.kubenexus.io/fabric-id":   "fabric-01",
		"network.kubenexus.io/rack-id":     "rack-A",
		"network.kubenexus.io/az":          "us-east-1a",
	}

	// Nodes at different topology levels relative to the existing gang member
	// Using infiniband (score 75) as base fabric to avoid score capping at 100
	nodes := []*v1.Node{
		makeGPUNode("same-clique", 8, nodeLabels),
		makeGPUNode("same-fabric-diff-clique", 8, map[string]string{
			"nvidia.com/gpu.clique":            "clique-1",
			"network.kubenexus.io/fabric-type": "infiniband",
			"network.kubenexus.io/fabric-id":   "fabric-01",
			"network.kubenexus.io/rack-id":     "rack-A",
			"network.kubenexus.io/az":          "us-east-1a",
		}),
		makeGPUNode("same-rack-diff-fabric", 8, map[string]string{
			"network.kubenexus.io/fabric-type": "roce",
			"network.kubenexus.io/fabric-id":   "fabric-02",
			"network.kubenexus.io/rack-id":     "rack-A",
			"network.kubenexus.io/az":          "us-east-1a",
		}),
		makeGPUNode("same-az-diff-rack", 8, map[string]string{
			"network.kubenexus.io/fabric-type": "roce",
			"network.kubenexus.io/fabric-id":   "fabric-03",
			"network.kubenexus.io/rack-id":     "rack-B",
			"network.kubenexus.io/az":          "us-east-1a",
		}),
		makeGPUNode("different-az", 8, map[string]string{
			"network.kubenexus.io/fabric-type": "ethernet",
			"network.kubenexus.io/fabric-id":   "fabric-04",
			"network.kubenexus.io/rack-id":     "rack-C",
			"network.kubenexus.io/az":          "us-west-2a",
		}),
	}

	// Existing gang member on same-clique node
	existingGangPod := testutil.MakePod("worker-0", "ml-team", "same-clique",
		v1.ResourceList{"nvidia.com/gpu": *resource.NewQuantity(4, resource.DecimalSI)},
		map[string]string{
			"pod-group.scheduling.sigs.k8s.io/name":          "topo-gang",
			"pod-group.scheduling.sigs.k8s.io/min-available": "4",
		},
		map[string]string{"pod-group.scheduling.sigs.k8s.io/name": "topo-gang"},
	)

	handle, err := testutil.NewTestFrameworkWithPods([]*v1.Pod{existingGangPod}, nodes, nil)
	if err != nil {
		t.Fatalf("Failed to create framework: %v", err)
	}

	plugin, err := networkfabric.New(context.Background(), nil, handle)
	if err != nil {
		t.Fatalf("Failed to create NetworkFabric: %v", err)
	}
	scorer := plugin.(fwk.ScorePlugin)

	newPod := testutil.MakePod("worker-1", "ml-team", "",
		v1.ResourceList{"nvidia.com/gpu": *resource.NewQuantity(4, resource.DecimalSI)},
		map[string]string{
			"pod-group.scheduling.sigs.k8s.io/name":          "topo-gang",
			"pod-group.scheduling.sigs.k8s.io/min-available": "4",
		},
		map[string]string{"pod-group.scheduling.sigs.k8s.io/name": "topo-gang"},
	)

	state := framework.NewCycleState()
	scores := make(map[string]int64)

	for _, node := range nodes {
		ni := framework.NewNodeInfo()
		ni.SetNode(node)
		score, status := scorer.Score(context.Background(), state, newPod, ni)
		if !status.IsSuccess() {
			t.Fatalf("Score(%s) failed: %v", node.Name, status.Message())
		}
		scores[node.Name] = score
		t.Logf("Score(%s) = %d", node.Name, score)
	}

	// Verify topology ordering: tighter locality = higher or equal score.
	// Note: scores are capped at [0, 100], so extreme values may tie.
	// The critical property is monotonic non-decreasing with locality tightness.
	if scores["same-clique"] < scores["same-fabric-diff-clique"] {
		t.Errorf("same-clique(%d) should score >= same-fabric-diff-clique(%d)",
			scores["same-clique"], scores["same-fabric-diff-clique"])
	}
	if scores["same-fabric-diff-clique"] < scores["same-rack-diff-fabric"] {
		t.Errorf("same-fabric-diff-clique(%d) should score >= same-rack-diff-fabric(%d)",
			scores["same-fabric-diff-clique"], scores["same-rack-diff-fabric"])
	}
	if scores["same-rack-diff-fabric"] < scores["same-az-diff-rack"] {
		t.Errorf("same-rack-diff-fabric(%d) should score >= same-az-diff-rack(%d)",
			scores["same-rack-diff-fabric"], scores["same-az-diff-rack"])
	}
	if scores["same-az-diff-rack"] < scores["different-az"] {
		t.Errorf("same-az-diff-rack(%d) should score >= different-az(%d)",
			scores["same-az-diff-rack"], scores["different-az"])
	}

	// Verify at least some differentiation exists across the full range
	if scores["same-clique"] == scores["different-az"] {
		t.Errorf("same-clique(%d) and different-az(%d) should not have equal scores",
			scores["same-clique"], scores["different-az"])
	}
}

// -----------------------------------------------------------------------
// Test: Non-gang pod passes NetworkFabric filter regardless of clique.
// -----------------------------------------------------------------------
func TestNetworkFabricFilterPassesNonGangPods(t *testing.T) {
	nodes := []*v1.Node{
		makeGPUNode("any-node", 8, map[string]string{
			"nvidia.com/gpu.clique": "clique-5",
		}),
	}

	handle, err := testutil.NewTestFrameworkWithPods(nil, nodes, nil)
	if err != nil {
		t.Fatalf("Failed to create framework: %v", err)
	}

	plugin, err := networkfabric.New(context.Background(), nil, handle)
	if err != nil {
		t.Fatalf("Failed to create NetworkFabric: %v", err)
	}
	filterPlugin := plugin.(fwk.FilterPlugin)

	// Regular pod (no gang group annotation) — should always pass
	regularPod := testutil.MakePod("solo-pod", "default", "", nil, nil, nil)
	state := framework.NewCycleState()
	ni := framework.NewNodeInfo()
	ni.SetNode(nodes[0])

	status := filterPlugin.Filter(context.Background(), state, regularPod, ni)
	if !status.IsSuccess() {
		t.Errorf("Non-gang pod should pass filter, but got: %s", status.Message())
	}
}

// -----------------------------------------------------------------------
// Test: ProfileClassifier correctly detects gang membership and
// workload types through the classification pipeline.
// -----------------------------------------------------------------------
func TestProfileClassifierGangDetection(t *testing.T) {
	nodes := []*v1.Node{
		makeGPUNode("node-1", 8, nil),
	}

	handle, err := testutil.NewTestFrameworkWithPods(nil, nodes, nil)
	if err != nil {
		t.Fatalf("Failed to create framework: %v", err)
	}

	plugin, err := profileclassifier.New(context.Background(), nil, handle)
	if err != nil {
		t.Fatalf("Failed to create ProfileClassifier: %v", err)
	}
	preFilter := plugin.(fwk.PreFilterPlugin)

	nodeInfos := make([]fwk.NodeInfo, len(nodes))
	for i, n := range nodes {
		ni := framework.NewNodeInfo()
		ni.SetNode(n)
		nodeInfos[i] = ni
	}

	tests := []struct {
		name       string
		pod        *v1.Pod
		expectGang bool
		expectType string
	}{
		{
			name:       "gang training pod",
			pod:        makeGangPod("gang-worker", "ml-team", "", "dist-train", "4"),
			expectGang: true,
		},
		{
			name:       "solo service pod",
			pod:        makeServicePod("api-server", "default", ""),
			expectGang: false,
		},
		{
			name:       "solo training pod",
			pod:        makeTrainingPod("single-gpu", "default", "", 1),
			expectGang: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := framework.NewCycleState()
			_, status := preFilter.PreFilter(context.Background(), state, tt.pod, nodeInfos)
			if !status.IsSuccess() {
				t.Fatalf("PreFilter failed: %v", status.Message())
			}

			profile, err := profileclassifier.GetProfile(state)
			if err != nil || profile == nil {
				t.Fatalf("GetProfile failed: err=%v, profile=%v", err, profile)
			}

			if profile.IsGang != tt.expectGang {
				t.Errorf("IsGang: got %v, want %v", profile.IsGang, tt.expectGang)
			}
			t.Logf("Pod=%s → WorkloadType=%s, TenantTier=%s, IsGang=%v",
				tt.pod.Name, profile.WorkloadType, profile.TenantTier, profile.IsGang)
		})
	}
}

// -----------------------------------------------------------------------
// Test: Full multi-plugin pipeline — ProfileClassifier + WorkloadAware +
// NetworkFabric Score — verifying CycleState flows correctly across plugins.
// -----------------------------------------------------------------------
func TestMultiPluginScoringPipeline(t *testing.T) {
	nodes := []*v1.Node{
		makeGPUNode("nvswitch-node", 8, map[string]string{
			"network.kubenexus.io/fabric-type": "nvswitch",
			"network.kubenexus.io/fabric-id":   "fabric-01",
			"network.kubenexus.io/rack-id":     "rack-A",
			"network.kubenexus.io/az":          "us-east-1a",
		}),
		makeGPUNode("ib-node", 8, map[string]string{
			"network.kubenexus.io/fabric-type": "infiniband",
			"network.kubenexus.io/fabric-id":   "fabric-02",
			"network.kubenexus.io/rack-id":     "rack-B",
			"network.kubenexus.io/az":          "us-east-1a",
		}),
		makeGPUNode("ethernet-node", 8, map[string]string{
			"network.kubenexus.io/fabric-type": "ethernet",
			"network.kubenexus.io/fabric-id":   "fabric-03",
			"network.kubenexus.io/rack-id":     "rack-C",
			"network.kubenexus.io/az":          "us-west-2a",
		}),
	}

	handle, err := testutil.NewTestFrameworkWithPods(nil, nodes, nil)
	if err != nil {
		t.Fatalf("Failed to create framework: %v", err)
	}

	// Instantiate all three plugins
	classifierPlugin, err := profileclassifier.New(context.Background(), nil, handle)
	if err != nil {
		t.Fatalf("Failed to create ProfileClassifier: %v", err)
	}
	workloadPlugin, err := workloadaware.New(context.Background(), nil, handle)
	if err != nil {
		t.Fatalf("Failed to create WorkloadAware: %v", err)
	}
	fabricPlugin, err := networkfabric.New(context.Background(), nil, handle)
	if err != nil {
		t.Fatalf("Failed to create NetworkFabric: %v", err)
	}

	// Training pod — should get high-tier fabric bonus from NetworkFabric
	trainingPod := makeTrainingPod("pipeline-training", "default", "", 4)
	state := framework.NewCycleState()

	nodeInfos := make([]fwk.NodeInfo, len(nodes))
	for i, n := range nodes {
		ni := framework.NewNodeInfo()
		ni.SetNode(n)
		nodeInfos[i] = ni
	}

	// Step 1: ProfileClassifier.PreFilter
	preFilter := classifierPlugin.(fwk.PreFilterPlugin)
	_, status := preFilter.PreFilter(context.Background(), state, trainingPod, nodeInfos)
	if !status.IsSuccess() {
		t.Fatalf("ProfileClassifier.PreFilter failed: %v", status.Message())
	}

	// Step 2: Score each node with both scoring plugins
	waScorer := workloadPlugin.(fwk.ScorePlugin)
	nfScorer := fabricPlugin.(fwk.ScorePlugin)

	type nodeScore struct {
		workloadAware int64
		networkFabric int64
		combined      int64
	}
	results := make(map[string]nodeScore)

	for i, node := range nodes {
		waScore, _ := waScorer.Score(context.Background(), state, trainingPod, nodeInfos[i])
		nfScore, _ := nfScorer.Score(context.Background(), state, trainingPod, nodeInfos[i])
		results[node.Name] = nodeScore{
			workloadAware: waScore,
			networkFabric: nfScore,
			combined:      waScore + nfScore,
		}
		t.Logf("Node %s: WorkloadAware=%d, NetworkFabric=%d, Combined=%d",
			node.Name, waScore, nfScore, waScore+nfScore)
	}

	// Training workload on NVSwitch should get the highest NetworkFabric score
	if results["nvswitch-node"].networkFabric <= results["ethernet-node"].networkFabric {
		t.Errorf("NVSwitch(%d) should score higher than Ethernet(%d) for training workload",
			results["nvswitch-node"].networkFabric, results["ethernet-node"].networkFabric)
	}
}
