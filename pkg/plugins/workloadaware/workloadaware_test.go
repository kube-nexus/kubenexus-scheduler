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

package workloadaware

import (
	"testing"
)

func TestWorkloadAwarePluginName(t *testing.T) {
	plugin := &WorkloadAware{}
	expected := "WorkloadAwareScoring"
	if plugin.Name() != expected {
		t.Errorf("Expected plugin name %s, got %s", expected, plugin.Name())
	}
}

func TestWorkloadAwareConstants(t *testing.T) {
	if Name != "WorkloadAwareScoring" {
		t.Errorf("Expected Name to be 'WorkloadAwareScoring', got %s", Name)
	}

	if MaxNodeScore != 100 {
		t.Errorf("Expected MaxNodeScore to be 100, got %d", MaxNodeScore)
	}
}

func TestScoreExtensions(t *testing.T) {
	plugin := &WorkloadAware{}
	if plugin.ScoreExtensions() != nil {
		t.Error("WorkloadAware.ScoreExtensions() should return nil")
	}
}
