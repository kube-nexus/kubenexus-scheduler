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

package vramscheduler

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"

	testutil "github.com/kube-nexus/kubenexus-scheduler/test/util"
)

const (
	ResourceGPU = v1.ResourceName("nvidia.com/gpu")
	GiB         = 1024 * 1024 * 1024 // 1 GiB in bytes
)

func TestName(t *testing.T) {
	plugin := &VRAMScheduler{}
	if plugin.Name() != Name {
		t.Errorf("Expected plugin name %s, got %s", Name, plugin.Name())
	}
}

func TestScoreExtensions(t *testing.T) {
	plugin := &VRAMScheduler{}
	if plugin.ScoreExtensions() != nil {
		t.Error("Expected nil ScoreExtensions")
	}
}

func TestGetVRAMRequest(t *testing.T) {
	tests := []struct {
		name           string
		annotations    map[string]string
		resourceClaims []v1.PodResourceClaim
		expectedVRAM   int64
	}{
		{
			name:         "80Gi VRAM request for 70B model (annotation)",
			annotations:  map[string]string{AnnotationVRAMRequest: "80Gi"},
			expectedVRAM: 80 * 1024 * 1024 * 1024,
		},
		{
			name:         "24Gi VRAM request for 7B model (annotation)",
			annotations:  map[string]string{AnnotationVRAMRequest: "24Gi"},
			expectedVRAM: 24 * 1024 * 1024 * 1024,
		},
		{
			name: "DRA ResourceClaim with annotation hint",
			resourceClaims: []v1.PodResourceClaim{
				{Name: "gpu-claim"},
			},
			annotations:  map[string]string{AnnotationVRAMRequest: "40Gi"},
			expectedVRAM: 40 * 1024 * 1024 * 1024,
		},
		{
			name: "DRA ResourceClaim with nvidia pattern",
			resourceClaims: []v1.PodResourceClaim{
				{Name: "nvidia-gpu-0"},
			},
			annotations:  map[string]string{AnnotationVRAMRequest: "80Gi"},
			expectedVRAM: 80 * 1024 * 1024 * 1024,
		},
		{
			name: "DRA ResourceClaim with accelerator pattern",
			resourceClaims: []v1.PodResourceClaim{
				{Name: "accelerator-claim"},
			},
			annotations:  map[string]string{AnnotationVRAMRequest: "48Gi"},
			expectedVRAM: 48 * 1024 * 1024 * 1024,
		},
		{
			name:         "No VRAM request",
			annotations:  map[string]string{},
			expectedVRAM: 0,
		},
		{
			name:         "Invalid VRAM format",
			annotations:  map[string]string{AnnotationVRAMRequest: "invalid"},
			expectedVRAM: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Namespace:   "default",
					Annotations: tt.annotations,
				},
				Spec: v1.PodSpec{
					ResourceClaims: tt.resourceClaims,
				},
			}
			vram := getVRAMRequest(pod)
			if vram != tt.expectedVRAM {
				t.Errorf("Expected VRAM %d bytes, got %d bytes", tt.expectedVRAM, vram)
			}
		})
	}
}

func TestInferVRAMFromModel(t *testing.T) {
	tests := []struct {
		model    string
		expected int64
	}{
		{"H100", 80 * 1024 * 1024 * 1024},
		{"H100-80GB", 80 * 1024 * 1024 * 1024},
		{"H200", 141 * 1024 * 1024 * 1024},
		{"A100-80GB", 80 * 1024 * 1024 * 1024},
		{"A100", 40 * 1024 * 1024 * 1024},
		{"A40", 48 * 1024 * 1024 * 1024},
		{"L40S", 48 * 1024 * 1024 * 1024},
		{"L4", 24 * 1024 * 1024 * 1024},
		{"T4", 16 * 1024 * 1024 * 1024},
		{"V100", 32 * 1024 * 1024 * 1024},
		{"UNKNOWN-GPU", 0},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			vram := inferVRAMFromModel(tt.model)
			if vram != tt.expected {
				t.Errorf("Model %s: expected %d bytes, got %d bytes", tt.model, tt.expected, vram)
			}
		})
	}
}

