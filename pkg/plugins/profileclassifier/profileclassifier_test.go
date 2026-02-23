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

package profileclassifier

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	st "k8s.io/kubernetes/pkg/scheduler/testing"
)

func TestProfileClassifier_PreFilter(t *testing.T) {
	t.Skip("Integration test - requires full scheduler framework setup")
}

func TestTenantTierParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected TenantTier
	}{
		{"gold", TierGold},
		{"Gold", TierGold},
		{"GOLD", TierGold},
		{"silver", TierSilver},
		{"bronze", TierBronze},
		{"unknown", TierUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := parseTenantTier(tt.input); got != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, got)
			}
		})
	}
}

func TestWorkloadTypeParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected WorkloadType
	}{
		{"training", WorkloadTraining},
		{"Training", WorkloadTraining},
		{"inference", WorkloadInference},
		{"batch", WorkloadBatch},
		{"service", WorkloadService},
		{"interactive", WorkloadInteractive},
		{"unknown", WorkloadUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := parseWorkloadType(tt.input); got != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, got)
			}
		})
	}
}

func TestGangDetection(t *testing.T) {
	tests := []struct {
		name     string
		pod      *v1.Pod
		expected bool
	}{
		{
			name: "gang annotation",
			pod: st.MakePod().
				Annotation("scheduling.kubenexus.io/min-available", "4").Obj(),
			expected: true,
		},
		{
			name:     "no gang",
			pod:      st.MakePod().Obj(),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGangPod(tt.pod); got != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestGetGPURequest(t *testing.T) {
	tests := []struct {
		name     string
		pod      *v1.Pod
		expected int64
	}{
		{
			name:     "single GPU",
			pod:      st.MakePod().Req(map[v1.ResourceName]string{"nvidia.com/gpu": "1"}).Obj(),
			expected: 1,
		},
		{
			name:     "no GPU",
			pod:      st.MakePod().Obj(),
			expected: 0,
		},
		{
			name: "multi-container GPUs",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									"nvidia.com/gpu": resource.MustParse("2"),
								},
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									"nvidia.com/gpu": resource.MustParse("2"),
								},
							},
						},
					},
				},
			},
			expected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getGPURequest(tt.pod); got != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, got)
			}
		})
	}
}

func ptrInt32(i int32) *int32 {
	return &i
}
