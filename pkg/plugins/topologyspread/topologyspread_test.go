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

	framework "k8s.io/kube-scheduler/framework"
)

func TestName(t *testing.T) {
	plugin := &TopologySpreadScorePlugin{}
	if got := plugin.Name(); got != TopologyScoringName {
		t.Errorf("Name() = %v, want %v", got, TopologyScoringName)
	}
}

func TestScoreExtensions(t *testing.T) {
	plugin := &TopologySpreadScorePlugin{}
	if got := plugin.ScoreExtensions(); got != nil {
		t.Errorf("ScoreExtensions() = %v, want nil", got)
	}
}

func TestConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      interface{}
		want     interface{}
	}{
		{
			name: "TopologyScoringName",
			got:  TopologyScoringName,
			want: "TopologyScoring",
		},
		{
			name: "MaxScore",
			got:  MaxScore,
			want: framework.MaxNodeScore,
		},
		{
			name: "ZoneLabel",
			got:  ZoneLabel,
			want: "topology.kubernetes.io/zone",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