func TestIsHighEndGPU(t *testing.T) {
	tests := []struct {
		name      string
		gpuModel  string
		isHighEnd bool
	}{
		{"H100 is high-end", "H100", true},
		{"H200 is high-end", "H200", true},
		{"A100-80GB is high-end", "A100-80GB", true},
		{"MI300 is high-end", "MI300", true},
		{"A100 (40GB) is not high-end", "A100", false},
		{"L40S is not high-end", "L40S", false},
		{"T4 is not high-end", "T4", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := testutil.MakeNode("test-node", map[string]string{
				LabelGPUModel: tt.gpuModel,
			}, v1.ResourceList{})
			result := isHighEndGPU(node)
			if result != tt.isHighEnd {
				t.Errorf("Expected isHighEnd=%v, got %v", tt.isHighEnd, result)
			}
		})
	}
}

func TestCalculateUtilizationScore(t *testing.T) {
	tests := []struct {
		name        string
		utilization float64
		expected    int64
	}{
		{"Perfect fit 100%", 1.00, ScorePerfectFit},
		{"Perfect fit 96%", 0.96, ScorePerfectFit},
		{"Good fit 90%", 0.90, ScoreGoodFit},
		{"Good fit 70%", 0.70, ScoreGoodFit},
		{"Acceptable 60%", 0.60, ScoreAcceptableFit},
		{"Acceptable 50%", 0.50, ScoreAcceptableFit},
		{"Poor fit 40%", 0.40, ScorePoorFit},
		{"Poor fit 30%", 0.30, ScorePoorFit},
		{"Insufficient 20%", 0.20, ScoreInsufficientVRAM},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := calculateUtilizationScore(tt.utilization, "silver")
			if score != tt.expected {
				t.Errorf("Expected score %d for %.0f%% utilization, got %d",
					tt.expected, tt.utilization*100, score)
			}
		})
	}
}

func TestGetNodeGPUVRAMFromLabels(t *testing.T) {
	tests := []struct {
		name         string
		labels       map[string]string
		expectedVRAM int64
		expectedGPUs int
	}{
		{
			name: "Explicit VRAM label with GPU count",
			labels: map[string]string{
				LabelGPUVRAM:  "80Gi",
				LabelGPUCount: "8",
			},
			expectedVRAM: 80 * 1024 * 1024 * 1024,
			expectedGPUs: 8,
		},
		{
			name: "Inferred from H100 model",
			labels: map[string]string{
				LabelGPUModel: "H100",
				LabelGPUCount: "8",
			},
			expectedVRAM: 80 * 1024 * 1024 * 1024,
			expectedGPUs: 8,
		},
		{
			name: "Inferred from T4 model",
			labels: map[string]string{
				LabelGPUModel: "T4",
				LabelGPUCount: "4",
			},
			expectedVRAM: 16 * 1024 * 1024 * 1024,
			expectedGPUs: 4,
		},
		{
			name:         "No VRAM information",
			labels:       map[string]string{},
			expectedVRAM: 0,
			expectedGPUs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := testutil.MakeNode("test-node", tt.labels, v1.ResourceList{})
			vram, gpus := getNodeGPUVRAMFromLabels(node)
			if vram != tt.expectedVRAM {
				t.Errorf("Expected VRAM %d, got %d", tt.expectedVRAM, vram)
			}
			if gpus != tt.expectedGPUs {
				t.Errorf("Expected %d GPUs, got %d", tt.expectedGPUs, gpus)
			}
		})
	}
}

