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

package topologyspread

import (
	"testing"
)

func TestTopologySpreadPluginName(t *testing.T) {
	plugin := &TopologySpread{}
	expected := "TopologySpreadScoring"
	if plugin.Name() != expected {
		t.Errorf("Expected plugin name %s, got %s", expected, plugin.Name())
	}
}

func TestTopologySpreadConstants(t *testing.T) {
	if Name != "TopologySpreadScoring" {
		t.Errorf("Expected Name to be 'TopologySpreadScoring', got %s", Name)
	}

	if ZoneLabel != "topology.kubernetes.io/zone" {
		t.Errorf("Expected ZoneLabel to be 'topology.kubernetes.io/zone', got %s", ZoneLabel)
	}

	if MaxScore != 100 {
		t.Errorf("Expected MaxScore to be 100, got %d", MaxScore)
	}
}

func TestScoreExtensions(t *testing.T) {
	plugin := &TopologySpread{}
	if plugin.ScoreExtensions() != nil {
		t.Error("TopologySpread.ScoreExtensions() should return nil")
	}
}
