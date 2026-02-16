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

package scoring

import (
	"testing"
)

func TestHybridScorePluginName(t *testing.T) {
	plugin := &HybridScorePlugin{}
	if plugin.Name() != Name {
		t.Errorf("Expected plugin name %s, got %s", Name, plugin.Name())
	}
}

func TestHybridScoreConstants(t *testing.T) {
	if Name != "HybridScore" {
		t.Errorf("Expected Name to be 'HybridScore', got %s", Name)
	}
	
	if MaxNodeScore != 100 {
		t.Errorf("Expected MaxNodeScore to be 100, got %d", MaxNodeScore)
	}
}

func TestTopologySpreadPluginName(t *testing.T) {
	plugin := &TopologySpreadScorePlugin{}
	if plugin.Name() != PluginName {
		t.Errorf("Expected plugin name %s, got %s", PluginName, plugin.Name())
	}
}

func TestTopologyConstants(t *testing.T) {
	if PluginName != "TopologySpread" {
		t.Errorf("Expected PluginName to be 'TopologySpread', got %s", PluginName)
	}
	
	if ZoneLabel != "topology.kubernetes.io/zone" {
		t.Errorf("Expected ZoneLabel to be 'topology.kubernetes.io/zone', got %s", ZoneLabel)
	}
	
	if MaxScore != 100 {
		t.Errorf("Expected MaxScore to be 100, got %d", MaxScore)
	}
}

func TestScoreExtensions(t *testing.T) {
	// Both plugins should return nil for ScoreExtensions
	hybridPlugin := &HybridScorePlugin{}
	if hybridPlugin.ScoreExtensions() != nil {
		t.Error("HybridScorePlugin.ScoreExtensions() should return nil")
	}
	
	topoPlugin := &TopologySpreadScorePlugin{}
	if topoPlugin.ScoreExtensions() != nil {
		t.Error("TopologySpreadScorePlugin.ScoreExtensions() should return nil")
	}
}
