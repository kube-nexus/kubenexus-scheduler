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

package resourcefragmentation

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	testutil "github.com/kube-nexus/kubenexus-scheduler/test/util"
)

const (
	// Test constants for GPU topology
	TopologyNVSwitch = "nvswitch"
	TopologyNVLink   = "nvlink"
	TopologyPCIe     = "pcie"
)

func TestName(t *testing.T) {
	plugin := &ResourceFragmentationScore{}
	if got := plugin.Name(); got != Name {
		t.Errorf("Name() = %v, want %v", got, Name)
	}
}

func TestScoreExtensions(t *testing.T) {
	plugin := &ResourceFragmentationScore{}
	if got := plugin.ScoreExtensions(); got != nil {
		t.Errorf("ScoreExtensions() = %v, want nil", got)
	}
}

func TestConstants(t *testing.T) {
	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"Name", Name, "ResourceFragmentationScore"},
		{"LargeIslandThreshold", LargeIslandThreshold, 4},
		{"SmallRequestThreshold", SmallRequestThreshold, 2},
		{"PenaltyFragmentPristineIsland", int64(PenaltyFragmentPristineIsland), int64(0)},
		{"BonusPerfectFit", int64(BonusPerfectFit), int64(90)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestGetGPURequest(t *testing.T) {
	tests := []struct {
		name        string
		pod         *v1.Pod
		expectedGPU int
	}{
		{
			name: "single container with GPU",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									ResourceGPU: resource.MustParse("4"),
								},
							},
						},
					},
				},
			},
			expectedGPU: 4,
		},
		{
			name: "no GPU request",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("2"),
								},
							},
						},
					},
				},
			},
			expectedGPU: 0,
		},
		{
			name: "multiple containers with GPUs",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									ResourceGPU: resource.MustParse("2"),
								},
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									ResourceGPU: resource.MustParse("2"),
								},
							},
						},
					},
				},
			},
			expectedGPU: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpuCount := getGPURequest(tt.pod)
			if gpuCount != tt.expectedGPU {
				t.Errorf("Expected %d GPUs, got %d", tt.expectedGPU, gpuCount)
			}
		})
	}
}

