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

package tenanthardware

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"

	testutil "sigs.k8s.io/scheduler-plugins/test/util"
)

const (
	// Test constants
	ResourceGPU            = v1.ResourceName("nvidia.com/gpu")
	AnnotationPriorityTier = "scheduling.kubenexus.io/priority-tier"
)

func TestName(t *testing.T) {
	plugin := &TenantHardwareAffinity{}
	if plugin.Name() != Name {
		t.Errorf("Expected plugin name %s, got %s", Name, plugin.Name())
	}
}

func TestScoreExtensions(t *testing.T) {
	plugin := &TenantHardwareAffinity{}
	if plugin.ScoreExtensions() != nil {
		t.Error("Expected nil ScoreExtensions")
	}
}

func TestGetTenantPriority(t *testing.T) {
	plugin := &TenantHardwareAffinity{}

	tests := []struct {
		name              string
		priorityClassName string
		annotations       map[string]string
		expectedPriority  string
	}{
		{
			name:              "High priority class",
			priorityClassName: "high-priority",
			expectedPriority:  PriorityHigh,
		},
		{
			name:              "Critical priority class",
			priorityClassName: "system-critical",
			expectedPriority:  PriorityHigh,
		},
		{
			name:              "Medium priority class",
			priorityClassName: "medium-priority",
			expectedPriority:  PriorityMedium,
		},
		{
			name:              "Normal priority class",
			priorityClassName: "normal",
			expectedPriority:  PriorityMedium,
		},
		{
			name:              "Low priority class",
			priorityClassName: "low-priority",
			expectedPriority:  PriorityLow,
		},
		{
			name:              "Best-effort priority class",
			priorityClassName: "best-effort",
			expectedPriority:  PriorityLow,
		},
		{
			name:              "No priority class defaults to medium",
			priorityClassName: "",
			expectedPriority:  PriorityMedium,
		},
		{
			name:              "Annotation override",
			priorityClassName: "low-priority",
			annotations:       map[string]string{"scheduling.kubenexus.io/priority-tier": "high-priority"},
			expectedPriority:  "high-priority",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Namespace:   "default",
					Annotations: tt.annotations,
				},
				Spec: v1.PodSpec{
					PriorityClassName: tt.priorityClassName,
				},
			}
			priority := plugin.getTenantPriority(pod)
			if priority != tt.expectedPriority {
				t.Errorf("Expected priority %s, got %s", tt.expectedPriority, priority)
			}
		})
	}
}

func TestInferTierFromGPUModel(t *testing.T) {
	plugin := &TenantHardwareAffinity{}

	tests := []struct {
		name         string
		gpuModel     string
		expectedTier string
	}{
		{
			name:         "H100 is premium",
			gpuModel:     "H100",
			expectedTier: TierPremium,
		},
		{
			name:         "H200 is premium",
			gpuModel:     "H200",
			expectedTier: TierPremium,
		},
		{
			name:         "A100-80GB is premium",
			gpuModel:     "A100-80GB",
			expectedTier: TierPremium,
		},
		{
			name:         "A100 is standard",
			gpuModel:     "A100",
			expectedTier: TierStandard,
		},
		{
			name:         "A40 is standard",
			gpuModel:     "A40",
			expectedTier: TierStandard,
		},
		{
			name:         "L40 is economy",
			gpuModel:     "L40",
			expectedTier: TierEconomy,
		},
		{
			name:         "T4 is economy",
			gpuModel:     "T4",
			expectedTier: TierEconomy,
		},
		{
			name:         "Unknown GPU returns empty",
			gpuModel:     "Unknown-GPU",
			expectedTier: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tier := plugin.inferTierFromGPUModel(tt.gpuModel)
			if tier != tt.expectedTier {
				t.Errorf("Expected tier %s for GPU %s, got %s", tt.expectedTier, tt.gpuModel, tier)
			}
		})
	}
}