func TestIsGPUDriver(t *testing.T) {
	tests := []struct {
		driver   string
		expected bool
	}{
		{"gpu.example.com", true},
		{"nvidia.com/gpu", true},
		{"amd.com/gpu", true},
		{"intel.com/gpu", true},
		{"accelerator.example.com", true},
		{"cpu.example.com", false},
		{"memory.driver", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.driver, func(t *testing.T) {
			result := isGPUDriver(tt.driver)
			if result != tt.expected {
				t.Errorf("isGPUDriver(%q) = %v, expected %v", tt.driver, result, tt.expected)
			}
		})
	}
}

func TestFilterWithFramework(t *testing.T) {
	nodes := []*v1.Node{
		testutil.MakeNode("h100-node", map[string]string{
			LabelGPUModel: "H100",
			LabelGPUVRAM:  "80Gi",
			LabelGPUCount: "8",
		}, v1.ResourceList{ResourceGPU: resource.MustParse("8")}),
		testutil.MakeNode("l40s-node", map[string]string{
			LabelGPUModel: "L40S",
			LabelGPUVRAM:  "48Gi",
			LabelGPUCount: "4",
		}, v1.ResourceList{ResourceGPU: resource.MustParse("4")}),
		testutil.MakeNode("t4-node", map[string]string{
			LabelGPUModel: "T4",
			LabelGPUVRAM:  "16Gi",
			LabelGPUCount: "4",
		}, v1.ResourceList{ResourceGPU: resource.MustParse("4")}),
	}

	tests := []struct {
		name         string
		vramRequest  string
		expectedPass map[string]bool
	}{
		{
			name:        "70B model needs 80GB VRAM",
			vramRequest: "80Gi",
			expectedPass: map[string]bool{
				"h100-node": true,
				"l40s-node": false,
				"t4-node":   false,
			},
		},
		{
			name:        "13B model needs 40GB VRAM",
			vramRequest: "40Gi",
			expectedPass: map[string]bool{
				"h100-node": true,
				"l40s-node": true,
				"t4-node":   false,
			},
		},
		{
			name:        "7B model needs 15GB VRAM",
			vramRequest: "15Gi",
			expectedPass: map[string]bool{
				"h100-node": true,
				"l40s-node": true,
				"t4-node":   true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "llm-pod",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationVRAMRequest: tt.vramRequest,
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								ResourceGPU: resource.MustParse("1"),
							},
						},
					}},
				},
			}

			fh, err := testutil.NewTestFramework(nil,
				frameworkruntime.WithSnapshotSharedLister(testutil.NewFakeSharedLister(nil, nodes)))
			if err != nil {
				t.Fatalf("Failed to create framework: %v", err)
			}

			plugin, err := New(context.Background(), nil, fh)
			if err != nil {
				t.Fatalf("Failed to create plugin: %v", err)
			}

			filterPlugin, ok := plugin.(fwk.FilterPlugin)
			if !ok {
				t.Fatal("Plugin does not implement FilterPlugin interface")
			}

			state := framework.NewCycleState()

			for _, node := range nodes {
				nodeInfo, err := fh.SnapshotSharedLister().NodeInfos().Get(node.Name)
				if err != nil {
					t.Fatalf("Failed to get node %s: %v", node.Name, err)
				}
				status := filterPlugin.Filter(context.Background(), state, pod, nodeInfo)

				shouldPass := tt.expectedPass[node.Name]
				didPass := status.IsSuccess()

				if shouldPass != didPass {
					t.Errorf("Node %s: expected pass=%v, got pass=%v (status: %v)",
						node.Name, shouldPass, didPass, status.Message())
				}
			}
		})
	}
}