// TestScoreWithFramework tests Score() method with proper framework mocks
func TestScoreWithFramework(t *testing.T) {
	// Create nodes with different GPU topologies
	nodes := []*v1.Node{
		// Large pristine island (8 GPUs, NVSwitch)
		testutil.MakeNode("nvswitch-node-1", map[string]string{
			LabelGPUTopology: "nvswitch",
		}, v1.ResourceList{
			ResourceGPU: resource.MustParse("8"),
		}),
		// Medium island (4 GPUs, NVLink)
		testutil.MakeNode("nvlink-node-1", map[string]string{
			LabelGPUTopology: "nvlink",
		}, v1.ResourceList{
			ResourceGPU: resource.MustParse("4"),
		}),
		// Small island (2 GPUs, PCIe)
		testutil.MakeNode("pcie-node-1", map[string]string{
			LabelGPUTopology: "pcie",
		}, v1.ResourceList{
			ResourceGPU: resource.MustParse("2"),
		}),
		// Partially allocated large island
		testutil.MakeNode("nvswitch-node-2", map[string]string{
			LabelGPUTopology: "nvswitch",
		}, v1.ResourceList{
			ResourceGPU: resource.MustParse("8"),
		}),
	}

	// Pod already scheduled on nvswitch-node-2 (consumes 2 GPUs)
	existingPods := []*v1.Pod{
		testutil.MakePod("existing-pod", "default", "nvswitch-node-2",
			v1.ResourceList{ResourceGPU: resource.MustParse("2")},
			nil, nil),
	}

	tests := []struct {
		name           string
		pod            *v1.Pod
		expectedScores map[string]int64
		description    string
	}{
		{
			name: "Small request (2 GPUs) - should prefer partially used island",
			pod: testutil.MakePod("small-pod", "default", "",
				v1.ResourceList{ResourceGPU: resource.MustParse("2")},
				nil, nil),
			expectedScores: map[string]int64{
				"nvswitch-node-1": 0,  // Pristine large island gets maximum penalty
				"nvlink-node-1":   0,  // Pristine medium island gets penalty
				"pcie-node-1":     90, // Perfect fit on small island
				"nvswitch-node-2": 96, // Partially used node with CPU score
			},
			description: "Small requests avoid pristine islands",
		},
		{
			name: "Medium request (4 GPUs) - should prefer exact fit",
			pod: testutil.MakePod("medium-pod", "default", "",
				v1.ResourceList{ResourceGPU: resource.MustParse("4")},
				nil, nil),
			expectedScores: map[string]int64{
				"nvswitch-node-1": 0,  // Pristine island penalty
				"nvlink-node-1":   90, // Perfect fit bonus
				"pcie-node-1":     0,  // Too small
				"nvswitch-node-2": 98, // Partially used with CPU score
			},
			description: "Medium requests get perfect fit bonus",
		},
		{
			name: "Large request (8 GPUs) - needs large island",
			pod: testutil.MakePod("large-pod", "default", "",
				v1.ResourceList{ResourceGPU: resource.MustParse("8")},
				nil, nil),
			expectedScores: map[string]int64{
				"nvswitch-node-1": 90, // Perfect fit bonus
				"nvlink-node-1":   0,  // Too small
				"pcie-node-1":     0,  // Too small
				"nvswitch-node-2": 25, // Partially available (has 6 GPUs free)
			},
			description: "Large requests prioritize premium topology",
		},
		{
			name: "No GPU request - CPU fragmentation scores",
			pod: testutil.MakePod("cpu-pod", "default", "",
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("4")},
				nil, nil),
			expectedScores: map[string]int64{
				"nvswitch-node-1": 0, // CPU utilization score (0% used)
				"nvlink-node-1":   0, // CPU utilization score (0% used)
				"pcie-node-1":     0, // CPU utilization score (0% used)
				"nvswitch-node-2": 0, // CPU utilization score (0% used)
			},
			description: "CPU-only pods get CPU fragmentation scores",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create plugin directly with fake pod lister
			plugin := &ResourceFragmentationScore{
				handle:    nil,
				podLister: testutil.NewFakePodLister(existingPods),
			}

			// Create shared lister for getting NodeInfo
			snapshot := testutil.NewFakeSharedLister(existingPods, nodes)
			state := framework.NewCycleState()

			// Score each node
			for _, node := range nodes {
				nodeInfo, err := snapshot.NodeInfos().Get(node.Name)
				if err != nil {
					t.Fatalf("Failed to get NodeInfo for %s: %v", node.Name, err)
				}

				score, status := plugin.Score(context.Background(), state, tt.pod, nodeInfo)
				if !status.IsSuccess() {
					t.Errorf("Score failed for node %s: %v", node.Name, status.AsError())
				}

				expectedScore := tt.expectedScores[node.Name]
				if score != expectedScore {
					t.Errorf("%s - Node %s: expected score %d, got %d",
						tt.description, node.Name, expectedScore, score)
				}
			}
		})
	}
}

