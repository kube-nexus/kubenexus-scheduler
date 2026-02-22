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

package utils

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestGetPodGroupLabels tests extraction of pod group labels
func TestGetPodGroupLabels(t *testing.T) {
	tests := []struct {
		name             string
		pod              *v1.Pod
		wantGroupName    string
		wantMinAvailable int
		wantErr          bool
	}{
		{
			name: "valid gang pod",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"pod-group.scheduling.sigs.k8s.io/name":          "training-job",
						"pod-group.scheduling.sigs.k8s.io/min-available": "4",
					},
				},
			},
			wantGroupName:    "training-job",
			wantMinAvailable: 4,
			wantErr:          false,
		},
		{
			name: "independent pod (no labels)",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
				},
			},
			wantGroupName:    "",
			wantMinAvailable: 0,
			wantErr:          false,
		},
		{
			name: "pod with group name but no minAvailable",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"pod-group.scheduling.sigs.k8s.io/name": "service-group",
					},
				},
			},
			wantGroupName:    "",
			wantMinAvailable: 0,
			wantErr:          false, // Returns empty when minAvailable is missing
		},
		{
			name: "invalid minAvailable value",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"pod-group.scheduling.sigs.k8s.io/name":          "bad-group",
						"pod-group.scheduling.sigs.k8s.io/min-available": "invalid",
					},
				},
			},
			wantGroupName:    "",
			wantMinAvailable: 0,
			wantErr:          true,
		},
		{
			name: "zero minAvailable",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"pod-group.scheduling.sigs.k8s.io/name":          "zero-group",
						"pod-group.scheduling.sigs.k8s.io/min-available": "0",
					},
				},
			},
			wantGroupName:    "",
			wantMinAvailable: 0,
			wantErr:          true, // minAvailable must be >= 1
		},
		{
			name: "large gang (256 pods)",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"pod-group.scheduling.sigs.k8s.io/name":          "large-training",
						"pod-group.scheduling.sigs.k8s.io/min-available": "256",
					},
				},
			},
			wantGroupName:    "large-training",
			wantMinAvailable: 256,
			wantErr:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotGroupName, gotMinAvailable, err := GetPodGroupLabels(tt.pod)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetPodGroupLabels() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if gotGroupName != tt.wantGroupName {
				t.Errorf("GetPodGroupLabels() groupName = %v, want %v", gotGroupName, tt.wantGroupName)
			}

			if gotMinAvailable != tt.wantMinAvailable {
				t.Errorf("GetPodGroupLabels() minAvailable = %v, want %v", gotMinAvailable, tt.wantMinAvailable)
			}
		})
	}
}

// TestGetPodGroupLabels_NilPod tests handling of nil pod
func TestGetPodGroupLabels_NilPod(t *testing.T) {
	_, _, err := GetPodGroupLabels(nil)
	if err == nil {
		t.Error("GetPodGroupLabels() with nil pod should return error")
	}
}

// BenchmarkGetPodGroupLabels benchmarks label extraction performance
func BenchmarkGetPodGroupLabels(b *testing.B) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"pod-group.scheduling.sigs.k8s.io/name":          "bench-group",
				"pod-group.scheduling.sigs.k8s.io/min-available": "8",
			},
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _, _ = GetPodGroupLabels(pod) //nolint:errcheck // Benchmark intentionally ignores errors
	}
}