func TestScoreWithFramework(t *testing.T) {
	nodes := []*v1.Node{
		testutil.MakeNode("h100-node", map[string]string{
			LabelGPUModel: "H100",
			LabelGPUVRAM:  "80Gi",
		}, v1.ResourceList{ResourceGPU: resource.MustParse("8")}),
		testutil.MakeNode("l40s-node", map[string]string{
			LabelGPUModel: "L40S",
			LabelGPUVRAM:  "48Gi",
		}, v1.ResourceList{ResourceGPU: resource.MustParse("4")}),
		testutil.MakeNode("a100-node", map[string]string{
			LabelGPUModel: "A100",
			LabelGPUVRAM:  "40Gi",
		}, v1.ResourceList{ResourceGPU: resource.MustParse("8")}),
	}

	tests := []struct {
		name        string
		vramRequest string
		description string
	}{
		{
			name:        "Perfect fit: 76Gi on 80Gi GPU (95% utilization)",
			vramRequest: "76Gi",
			description: "Should score 100 on H100, 0 on others (insufficient)",
		},
		{
			name:        "Good fit: 40Gi on 48Gi GPU (83% utilization)",
			vramRequest: "40Gi",
			description: "L40S should score higher than H100 (better utilization)",
		},
		{
			name:        "Poor fit: 10Gi wastes VRAM",
			vramRequest: "10Gi",
			description: "All nodes score low due to poor utilization",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						AnnotationVRAMRequest: tt.vramRequest,
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{{
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								ResourceGPU: resource.MustParse("1"),
							},
						},
					}},
				},
			}

			fh, err := testutil.NewTestFramework(nil,
				frameworkruntime.WithSnapshotSharedLister(testutil.NewFakeSharedLister(nil, nodes)))
			if err != nil {
				t.Fatalf("Failed to create framework: %v", err)
			}

			plugin, err := New(context.Background(), nil, fh)
			if err != nil {
				t.Fatalf("Failed to create plugin: %v", err)
			}

			scorePlugin, ok := plugin.(fwk.ScorePlugin)
			if !ok {
				t.Fatal("Plugin does not implement ScorePlugin interface")
			}

			state := framework.NewCycleState()

			t.Logf("Test: %s", tt.description)
			for _, node := range nodes {
				nodeInfo, err := fh.SnapshotSharedLister().NodeInfos().Get(node.Name)
				if err != nil {
					t.Fatalf("Failed to get node %s: %v", node.Name, err)
				}
				score, status := scorePlugin.Score(context.Background(), state, pod, nodeInfo)
				if !status.IsSuccess() {
					t.Logf("  %s: score failed - %v", node.Name, status.AsError())
				} else {
					t.Logf("  %s: score %d", node.Name, score)
				}
			}
		})
	}
}

