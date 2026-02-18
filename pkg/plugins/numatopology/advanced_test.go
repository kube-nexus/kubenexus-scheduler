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

func TestIsMemoryIntensive(t *testing.T) {
	plugin := &NUMATopology{}

	tests := []struct {
		name     string
		pod      *v1.Pod
		expected bool
	}{
		{
			name: "Explicit annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationMemoryIntensive: "true",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("2"),
									v1.ResourceMemory: resource.MustParse("8Gi"),
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "High memory and ratio (>16GB, >4GB/core)",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("4"),
									v1.ResourceMemory: resource.MustParse("20Gi"),
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Low memory (<16GB)",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
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
			expected: false,
		},
		{
			name: "High memory but low ratio",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("8"),
									v1.ResourceMemory: resource.MustParse("20Gi"),
								},
							},
						},
					},
				},
			},
			expected: false, // 20GB / 8 cores = 2.5GB/core < 4GB/core
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := plugin.isMemoryIntensive(tt.pod)
			if result != tt.expected {
				t.Errorf("isMemoryIntensive() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetNUMAAffinityPreferences(t *testing.T) {
	plugin := &NUMATopology{}

	tests := []struct {
		name              string
		pod               *v1.Pod
		expectedPreferred []int
		expectedAvoided   []int
	}{
		{
			name: "Affinity only",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationNUMAAffinityNodeID: "0,1",
					},
				},
			},
			expectedPreferred: []int{0, 1},
			expectedAvoided:   nil,
		},
		{
			name: "Anti-affinity only",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationNUMAAntiAffinityNodeID: "2,3",
					},
				},
			},
			expectedPreferred: nil,
			expectedAvoided:   []int{2, 3},
		},
		{
			name: "Both affinity and anti-affinity",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationNUMAAffinityNodeID:     "0,1",
						AnnotationNUMAAntiAffinityNodeID: "2,3",
					},
				},
			},
			expectedPreferred: []int{0, 1},
			expectedAvoided:   []int{2, 3},
		},
		{
			name: "No annotations",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
			},
			expectedPreferred: nil,
			expectedAvoided:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			preferred, avoided := plugin.getNUMAAffinityPreferences(tt.pod)

			if len(preferred) != len(tt.expectedPreferred) {
				t.Errorf("preferred length = %d, want %d", len(preferred), len(tt.expectedPreferred))
			}

			if len(avoided) != len(tt.expectedAvoided) {
				t.Errorf("avoided length = %d, want %d", len(avoided), len(tt.expectedAvoided))
			}
		})
	}
}

