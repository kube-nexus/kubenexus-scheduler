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

package numatopology

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNUMATopologyPluginName(t *testing.T) {
	plugin := &NUMATopology{}
	expected := "NUMATopology"
	if plugin.Name() != expected {
		t.Errorf("Expected plugin name %s, got %s", expected, plugin.Name())
	}
}

func TestNUMATopologyConstants(t *testing.T) {
	if Name != "NUMATopology" {
		t.Errorf("Expected Name to be 'NUMATopology', got %s", Name)
	}

	if MaxNodeScore != 100 {
		t.Errorf("Expected MaxNodeScore to be 100, got %d", MaxNodeScore)
	}

	if NUMAPolicySingleNode != "single-numa-node" {
		t.Errorf("Expected NUMAPolicySingleNode to be 'single-numa-node', got %s", NUMAPolicySingleNode)
	}
}

func TestScoreExtensions(t *testing.T) {
	plugin := &NUMATopology{}
	if plugin.ScoreExtensions() != nil {
		t.Error("NUMATopology.ScoreExtensions() should return nil")
	}
}

func TestGetNUMAPolicy(t *testing.T) {
	plugin := &NUMATopology{}

	tests := []struct {
		name     string
		pod      *v1.Pod
		expected string
	}{
		{
			name: "Pod with explicit single-numa-node policy",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationNUMAPolicy: NUMAPolicySingleNode,
					},
				},
			},
			expected: NUMAPolicySingleNode,
		},
		{
			name: "Pod with best-effort policy",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationNUMAPolicy: NUMAPolicyBestEffort,
					},
				},
			},
			expected: NUMAPolicyBestEffort,
		},
		{
			name: "Pod with none policy",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationNUMAPolicy: NUMAPolicyNone,
					},
				},
			},
			expected: NUMAPolicyNone,
		},
		{
			name: "Batch pod without annotation (should default to single-numa-node)",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"workload.kubenexus.io/type": "batch",
					},
				},
			},
			expected: NUMAPolicySingleNode,
		},
		{
			name: "Service pod without annotation (should default to none)",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"workload.kubenexus.io/type": "service",
					},
				},
			},
			expected: NUMAPolicyNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := plugin.getNUMAPolicy(tt.pod)
			if result != tt.expected {
				t.Errorf("getNUMAPolicy() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseCPUList(t *testing.T) {
	tests := []struct {
		name        string
		cpuList     string
		expected    []int
		expectError bool
	}{
		{
			name:     "Single CPU",
			cpuList:  "0",
			expected: []int{0},
		},
		{
			name:     "CPU range",
			cpuList:  "0-7",
			expected: []int{0, 1, 2, 3, 4, 5, 6, 7},
		},
		{
			name:     "Multiple ranges",
			cpuList:  "0-3,8-11",
			expected: []int{0, 1, 2, 3, 8, 9, 10, 11},
		},
		{
			name:     "Mixed single and ranges",
			cpuList:  "0,2-4,7",
			expected: []int{0, 2, 3, 4, 7},
		},
		{
			name:     "Complex NUMA pattern",
			cpuList:  "0-15,32-47",
			expected: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47},
		},
		{
			name:        "Invalid format",
			cpuList:     "abc",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseCPUList(tt.cpuList)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("Length mismatch: got %d CPUs, want %d", len(result), len(tt.expected))
				return
			}

			for i, cpu := range result {
				if cpu != tt.expected[i] {
					t.Errorf("CPU mismatch at index %d: got %d, want %d", i, cpu, tt.expected[i])
				}
			}
		})
	}
}

func TestGetPodResourceRequests(t *testing.T) {
	plugin := &NUMATopology{}

	tests := []struct {
		name           string
		pod            *v1.Pod
		expectedCPU    int64
		expectedMemory int64
	}{
		{
			name: "Pod with CPU and memory requests",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("4"),
									v1.ResourceMemory: resource.MustParse("8Gi"),
								},
							},
						},
					},
				},
			},
			expectedCPU:    4,
			expectedMemory: 8 * 1024 * 1024 * 1024,
		},
		{
			name: "Pod with multiple containers",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("2"),
									v1.ResourceMemory: resource.MustParse("4Gi"),
								},
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("2"),
									v1.ResourceMemory: resource.MustParse("4Gi"),
								},
							},
						},
					},
				},
			},
			expectedCPU:    4,
			expectedMemory: 8 * 1024 * 1024 * 1024,
		},
		{
			name: "Pod with millicpu requests",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("500m"),
									v1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
			expectedCPU:    1, // Rounds up from 0.5
			expectedMemory: 1 * 1024 * 1024 * 1024,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cpu, memory := plugin.getPodResourceRequests(tt.pod)

			if cpu != tt.expectedCPU {
				t.Errorf("CPU mismatch: got %d, want %d", cpu, tt.expectedCPU)
			}

			if memory != tt.expectedMemory {
				t.Errorf("Memory mismatch: got %d, want %d", memory, tt.expectedMemory)
			}
		})
	}
}

func TestParseNUMATopology(t *testing.T) {
	plugin := &NUMATopology{}

	tests := []struct {
		name          string
		node          *v1.Node
		expectError   bool
		expectedCount int
	}{
		{
			name: "Node with 2 NUMA nodes",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						LabelNUMANodeCount:                "2",
						"numa.kubenexus.io/node-0-cpus":   "0-15",
						"numa.kubenexus.io/node-0-memory": "68719476736",
						"numa.kubenexus.io/node-1-cpus":   "16-31",
						"numa.kubenexus.io/node-1-memory": "68719476736",
					},
				},
			},
			expectedCount: 2,
		},
		{
			name: "Node without NUMA labels",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
			},
			expectError: true,
		},
		{
			name: "Node with invalid NUMA count",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						LabelNUMANodeCount: "invalid",
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := plugin.parseNUMATopology(tt.node)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(result) != tt.expectedCount {
				t.Errorf("NUMA node count mismatch: got %d, want %d", len(result), tt.expectedCount)
			}
		})
	}
}