// Test GPU topology extraction and bonus calculation
func TestGPUTopologyBonus(t *testing.T) {
	plugin := &VRAMScheduler{}

	tests := []struct {
		name          string
		gpuDevices    []GPUDevice
		gpusRequested int
		expectedBonus int64
		expectNUMA    bool
		expectNVLink  bool
		expectPCIe    bool
	}{
		{
			name: "Single GPU - no bonus",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", VRAM: 80 * GiB, NUMANode: 0},
			},
			gpusRequested: 1,
			expectedBonus: 0,
			expectNUMA:    true, // Single GPU on NUMA 0 counts as locality
			expectNVLink:  false,
			expectPCIe:    false,
		},
		{
			name: "2 GPUs on same NUMA node",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", VRAM: 80 * GiB, NUMANode: 0, NVLinkDomain: -1},
				{Name: "gpu-1", VRAM: 80 * GiB, NUMANode: 0, NVLinkDomain: -1},
			},
			gpusRequested: 2,
			expectedBonus: BonusGPUNUMALocality,
			expectNUMA:    true,
			expectNVLink:  false, // Explicitly set NVLinkDomain to -1
			expectPCIe:    false,
		},
		{
			name: "2 GPUs with NVLink connectivity",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", VRAM: 80 * GiB, NUMANode: -1, NVLinkPeers: []string{"gpu-1"}, NVLinkDomain: 0},
				{Name: "gpu-1", VRAM: 80 * GiB, NUMANode: -1, NVLinkPeers: []string{"gpu-0"}, NVLinkDomain: 0},
			},
			gpusRequested: 2,
			expectedBonus: BonusNVLinkConnected,
			expectNUMA:    false, // Set NUMA to -1 (unknown)
			expectNVLink:  true,
			expectPCIe:    false,
		},
		{
			name: "4 GPUs on same PCIe switch",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", VRAM: 80 * GiB, NUMANode: -1, NVLinkDomain: -1, PCIeSwitch: "switch-0"},
				{Name: "gpu-1", VRAM: 80 * GiB, NUMANode: -1, NVLinkDomain: -1, PCIeSwitch: "switch-0"},
				{Name: "gpu-2", VRAM: 80 * GiB, NUMANode: -1, NVLinkDomain: -1, PCIeSwitch: "switch-0"},
				{Name: "gpu-3", VRAM: 80 * GiB, NUMANode: -1, NVLinkDomain: -1, PCIeSwitch: "switch-0"},
			},
			gpusRequested: 4,
			expectedBonus: BonusPCIeLocality,
			expectNUMA:    false, // Set NUMA to -1
			expectNVLink:  false, // Set NVLink to -1
			expectPCIe:    true,
		},
		{
			name: "8 GPUs - perfect topology (NUMA + NVLink + PCIe)",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", VRAM: 80 * GiB, NUMANode: 0, NVLinkDomain: 0, PCIeSwitch: "switch-0"},
				{Name: "gpu-1", VRAM: 80 * GiB, NUMANode: 0, NVLinkDomain: 0, PCIeSwitch: "switch-0"},
				{Name: "gpu-2", VRAM: 80 * GiB, NUMANode: 0, NVLinkDomain: 0, PCIeSwitch: "switch-0"},
				{Name: "gpu-3", VRAM: 80 * GiB, NUMANode: 0, NVLinkDomain: 0, PCIeSwitch: "switch-0"},
				{Name: "gpu-4", VRAM: 80 * GiB, NUMANode: 0, NVLinkDomain: 0, PCIeSwitch: "switch-0"},
				{Name: "gpu-5", VRAM: 80 * GiB, NUMANode: 0, NVLinkDomain: 0, PCIeSwitch: "switch-0"},
				{Name: "gpu-6", VRAM: 80 * GiB, NUMANode: 0, NVLinkDomain: 0, PCIeSwitch: "switch-0"},
				{Name: "gpu-7", VRAM: 80 * GiB, NUMANode: 0, NVLinkDomain: 0, PCIeSwitch: "switch-0"},
			},
			gpusRequested: 8,
			expectedBonus: BonusGPUNUMALocality + BonusNVLinkConnected + BonusPCIeLocality,
			expectNUMA:    true,
			expectNVLink:  true,
			expectPCIe:    true,
		},
		{
			name: "4 GPUs split across NUMA nodes (no NUMA bonus)",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", VRAM: 80 * GiB, NUMANode: 0, NVLinkDomain: -1},
				{Name: "gpu-1", VRAM: 80 * GiB, NUMANode: 0, NVLinkDomain: -1},
				{Name: "gpu-2", VRAM: 80 * GiB, NUMANode: 1, NVLinkDomain: -1},
				{Name: "gpu-3", VRAM: 80 * GiB, NUMANode: 1, NVLinkDomain: -1},
			},
			gpusRequested: 4,
			expectedBonus: 0,
			expectNUMA:    false, // 2 per NUMA node, need 4
			expectNVLink:  false, // No NVLink
			expectPCIe:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
			}

			// Test individual topology checks
			numaLocality := plugin.checkGPUNUMALocality(tt.gpuDevices, tt.gpusRequested)
			if numaLocality != tt.expectNUMA {
				t.Errorf("NUMA locality: expected %v, got %v", tt.expectNUMA, numaLocality)
			}

			nvlinkConnectivity := plugin.checkNVLinkConnectivity(tt.gpuDevices, tt.gpusRequested)
			if nvlinkConnectivity != tt.expectNVLink {
				t.Errorf("NVLink connectivity: expected %v, got %v", tt.expectNVLink, nvlinkConnectivity)
			}

			pcieLocality := plugin.checkPCIeLocality(tt.gpuDevices, tt.gpusRequested)
			if pcieLocality != tt.expectPCIe {
				t.Errorf("PCIe locality: expected %v, got %v", tt.expectPCIe, pcieLocality)
			}

			// Test total bonus calculation
			totalBonus := plugin.calculateGPUTopologyBonus(tt.gpuDevices, tt.gpusRequested, pod)
			if totalBonus != tt.expectedBonus {
				t.Errorf("Total bonus: expected %d, got %d", tt.expectedBonus, totalBonus)
			}

			t.Logf("✓ %s: bonus=%d (NUMA=%v, NVLink=%v, PCIe=%v)",
				tt.name, totalBonus, numaLocality, nvlinkConnectivity, pcieLocality)
		})
	}
}