func TestCalculateAffinityScore(t *testing.T) {
	plugin := &TenantHardwareAffinity{}

	tests := []struct {
		name           string
		tenantPriority string
		hardwareTier   string
		expectedScore  int64
		description    string
	}{
		{
			name:           "High priority on premium hardware - perfect match",
			tenantPriority: PriorityHigh,
			hardwareTier:   TierPremium,
			expectedScore:  ScorePerfectMatch,
			description:    "Perfect match",
		},
		{
			name:           "Medium priority on standard hardware - perfect match",
			tenantPriority: PriorityMedium,
			hardwareTier:   TierStandard,
			expectedScore:  ScorePerfectMatch,
			description:    "Perfect match",
		},
		{
			name:           "Low priority on economy hardware - perfect match",
			tenantPriority: PriorityLow,
			hardwareTier:   TierEconomy,
			expectedScore:  ScorePerfectMatch,
			description:    "Perfect match",
		},
		{
			name:           "High priority on standard hardware - acceptable",
			tenantPriority: PriorityHigh,
			hardwareTier:   TierStandard,
			expectedScore:  ScoreAcceptableMatch,
			description:    "Acceptable downgrade",
		},
		{
			name:           "High priority on economy hardware - acceptable but worse",
			tenantPriority: PriorityHigh,
			hardwareTier:   TierEconomy,
			expectedScore:  ScoreAcceptableMatch - 10,
			description:    "Acceptable but not ideal",
		},
		{
			name:           "Medium priority on premium hardware - penalty",
			tenantPriority: PriorityMedium,
			hardwareTier:   TierPremium,
			expectedScore:  ScoreMismatchPenalty,
			description:    "Heavy penalty to preserve premium",
		},
		{
			name:           "Medium priority on economy hardware - acceptable",
			tenantPriority: PriorityMedium,
			hardwareTier:   TierEconomy,
			expectedScore:  ScoreAcceptableMatch,
			description:    "Acceptable downgrade",
		},
		{
			name:           "Low priority on premium hardware - penalty",
			tenantPriority: PriorityLow,
			hardwareTier:   TierPremium,
			expectedScore:  ScoreMismatchPenalty,
			description:    "Heavy penalty to preserve premium",
		},
		{
			name:           "Low priority on standard hardware - penalty",
			tenantPriority: PriorityLow,
			hardwareTier:   TierStandard,
			expectedScore:  ScoreMismatchPenalty,
			description:    "Heavy penalty to preserve standard",
		},
		{
			name:           "No hardware tier info - neutral",
			tenantPriority: PriorityHigh,
			hardwareTier:   "",
			expectedScore:  ScoreNoHardwareInfo,
			description:    "Neutral when no tier info",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
			}
			score := plugin.calculateAffinityScore(tt.tenantPriority, tt.hardwareTier, node)
			if score != tt.expectedScore {
				t.Errorf("%s: Expected score %d, got %d", tt.description, tt.expectedScore, score)
			}
		})
	}
}

func TestGetHardwareTier(t *testing.T) {
	plugin := &TenantHardwareAffinity{}

	tests := []struct {
		name         string
		node         *v1.Node
		expectedTier string
	}{
		{
			name: "Explicit premium tier label",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						LabelHardwareTier: TierPremium,
					},
				},
			},
			expectedTier: TierPremium,
		},
		{
			name: "Infer premium from H100 model",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						LabelGPUModel: "H100",
					},
				},
			},
			expectedTier: TierPremium,
		},
		{
			name: "Infer standard from A100 model",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						LabelGPUModel: "A100",
					},
				},
			},
			expectedTier: TierStandard,
		},
		{
			name: "Infer economy from L40 model",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						LabelGPUModel: "L40",
					},
				},
			},
			expectedTier: TierEconomy,
		},
		{
			name: "No tier information",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
				},
			},
			expectedTier: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tier := plugin.getHardwareTier(tt.node)
			if tier != tt.expectedTier {
				t.Errorf("Expected tier %s, got %s", tt.expectedTier, tier)
			}
		})
	}
}