func TestParseNUMAIDList(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []int
	}{
		{
			name:     "Single ID",
			input:    "0",
			expected: []int{0},
		},
		{
			name:     "Multiple IDs",
			input:    "0,1,2",
			expected: []int{0, 1, 2},
		},
		{
			name:     "With spaces",
			input:    "0, 1, 2",
			expected: []int{0, 1, 2},
		},
		{
			name:     "Empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "Invalid entries ignored",
			input:    "0,abc,2",
			expected: []int{0, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseNUMAIDList(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("parseNUMAIDList() length = %d, want %d", len(result), len(tt.expected))
			}

			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("parseNUMAIDList()[%d] = %d, want %d", i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestIsNUMAInList(t *testing.T) {
	plugin := &NUMATopology{}

	tests := []struct {
		name     string
		numaID   int
		list     []int
		expected bool
	}{
		{
			name:     "ID in list",
			numaID:   1,
			list:     []int{0, 1, 2},
			expected: true,
		},
		{
			name:     "ID not in list",
			numaID:   3,
			list:     []int{0, 1, 2},
			expected: false,
		},
		{
			name:     "Empty list",
			numaID:   0,
			list:     []int{},
			expected: false,
		},
		{
			name:     "Nil list",
			numaID:   0,
			list:     nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := plugin.isNUMAInList(tt.numaID, tt.list)
			if result != tt.expected {
				t.Errorf("isNUMAInList() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCalculateNUMADistanceScore(t *testing.T) {
	plugin := &NUMATopology{}

	tests := []struct {
		name     string
		numa     NUMANode
		pod      *v1.Pod
		expected float64
		minScore float64
		maxScore float64
	}{
		{
			name: "No distance info",
			numa: NUMANode{
				ID:       0,
				Distance: map[int]int{},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
			},
			expected: 50.0,
			minScore: 50.0,
			maxScore: 50.0,
		},
		{
			name: "Single NUMA (no other nodes)",
			numa: NUMANode{
				ID: 0,
				Distance: map[int]int{
					0: 10,
				},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
			},
			expected: 100.0,
			minScore: 100.0,
			maxScore: 100.0,
		},
		{
			name: "Two NUMA nodes, local distance",
			numa: NUMANode{
				ID: 0,
				Distance: map[int]int{
					0: 10,
					1: 20,
				},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
			},
			// Score = 100 - (20-10)*5 = 50
			expected: 50.0,
			minScore: 40.0,
			maxScore: 60.0,
		},
		{
			name: "High distance weight",
			numa: NUMANode{
				ID: 0,
				Distance: map[int]int{
					0: 10,
					1: 30,
				},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationNUMADistance: "100", // 2x weight
					},
				},
			},
			// Score = 100 - (30-10)*5*2 = -100 → 0
			expected: 0.0,
			minScore: 0.0,
			maxScore: 10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := plugin.calculateNUMADistanceScore(tt.numa, []NUMANode{tt.numa}, tt.pod)

			if result < tt.minScore || result > tt.maxScore {
				t.Errorf("calculateNUMADistanceScore() = %.2f, want between %.2f and %.2f",
					result, tt.minScore, tt.maxScore)
			}
		})
	}
}

func TestCalculateGangAffinityScore(t *testing.T) {
	plugin := &NUMATopology{
		gangState: make(map[string]*GangNUMAState),
	}

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}

	tests := []struct {
		name     string
		pod      *v1.Pod
		numa     NUMANode
		setup    func()
		expected float64
	}{
		{
			name: "Not a gang member",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
			},
			numa:     NUMANode{ID: 0},
			setup:    func() {},
			expected: 50.0,
		},
		{
			name: "First gang member",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationGangGroup: "gang-1",
					},
				},
			},
			numa:     NUMANode{ID: 0},
			setup:    func() {},
			expected: 50.0,
		},
		{
			name: "Packed - same NUMA as gang member",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod-1",
					Annotations: map[string]string{
						AnnotationGangGroup:      "gang-2",
						AnnotationGangNUMASpread: GangNUMASpreadPacked,
					},
				},
			},
			numa: NUMANode{ID: 0},
			setup: func() {
				plugin.gangState["gang-2"] = &GangNUMAState{
					GangGroup:    "gang-2",
					SpreadPolicy: GangNUMASpreadPacked,
					AssignedMembers: map[string]int{
						"default/pod-0": 0,
					},
				}
			},
			expected: 100.0,
		},
		{
			name: "Packed - different NUMA from gang member",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod-1",
					Annotations: map[string]string{
						AnnotationGangGroup:      "gang-3",
						AnnotationGangNUMASpread: GangNUMASpreadPacked,
					},
				},
			},
			numa: NUMANode{ID: 1},
			setup: func() {
				plugin.gangState["gang-3"] = &GangNUMAState{
					GangGroup:    "gang-3",
					SpreadPolicy: GangNUMASpreadPacked,
					AssignedMembers: map[string]int{
						"default/pod-0": 0,
					},
				}
			},
			expected: 20.0,
		},
		{
			name: "Isolated - empty NUMA",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod-1",
					Annotations: map[string]string{
						AnnotationGangGroup:      "gang-4",
						AnnotationGangNUMASpread: GangNUMASpreadIsolated,
					},
				},
			},
			numa: NUMANode{ID: 1},
			setup: func() {
				plugin.gangState["gang-4"] = &GangNUMAState{
					GangGroup:    "gang-4",
					SpreadPolicy: GangNUMASpreadIsolated,
					AssignedMembers: map[string]int{
						"default/pod-0": 0,
					},
				}
			},
			expected: 100.0,
		},
		{
			name: "Isolated - occupied NUMA",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod-1",
					Annotations: map[string]string{
						AnnotationGangGroup:      "gang-5",
						AnnotationGangNUMASpread: GangNUMASpreadIsolated,
					},
				},
			},
			numa: NUMANode{ID: 0},
			setup: func() {
				plugin.gangState["gang-5"] = &GangNUMAState{
					GangGroup:    "gang-5",
					SpreadPolicy: GangNUMASpreadIsolated,
					AssignedMembers: map[string]int{
						"default/pod-0": 0,
					},
				}
			},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup gang state
			plugin.gangState = make(map[string]*GangNUMAState)
			tt.setup()

			result := plugin.calculateGangAffinityScore(tt.pod, tt.numa, node)

			if result != tt.expected {
				t.Errorf("calculateGangAffinityScore() = %.2f, want %.2f", result, tt.expected)
			}
		})
	}
}

