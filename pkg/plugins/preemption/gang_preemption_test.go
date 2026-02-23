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

package preemption

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetTierPriority(t *testing.T) {
	tests := []struct {
		name     string
		tier     string
		expected int
	}{
		{"Gold tier", "gold", 3},
		{"Silver tier", "silver", 2},
		{"Bronze tier", "bronze", 1},
		{"Unknown tier", "unknown", 1},
		{"Empty tier", "", 1},
		{"Mixed case", "GOLD", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getTierPriority(tt.tier)
			if result != tt.expected {
				t.Errorf("getTierPriority(%q) = %d, want %d", tt.tier, result, tt.expected)
			}
		})
	}
}

func TestGetTenantTierFromPod(t *testing.T) {
	gp := &GangPreemption{}

	tests := []struct {
		name     string
		pod      *v1.Pod
		expected string
	}{
		{
			name: "Kueue label with gold",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						"kueue.x-k8s.io/queue-name": "gold-tier-queue",
					},
				},
			},
			expected: "gold",
		},
		{
			name: "Kueue label with silver",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						"kueue.x-k8s.io/queue-name": "silver-priority",
					},
				},
			},
			expected: "silver",
		},
		{
			name: "Namespace with gold",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "gold-tenant",
				},
			},
			expected: "gold",
		},
		{
			name: "Default to bronze",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			expected: "bronze",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gp.getTenantTierFromPod(tt.pod)
			if result != tt.expected {
				t.Errorf("getTenantTierFromPod() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestCalculateGangResourceNeeds(t *testing.T) {
	gp := &GangPreemption{}

	pod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:                    resource.MustParse("2"),
							v1.ResourceMemory:                 resource.MustParse("4Gi"),
							v1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	minAvailable := 4
	needs := gp.calculateGangResourceNeeds(pod, minAvailable)

	expectedCPU := int64(2000 * 4)
	expectedMemory := int64(4 * 1024 * 1024 * 1024 * 4)
	expectedGPU := int64(1 * 4)

	if needs.CPU != expectedCPU {
		t.Errorf("CPU needs = %d, want %d", needs.CPU, expectedCPU)
	}
	if needs.Memory != expectedMemory {
		t.Errorf("Memory needs = %d, want %d", needs.Memory, expectedMemory)
	}
	if needs.GPU != expectedGPU {
		t.Errorf("GPU needs = %d, want %d", needs.GPU, expectedGPU)
	}
}

func TestTenantTierPreemptionLogic(t *testing.T) {
	tests := []struct {
		name           string
		gangTier       string
		victimTier     string
		gangPriority   int32
		victimPriority int32
		shouldPreempt  bool
	}{
		{
			name:           "Gold can preempt bronze",
			gangTier:       "gold",
			victimTier:     "bronze",
			gangPriority:   100,
			victimPriority: 100,
			shouldPreempt:  true,
		},
		{
			name:           "Gold can preempt silver",
			gangTier:       "gold",
			victimTier:     "silver",
			gangPriority:   100,
			victimPriority: 100,
			shouldPreempt:  true,
		},
		{
			name:           "Silver can preempt bronze",
			gangTier:       "silver",
			victimTier:     "bronze",
			gangPriority:   100,
			victimPriority: 100,
			shouldPreempt:  true,
		},
		{
			name:           "Bronze cannot preempt silver",
			gangTier:       "bronze",
			victimTier:     "silver",
			gangPriority:   100,
			victimPriority: 100,
			shouldPreempt:  false,
		},
		{
			name:           "Same tier higher priority can preempt",
			gangTier:       "silver",
			victimTier:     "silver",
			gangPriority:   1000,
			victimPriority: 100,
			shouldPreempt:  true,
		},
		{
			name:           "Same tier equal priority cannot preempt",
			gangTier:       "silver",
			victimTier:     "silver",
			gangPriority:   100,
			victimPriority: 100,
			shouldPreempt:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gangTierPrio := getTierPriority(tt.gangTier)
			victimTierPrio := getTierPriority(tt.victimTier)

			canPreempt := false
			if gangTierPrio > victimTierPrio {
				canPreempt = true
			} else if gangTierPrio == victimTierPrio && tt.gangPriority > tt.victimPriority {
				canPreempt = true
			}

			if canPreempt != tt.shouldPreempt {
				t.Errorf("Preemption logic mismatch: got %v, want %v", canPreempt, tt.shouldPreempt)
			}
		})
	}
}