// TestScoreWithFramework tests Score() method with proper framework.Handle
func TestScoreWithFramework(t *testing.T) {
	// Create test nodes with different hardware tiers
	nodes := []*v1.Node{
		testutil.MakeNode("premium-node", map[string]string{
			LabelHardwareTier: TierPremium,
			LabelGPUModel:     "H100",
		}, v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("64"),
			v1.ResourceMemory: resource.MustParse("512Gi"),
			ResourceGPU:       resource.MustParse("8"),
		}),
		testutil.MakeNode("standard-node", map[string]string{
			LabelHardwareTier: TierStandard,
			LabelGPUModel:     "A100",
		}, v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("48"),
			v1.ResourceMemory: resource.MustParse("384Gi"),
			ResourceGPU:       resource.MustParse("8"),
		}),
		testutil.MakeNode("economy-node", map[string]string{
			LabelHardwareTier: TierEconomy,
			LabelGPUModel:     "T4",
		}, v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("32"),
			v1.ResourceMemory: resource.MustParse("256Gi"),
			ResourceGPU:       resource.MustParse("4"),
		}),
	}

	tests := []struct {
		name           string
		pod            *v1.Pod
		expectedScores map[string]int64
	}{
		{
			name: "High priority pod prefers premium hardware",
			pod: testutil.MakePod("high-priority-pod", "default", "",
				v1.ResourceList{ResourceGPU: resource.MustParse("2")},
				nil,
				map[string]string{AnnotationPriorityTier: "high-priority"}),
			expectedScores: map[string]int64{
				"premium-node":  ScorePerfectMatch,         // 100
				"standard-node": ScoreAcceptableMatch,      // 70
				"economy-node":  ScoreAcceptableMatch - 10, // 60
			},
		},
		{
			name: "Medium priority pod prefers standard hardware",
			pod: testutil.MakePod("medium-priority-pod", "default", "",
				v1.ResourceList{ResourceGPU: resource.MustParse("2")},
				nil,
				map[string]string{AnnotationPriorityTier: "medium-priority"}),
			expectedScores: map[string]int64{
				"premium-node":  ScoreMismatchPenalty, // 20
				"standard-node": ScorePerfectMatch,    // 100
				"economy-node":  ScoreAcceptableMatch, // 70
			},
		},
		{
			name: "Low priority pod prefers economy hardware",
			pod: testutil.MakePod("low-priority-pod", "default", "",
				v1.ResourceList{ResourceGPU: resource.MustParse("1")},
				nil,
				map[string]string{AnnotationPriorityTier: "low-priority"}),
			expectedScores: map[string]int64{
				"premium-node":  ScoreMismatchPenalty, // 20
				"standard-node": ScoreMismatchPenalty, // 20
				"economy-node":  ScorePerfectMatch,    // 100
			},
		},
		{
			name: "Pod with priority class name",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "priority-class-pod",
					Namespace: "default",
				},
				Spec: v1.PodSpec{
					PriorityClassName: "high-priority",
					Containers: []v1.Container{{
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{ResourceGPU: resource.MustParse("2")},
						},
					}},
				},
			},
			expectedScores: map[string]int64{
				"premium-node":  ScorePerfectMatch,         // 100
				"standard-node": ScoreAcceptableMatch,      // 70
				"economy-node":  ScoreAcceptableMatch - 10, // 60
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create framework
			fh, err := testutil.NewTestFramework(nil,
				frameworkruntime.WithSnapshotSharedLister(testutil.NewFakeSharedLister(nil, nodes)))
			if err != nil {
				t.Fatalf("Failed to create framework: %v", err)
			}

			// Create plugin
			plugin, err := New(context.Background(), nil, fh)
			if err != nil {
				t.Fatalf("Failed to create plugin: %v", err)
			}

			scorePlugin, ok := plugin.(fwk.ScorePlugin)
			if !ok {
				t.Fatal("Plugin does not implement ScorePlugin interface")
			}

			state := framework.NewCycleState()

			// Score each node
			for _, node := range nodes {
				nodeInfo, err := fh.SnapshotSharedLister().NodeInfos().Get(node.Name)
				if err != nil {
					t.Fatalf("Failed to get NodeInfo for %s: %v", node.Name, err)
				}

				score, status := scorePlugin.Score(context.Background(), state, tt.pod, nodeInfo)
				if !status.IsSuccess() {
					t.Errorf("Score failed for node %s: %v", node.Name, status.AsError())
				}

				expectedScore := tt.expectedScores[node.Name]
				if score != expectedScore {
					t.Errorf("Node %s: expected score %d, got %d", node.Name, expectedScore, score)
				}
			}
		})
	}
}

// TestScoreWithNoGPUNodes tests scoring behavior on nodes without GPUs
func TestScoreWithNoGPUNodes(t *testing.T) {
	nodes := []*v1.Node{
		testutil.MakeNode("cpu-node-1", map[string]string{
			LabelHardwareTier: TierStandard,
		}, v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("64"),
			v1.ResourceMemory: resource.MustParse("512Gi"),
		}),
		testutil.MakeNode("cpu-node-2", map[string]string{}, v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("32"),
			v1.ResourceMemory: resource.MustParse("256Gi"),
		}),
	}

	pod := testutil.MakePod("cpu-pod", "default", "",
		v1.ResourceList{v1.ResourceCPU: resource.MustParse("4")},
		nil, nil)

	fh, err := testutil.NewTestFramework(nil,
		frameworkruntime.WithSnapshotSharedLister(testutil.NewFakeSharedLister(nil, nodes)))
	if err != nil {
		t.Fatalf("Failed to create framework: %v", err)
	}

	plugin, err := New(context.Background(), nil, fh)
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}

		scorePlugin, ok := plugin.(fwk.ScorePlugin)
		if !ok {
			t.Fatal("Plugin does not implement ScorePlugin interface")
		}
	state := framework.NewCycleState()

	for _, node := range nodes {
			nodeInfo, err := fh.SnapshotSharedLister().NodeInfos().Get(node.Name)
			if err != nil {
				t.Fatalf("Failed to get node %s: %v", node.Name, err)
			}
		score, status := scorePlugin.Score(context.Background(), state, pod, nodeInfo)
		if !status.IsSuccess() {
			t.Errorf("Score failed for node %s: %v", node.Name, status.AsError())
		}

		// Without hardware tier info on some nodes, should get neutral score
		if node.Name == "cpu-node-2" && score != ScoreNoHardwareInfo {
			t.Errorf("Node %s: expected neutral score %d, got %d",
				node.Name, ScoreNoHardwareInfo, score)
		}
	}
}