func TestRecordGangPlacement(t *testing.T) {
	plugin := &NUMATopology{
		gangState: make(map[string]*GangNUMAState),
	}

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-0",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationGangGroup:      "gang-1",
				AnnotationGangNUMASpread: GangNUMASpreadPacked,
			},
		},
	}

	// Record first placement
	plugin.recordGangPlacement(pod, 0, node)

	// Check gang state was created
	gangState, exists := plugin.gangState["gang-1"]
	if !exists {
		t.Fatal("Gang state not created")
	}

	if gangState.SpreadPolicy != GangNUMASpreadPacked {
		t.Errorf("SpreadPolicy = %s, want %s", gangState.SpreadPolicy, GangNUMASpreadPacked)
	}

	if len(gangState.AssignedMembers) != 1 {
		t.Errorf("AssignedMembers length = %d, want 1", len(gangState.AssignedMembers))
	}

	if numaID := gangState.AssignedMembers["default/pod-0"]; numaID != 0 {
		t.Errorf("Assigned NUMA = %d, want 0", numaID)
	}

	// Record second placement
	pod2 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-1",
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationGangGroup: "gang-1",
			},
		},
	}

	plugin.recordGangPlacement(pod2, 1, node)

	if len(gangState.AssignedMembers) != 2 {
		t.Errorf("AssignedMembers length = %d, want 2", len(gangState.AssignedMembers))
	}
}

func TestAdvancedNUMAConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{
			name:     "AnnotationGangGroup",
			constant: AnnotationGangGroup,
			expected: "scheduling.kubenexus.io/gang-group",
		},
		{
			name:     "AnnotationGangNUMASpread",
			constant: AnnotationGangNUMASpread,
			expected: "scheduling.kubenexus.io/gang-numa-spread",
		},
		{
			name:     "AnnotationNUMAAffinityNodeID",
			constant: AnnotationNUMAAffinityNodeID,
			expected: "scheduling.kubenexus.io/numa-affinity-node-id",
		},
		{
			name:     "AnnotationMemoryIntensive",
			constant: AnnotationMemoryIntensive,
			expected: "scheduling.kubenexus.io/memory-intensive",
		},
		{
			name:     "GangNUMASpreadPacked",
			constant: GangNUMASpreadPacked,
			expected: "packed",
		},
		{
			name:     "GangNUMASpreadBalanced",
			constant: GangNUMASpreadBalanced,
			expected: "balanced",
		},
		{
			name:     "GangNUMASpreadIsolated",
			constant: GangNUMASpreadIsolated,
			expected: "isolated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("%s = %s, want %s", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestScoringWeights(t *testing.T) {
	totalWeight := WeightNUMAFit + WeightMemoryBandwidth + WeightNUMADistance + WeightGangAffinity

	// Check weights sum to 1.0
	if totalWeight != 1.0 {
		t.Errorf("Total scoring weights = %.2f, want 1.0", totalWeight)
	}

	// Check individual weights
	if WeightNUMAFit != 0.40 {
		t.Errorf("WeightNUMAFit = %.2f, want 0.40", WeightNUMAFit)
	}

	if WeightMemoryBandwidth != 0.25 {
		t.Errorf("WeightMemoryBandwidth = %.2f, want 0.25", WeightMemoryBandwidth)
	}

	if WeightNUMADistance != 0.20 {
		t.Errorf("WeightNUMADistance = %.2f, want 0.20", WeightNUMADistance)
	}

	if WeightGangAffinity != 0.15 {
		t.Errorf("WeightGangAffinity = %.2f, want 0.15", WeightGangAffinity)
	}
}