// TestDetectGPUIsland tests island detection logic
func TestDetectGPUIsland(t *testing.T) {
	tests := []struct {
		name              string
		node              *v1.Node
		expectedTotalGPUs int
		expectedTopology  string
		expectedQuality   int
	}{
		{
			name: "NVSwitch large island",
			node: testutil.MakeNode("nvswitch-8gpu", map[string]string{
				LabelGPUTopology: "nvswitch",
			}, v1.ResourceList{
				ResourceGPU: resource.MustParse("8"),
			}),
			expectedTotalGPUs: 8,
			expectedTopology:  "nvswitch",
			expectedQuality:   IslandQualityNVSwitch,
		},
		{
			name: "NVLink medium island",
			node: testutil.MakeNode("nvlink-4gpu", map[string]string{
				LabelGPUTopology: "nvlink",
			}, v1.ResourceList{
				ResourceGPU: resource.MustParse("4"),
			}),
			expectedTotalGPUs: 4,
			expectedTopology:  "nvlink",
			expectedQuality:   IslandQualityNVLink,
		},
		{
			name: "PCIe small island",
			node: testutil.MakeNode("pcie-2gpu", map[string]string{
				LabelGPUTopology: "pcie",
			}, v1.ResourceList{
				ResourceGPU: resource.MustParse("2"),
			}),
			expectedTotalGPUs: 2,
			expectedTopology:  "pcie",
			expectedQuality:   IslandQualityPCIe,
		},
		{
			name: "Unknown topology defaults to unknown",
			node: testutil.MakeNode("unknown-gpu", map[string]string{},
				v1.ResourceList{
					ResourceGPU: resource.MustParse("4"),
				}),
			expectedTotalGPUs: 4,
			expectedTopology:  "unknown",
			expectedQuality:   IslandQualityUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create plugin with fake pod lister
			plugin := &ResourceFragmentationScore{
				podLister: testutil.NewFakePodLister(nil),
			}

			// Create NodeInfo from node
			nodeInfo := framework.NewNodeInfo()
			nodeInfo.SetNode(tt.node)

			island := plugin.detectGPUIsland(nodeInfo)

			if island == nil {
				t.Fatal("Expected non-nil GPUIsland")
			}

			if island.TotalGPUs != tt.expectedTotalGPUs {
				t.Errorf("Expected TotalGPUs=%d, got %d", tt.expectedTotalGPUs, island.TotalGPUs)
			}
			if island.Topology != tt.expectedTopology {
				t.Errorf("Expected Topology=%s, got %s", tt.expectedTopology, island.Topology)
			}
			if island.Quality != tt.expectedQuality {
				t.Errorf("Expected Quality=%d, got %d", tt.expectedQuality, island.Quality)
			}
		})
	}
}

// TestScoreTopologyPreference tests topology tier scoring
func TestScoreTopologyPreference(t *testing.T) {
	nodes := []*v1.Node{
		testutil.MakeNode("nvswitch-node", map[string]string{
			LabelGPUTopology: "nvswitch",
		}, v1.ResourceList{ResourceGPU: resource.MustParse("8")}),
		testutil.MakeNode("nvlink-node", map[string]string{
			LabelGPUTopology: "nvlink",
		}, v1.ResourceList{ResourceGPU: resource.MustParse("4")}),
		testutil.MakeNode("pcie-node", map[string]string{
			LabelGPUTopology: "pcie",
		}, v1.ResourceList{ResourceGPU: resource.MustParse("2")}),
	}

	// Large multi-GPU workload requesting 8 GPUs
	pod := testutil.MakePod("ml-training", "default", "",
		v1.ResourceList{ResourceGPU: resource.MustParse("8")},
		nil, nil)

	snapshot := testutil.NewFakeSharedLister(nil, nodes)

	// Create plugin directly with fake pod lister
	plugin := &ResourceFragmentationScore{
		handle:    nil,
		podLister: testutil.NewFakePodLister(nil),
	}

	state := framework.NewCycleState()

	// NVSwitch should score highest for large workloads
	nvswitchInfo, err := snapshot.NodeInfos().Get("nvswitch-node")
	if err != nil {
		t.Fatalf("Failed to get nvswitch-node: %v", err)
	}
	nvswitchScore, _ := plugin.Score(context.Background(), state, pod, nvswitchInfo)

	// NVLink and PCIe should be filtered out (can't fit 8 GPUs)
	nvlinkInfo, err := snapshot.NodeInfos().Get("nvlink-node")
	if err != nil {
		t.Fatalf("Failed to get nvlink-node: %v", err)
	}
	nvlinkScore, _ := plugin.Score(context.Background(), state, pod, nvlinkInfo)

	pcieInfo, err := snapshot.NodeInfos().Get("pcie-node")
	if err != nil {
		t.Fatalf("Failed to get pcie-node: %v", err)
	}
	pcieScore, _ := plugin.Score(context.Background(), state, pod, pcieInfo)

	// NVSwitch should have perfect fit bonus (8 GPUs requested, 8 available)
	if nvswitchScore != BonusPerfectFit {
		t.Errorf("NVSwitch node should score %d (perfect fit) for 8-GPU workload, got %d",
			BonusPerfectFit, nvswitchScore)
	}
	if nvlinkScore != 0 {
		t.Errorf("NVLink node should score 0 (too small), got %d", nvlinkScore)
	}
	if pcieScore != 0 {
		t.Errorf("PCIe node should score 0 (too small), got %d", pcieScore)
	}
}
