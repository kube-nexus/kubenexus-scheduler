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

package backfill

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBackfillScoringPluginName(t *testing.T) {
	plugin := &BackfillScoring{}
	expected := "BackfillScoring"
	if plugin.Name() != expected {
		t.Errorf("Expected plugin name %s, got %s", expected, plugin.Name())
	}
}

func TestBackfillScoringConstants(t *testing.T) {
	if Name != "BackfillScoring" {
		t.Errorf("Expected Name to be 'BackfillScoring', got %s", Name)
	}

	if BackfillPriorityThreshold != 100 {
		t.Errorf("Expected BackfillPriorityThreshold to be 100, got %d", BackfillPriorityThreshold)
	}

	if BackfillLabelKey != "scheduling.kubenexus.io/backfill" {
		t.Errorf("Expected BackfillLabelKey to be 'scheduling.kubenexus.io/backfill', got %s", BackfillLabelKey)
	}

	if MaxNodeScore != 100 {
		t.Errorf("Expected MaxNodeScore to be 100, got %d", MaxNodeScore)
	}
}

func TestScoreExtensions(t *testing.T) {
	plugin := &BackfillScoring{}
	if plugin.ScoreExtensions() != nil {
		t.Error("BackfillScoring.ScoreExtensions() should return nil")
	}
}

func TestIsBackfillEligible(t *testing.T) {
	plugin := &BackfillScoring{}

	tests := []struct {
		name     string
		pod      *v1.Pod
		expected bool
	}{
		{
			name: "Pod with backfill label true",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						BackfillLabelKey: "true",
					},
				},
			},
			expected: true,
		},
		{
			name: "Pod with backfill label false",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						BackfillLabelKey: "false",
					},
				},
			},
			expected: false,
		},
		{
			name: "Pod with low priority (50)",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Priority: int32Ptr(50),
				},
			},
			expected: true,
		},
		{
			name: "Pod with priority at threshold (100)",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Priority: int32Ptr(100),
				},
			},
			expected: true,
		},
		{
			name: "Pod with high priority (1000)",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Priority: int32Ptr(1000),
				},
			},
			expected: false,
		},
		{
			name: "Pod with no priority and no label",
			pod: &v1.Pod{
				Spec: v1.PodSpec{},
			},
			expected: false,
		},
		{
			name: "Pod with label takes precedence over priority",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						BackfillLabelKey: "true",
					},
				},
				Spec: v1.PodSpec{
					Priority: int32Ptr(1000), // High priority but label overrides
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := plugin.isBackfillEligible(tt.pod)
			if result != tt.expected {
				t.Errorf("isBackfillEligible() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func int32Ptr(i int32) *int32 {
	return &i
}