// Test GPU NUMA locality detection
func TestCheckGPUNUMALocality(t *testing.T) {
	plugin := &VRAMScheduler{}

	tests := []struct {
		name          string
		gpuDevices    []GPUDevice
		gpusRequested int
		expected      bool
	}{
		{
			name: "4 GPUs all on NUMA node 0",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", NUMANode: 0},
				{Name: "gpu-1", NUMANode: 0},
				{Name: "gpu-2", NUMANode: 0},
				{Name: "gpu-3", NUMANode: 0},
			},
			gpusRequested: 4,
			expected:      true,
		},
		{
			name: "8 GPUs split: 4 on NUMA 0, 4 on NUMA 1 (request 4)",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", NUMANode: 0},
				{Name: "gpu-1", NUMANode: 0},
				{Name: "gpu-2", NUMANode: 0},
				{Name: "gpu-3", NUMANode: 0},
				{Name: "gpu-4", NUMANode: 1},
				{Name: "gpu-5", NUMANode: 1},
				{Name: "gpu-6", NUMANode: 1},
				{Name: "gpu-7", NUMANode: 1},
			},
			gpusRequested: 4,
			expected:      true, // Can satisfy with either NUMA node
		},
		{
			name: "8 GPUs split: 3 on NUMA 0, 5 on NUMA 1 (request 4)",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", NUMANode: 0},
				{Name: "gpu-1", NUMANode: 0},
				{Name: "gpu-2", NUMANode: 0},
				{Name: "gpu-3", NUMANode: 1},
				{Name: "gpu-4", NUMANode: 1},
				{Name: "gpu-5", NUMANode: 1},
				{Name: "gpu-6", NUMANode: 1},
				{Name: "gpu-7", NUMANode: 1},
			},
			gpusRequested: 4,
			expected:      true, // NUMA node 1 has 5 GPUs
		},
		{
			name: "4 GPUs scattered (request 4)",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", NUMANode: 0},
				{Name: "gpu-1", NUMANode: 1},
				{Name: "gpu-2", NUMANode: 2},
				{Name: "gpu-3", NUMANode: 3},
			},
			gpusRequested: 4,
			expected:      false, // No single NUMA node has 4 GPUs
		},
		{
			name: "GPUs with unknown NUMA (-1)",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", NUMANode: -1},
				{Name: "gpu-1", NUMANode: -1},
			},
			gpusRequested: 2,
			expected:      false, // Unknown NUMA nodes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := plugin.checkGPUNUMALocality(tt.gpuDevices, tt.gpusRequested)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// Test NVLink connectivity detection
