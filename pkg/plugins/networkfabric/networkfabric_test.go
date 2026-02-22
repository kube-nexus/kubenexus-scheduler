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

package networkfabric

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestName(t *testing.T) {
	plugin := &NetworkFabricScore{}
	if got := plugin.Name(); got != Name {
		t.Errorf("Name() = %v, want %v", got, Name)
	}
}

func TestScoreExtensions(t *testing.T) {
	plugin := &NetworkFabricScore{}
	if ext := plugin.ScoreExtensions(); ext != nil {
		t.Errorf("ScoreExtensions() = %v, want nil", ext)
	}
}

func TestConstants(t *testing.T) {
	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"Plugin name", Name, "NetworkFabricScore"},
		{"LabelFabricType", LabelFabricType, "network.kubenexus.io/fabric-type"},
		{"LabelFabricID", LabelFabricID, "network.kubenexus.io/fabric-id"},
		{"LabelRackID", LabelRackID, "network.kubenexus.io/rack-id"},
		{"LabelAZ", LabelAZ, "network.kubenexus.io/az"},
		{"ScoreNVSwitch", ScoreNVSwitch, 100},
		{"ScoreNVLink", ScoreNVLink, 90},
		{"ScoreInfiniBand", ScoreInfiniBand, 75},
		{"ScoreRoCE", ScoreRoCE, 60},
		{"ScoreEthernet", ScoreEthernet, 40},
		{"ScoreUnknown", ScoreUnknown, 50},
		{"BonusSameFabricDomain", BonusSameFabricDomain, 30},
		{"BonusSameRack", BonusSameRack, 20},
		{"BonusSameAZ", BonusSameAZ, 10},
		{"PenaltyCrossFabric", PenaltyCrossFabric, 30},
		{"PenaltyCrossRack", PenaltyCrossRack, 20},
		{"PenaltyCrossAZ", PenaltyCrossAZ, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestGetFabricType(t *testing.T) {
	tests := []struct {
		name       string
		labelValue string
		want       FabricType
	}{
		{"NVSwitch fabric", "nvswitch", FabricNVSwitch},
		{"NVSwitch uppercase", "NVSWITCH", FabricNVSwitch},
		{"NVLink fabric", "nvlink", FabricNVLink},
		{"InfiniBand full", "infiniband", FabricInfiniBand},
		{"InfiniBand short", "ib", FabricInfiniBand},
		{"RoCE fabric", "roce", FabricRoCE},
		{"RDMA alias", "rdma", FabricRoCE},
		{"Ethernet full", "ethernet", FabricEthernet},
		{"Ethernet short", "eth", FabricEthernet},
		{"Unknown fabric", "unknown-fabric", FabricUnknown},
		{"Empty label", "", FabricUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						LabelFabricType: tt.labelValue,
					},
				},
			}
			got := getFabricType(node)
			if got != tt.want {
				t.Errorf("getFabricType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetFabricTierScore(t *testing.T) {
	tests := []struct {
		name       string
		fabricType FabricType
		wantScore  int
	}{
		{"NVSwitch tier", FabricNVSwitch, ScoreNVSwitch},
		{"NVLink tier", FabricNVLink, ScoreNVLink},
		{"InfiniBand tier", FabricInfiniBand, ScoreInfiniBand},
		{"RoCE tier", FabricRoCE, ScoreRoCE},
		{"Ethernet tier", FabricEthernet, ScoreEthernet},
		{"Unknown tier", FabricUnknown, ScoreUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getFabricTierScore(tt.fabricType)
			if got != tt.wantScore {
				t.Errorf("getFabricTierScore(%v) = %d, want %d", tt.fabricType, got, tt.wantScore)
			}
		})
	}
}

func TestIsNetworkSensitive(t *testing.T) {
	tests := []struct {
		name           string
		annotationVal  string
		wantSensitive  bool
	}{
		{"Explicitly true", "true", true},
		{"Uppercase TRUE", "TRUE", true},
		{"Mixed case True", "True", true},
		{"Explicitly false", "false", false},
		{"Empty annotation", "", false},
		{"Invalid value", "yes", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationNetworkSensitive: tt.annotationVal,
					},
				},
			}
			got := isNetworkSensitive(pod)
			if got != tt.wantSensitive {
				t.Errorf("isNetworkSensitive() = %v, want %v", got, tt.wantSensitive)
			}
		})
	}
}

