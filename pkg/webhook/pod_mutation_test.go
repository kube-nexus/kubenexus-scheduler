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

package webhook

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestGetTierGPUClass(t *testing.T) {
	tests := []struct {
		tier     string
		expected string
	}{
		{"gold", GPUClassH100},
		{"Gold", GPUClassH100},
		{"GOLD", GPUClassH100},
		{"silver", GPUClassA100},
		{"Silver", GPUClassA100},
		{"bronze", GPUClassL4},
		{"Bronze", GPUClassL4},
		{"unknown", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.tier, func(t *testing.T) {
			result := getTierGPUClass(tt.tier)
			if result != tt.expected {
				t.Errorf("getTierGPUClass(%q) = %q, want %q", tt.tier, result, tt.expected)
			}
		})
	}
}

func TestRequestsGPUs(t *testing.T) {
	tests := []struct {
		name     string
		pod      *v1.Pod
		expected bool
	}{
		{
			name: "Pod with nvidia GPU request",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									"nvidia.com/gpu": resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Pod without GPU",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									"cpu":    resource.MustParse("1"),
									"memory": resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := requestsGPUs(tt.pod)
			if result != tt.expected {
				t.Errorf("requestsGPUs() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestHasGPUClassNodeSelector(t *testing.T) {
	tests := []struct {
		name     string
		pod      *v1.Pod
		expected bool
	}{
		{
			name: "Pod with GPU class nodeSelector",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					NodeSelector: map[string]string{
						GPUClassLabel: "h100",
					},
				},
			},
			expected: true,
		},
		{
			name: "Pod without GPU class nodeSelector",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					NodeSelector: map[string]string{
						"disktype": "ssd",
					},
				},
			},
			expected: false,
		},
		{
			name: "Pod without nodeSelector",
			pod: &v1.Pod{
				Spec: v1.PodSpec{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasGPUClassNodeSelector(tt.pod)
			if result != tt.expected {
				t.Errorf("hasGPUClassNodeSelector() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestEscapeJSONPointer(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"gpu.nvidia.com/class", "gpu.nvidia.com~1class"},
		{"simple", "simple"},
		{"path/with/slash", "path~1with~1slash"},
		{"tilde~test", "tilde~0test"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := escapeJSONPointer(tt.input)
			if result != tt.expected {
				t.Errorf("escapeJSONPointer(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