func TestCheckNVLinkConnectivity(t *testing.T) {
	plugin := &VRAMScheduler{}

	tests := []struct {
		name          string
		gpuDevices    []GPUDevice
		gpusRequested int
		expected      bool
	}{
		{
			name: "2 GPUs with NVLink domain 0",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", NVLinkDomain: 0},
				{Name: "gpu-1", NVLinkDomain: 0},
			},
			gpusRequested: 2,
			expected:      true,
		},
		{
			name: "8 GPUs - 4 in domain 0, 4 in domain 1 (request 4)",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", NVLinkDomain: 0},
				{Name: "gpu-1", NVLinkDomain: 0},
				{Name: "gpu-2", NVLinkDomain: 0},
				{Name: "gpu-3", NVLinkDomain: 0},
				{Name: "gpu-4", NVLinkDomain: 1},
				{Name: "gpu-5", NVLinkDomain: 1},
				{Name: "gpu-6", NVLinkDomain: 1},
				{Name: "gpu-7", NVLinkDomain: 1},
			},
			gpusRequested: 4,
			expected:      true, // Either domain satisfies
		},
		{
			name: "4 GPUs all different domains (request 4)",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", NVLinkDomain: 0},
				{Name: "gpu-1", NVLinkDomain: 1},
				{Name: "gpu-2", NVLinkDomain: 2},
				{Name: "gpu-3", NVLinkDomain: 3},
			},
			gpusRequested: 4,
			expected:      false, // No single domain has 4 GPUs
		},
		{
			name: "Single GPU (no NVLink needed)",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", NVLinkDomain: 0},
			},
			gpusRequested: 1,
			expected:      false, // Single GPU doesn't need NVLink
		},
		{
			name: "2 GPUs with peer list",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", NVLinkPeers: []string{"gpu-1"}},
				{Name: "gpu-1", NVLinkPeers: []string{"gpu-0"}},
			},
			gpusRequested: 2,
			expected:      true, // Peer connectivity detected
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := plugin.checkNVLinkConnectivity(tt.gpuDevices, tt.gpusRequested)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// Test PCIe locality detection
func TestCheckPCIeLocality(t *testing.T) {
	plugin := &VRAMScheduler{}

	tests := []struct {
		name          string
		gpuDevices    []GPUDevice
		gpusRequested int
		expected      bool
	}{
		{
			name: "4 GPUs on same PCIe switch",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", PCIeSwitch: "switch-0"},
				{Name: "gpu-1", PCIeSwitch: "switch-0"},
				{Name: "gpu-2", PCIeSwitch: "switch-0"},
				{Name: "gpu-3", PCIeSwitch: "switch-0"},
			},
			gpusRequested: 4,
			expected:      true,
		},
		{
			name: "8 GPUs - 4 on switch-0, 4 on switch-1 (request 4)",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", PCIeSwitch: "switch-0"},
				{Name: "gpu-1", PCIeSwitch: "switch-0"},
				{Name: "gpu-2", PCIeSwitch: "switch-0"},
				{Name: "gpu-3", PCIeSwitch: "switch-0"},
				{Name: "gpu-4", PCIeSwitch: "switch-1"},
				{Name: "gpu-5", PCIeSwitch: "switch-1"},
				{Name: "gpu-6", PCIeSwitch: "switch-1"},
				{Name: "gpu-7", PCIeSwitch: "switch-1"},
			},
			gpusRequested: 4,
			expected:      true, // Either switch satisfies
		},
		{
			name: "4 GPUs on different switches",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", PCIeSwitch: "switch-0"},
				{Name: "gpu-1", PCIeSwitch: "switch-1"},
				{Name: "gpu-2", PCIeSwitch: "switch-2"},
				{Name: "gpu-3", PCIeSwitch: "switch-3"},
			},
			gpusRequested: 4,
			expected:      false, // No single switch has 4 GPUs
		},
		{
			name: "GPUs without PCIe switch info",
			gpuDevices: []GPUDevice{
				{Name: "gpu-0", PCIeSwitch: ""},
				{Name: "gpu-1", PCIeSwitch: ""},
			},
			gpusRequested: 2,
			expected:      false, // No PCIe switch information
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := plugin.checkPCIeLocality(tt.gpuDevices, tt.gpusRequested)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}