func TestGetMinFabricTier(t *testing.T) {
	tests := []struct {
		name          string
		annotationVal string
		want          FabricType
	}{
		{"Require NVSwitch", "nvswitch", FabricNVSwitch},
		{"Require NVLink", "nvlink", FabricNVLink},
		{"Require InfiniBand full", "infiniband", FabricInfiniBand},
		{"Require InfiniBand short", "ib", FabricInfiniBand},
		{"Require RoCE", "roce", FabricRoCE},
		{"Require Ethernet", "ethernet", FabricEthernet},
		{"No requirement", "", FabricUnknown},
		{"Invalid value", "quantum-link", FabricUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationMinFabricTier: tt.annotationVal,
					},
				},
			}
			got := getMinFabricTier(pod)
			if got != tt.want {
				t.Errorf("getMinFabricTier() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMeetsFabricTierRequirement(t *testing.T) {
	tests := []struct {
		name    string
		actual  FabricType
		minimum FabricType
		want    bool
	}{
		// NVSwitch meets all requirements
		{"NVSwitch >= NVSwitch", FabricNVSwitch, FabricNVSwitch, true},
		{"NVSwitch >= NVLink", FabricNVSwitch, FabricNVLink, true},
		{"NVSwitch >= InfiniBand", FabricNVSwitch, FabricInfiniBand, true},
		{"NVSwitch >= RoCE", FabricNVSwitch, FabricRoCE, true},
		{"NVSwitch >= Ethernet", FabricNVSwitch, FabricEthernet, true},
		
		// NVLink meets most requirements
		{"NVLink < NVSwitch", FabricNVLink, FabricNVSwitch, false},
		{"NVLink >= NVLink", FabricNVLink, FabricNVLink, true},
		{"NVLink >= InfiniBand", FabricNVLink, FabricInfiniBand, true},
		
		// InfiniBand mid-tier
		{"InfiniBand < NVSwitch", FabricInfiniBand, FabricNVSwitch, false},
		{"InfiniBand < NVLink", FabricInfiniBand, FabricNVLink, false},
		{"InfiniBand >= InfiniBand", FabricInfiniBand, FabricInfiniBand, true},
		{"InfiniBand >= RoCE", FabricInfiniBand, FabricRoCE, true},
		
		// RoCE lower tier
		{"RoCE < InfiniBand", FabricRoCE, FabricInfiniBand, false},
		{"RoCE >= RoCE", FabricRoCE, FabricRoCE, true},
		{"RoCE >= Ethernet", FabricRoCE, FabricEthernet, true},
		
		// Ethernet lowest tier
		{"Ethernet < RoCE", FabricEthernet, FabricRoCE, false},
		{"Ethernet >= Ethernet", FabricEthernet, FabricEthernet, true},
		
		// Unknown fabric
		{"Unknown < NVSwitch", FabricUnknown, FabricNVSwitch, false},
		{"Unknown >= Unknown", FabricUnknown, FabricUnknown, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := meetsFabricTierRequirement(tt.actual, tt.minimum)
			if got != tt.want {
				t.Errorf("meetsFabricTierRequirement(%v, %v) = %v, want %v",
					tt.actual, tt.minimum, got, tt.want)
			}
		})
	}
}

func TestCalculateLocalityScore(t *testing.T) {
	// Create fake nodes for testing - each with specific labels
	nodeFabricOnly := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-fabric",
			Labels: map[string]string{
				LabelFabricID: "fabric-01",
			},
		},
	}
	nodeRackOnly := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-rack",
			Labels: map[string]string{
				LabelRackID: "rack-a",
			},
		},
	}
	nodeAZOnly := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-az",
			Labels: map[string]string{
				LabelAZ: "us-west-1a",
			},
		},
	}

	tests := []struct {
		name              string
		gangPods          []*v1.Pod
		candidateFabricID string
		candidateRackID   string
		candidateAZ       string
		wantScore         int
	}{
		{
			name:              "No existing gang members",
			gangPods:          []*v1.Pod{},
			candidateFabricID: "fabric-01",
			candidateRackID:   "rack-a",
			candidateAZ:       "us-west-1a",
			wantScore:         0,
		},
		{
			name: "Same fabric domain - bonus",
			gangPods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-1",
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						NodeName: "node-fabric",
					},
				},
			},
			candidateFabricID: "fabric-01",
			candidateRackID:   "",
			candidateAZ:       "",
			wantScore:         BonusSameFabricDomain,
		},
		{
			name: "Same rack - bonus",
			gangPods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-1",
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						NodeName: "node-rack",
					},
				},
			},
			candidateFabricID: "",
			candidateRackID:   "rack-a",
			candidateAZ:       "",
			wantScore:         BonusSameRack,
		},
		{
			name: "Same AZ - bonus",
			gangPods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-1",
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						NodeName: "node-az",
					},
				},
			},
			candidateFabricID: "",
			candidateRackID:   "",
			candidateAZ:       "us-west-1a",
			wantScore:         BonusSameAZ,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create node lister with nodes for this test
			fakeNodeLister := &fakeNodeLister{
				nodes: map[string]*v1.Node{
					"node-fabric": nodeFabricOnly,
					"node-rack":   nodeRackOnly,
					"node-az":     nodeAZOnly,
				},
			}
			got := calculateLocalityScore(tt.gangPods, tt.candidateFabricID, tt.candidateRackID, tt.candidateAZ, fakeNodeLister)
			if got != tt.wantScore {
				t.Errorf("calculateLocalityScore() = %d, want %d", got, tt.wantScore)
			}
		})
	}
}

// fakeNodeLister implements a simple node lister for testing
type fakeNodeLister struct {
	nodes map[string]*v1.Node
}

func (f *fakeNodeLister) List(selector labels.Selector) ([]*v1.Node, error) {
	var result []*v1.Node
	for _, node := range f.nodes {
		result = append(result, node)
	}
	return result, nil
}

func (f *fakeNodeLister) Get(name string) (*v1.Node, error) {
	if node, ok := f.nodes[name]; ok {
		return node, nil
	}
	return nil, fmt.Errorf("node %s not found", name)
}

func TestFabricTypeStrings(t *testing.T) {
	tests := []struct {
		fabric FabricType
		want   string
	}{
		{FabricNVSwitch, "nvswitch"},
		{FabricNVLink, "nvlink"},
		{FabricInfiniBand, "infiniband"},
		{FabricRoCE, "roce"},
		{FabricEthernet, "ethernet"},
		{FabricUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(string(tt.fabric), func(t *testing.T) {
			if string(tt.fabric) != tt.want {
				t.Errorf("FabricType string = %v, want %v", tt.fabric, tt.want)
			}
		})
	}
}
