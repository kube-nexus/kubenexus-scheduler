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

package coscheduling

import (
	"context"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/utils"
	testutil "sigs.k8s.io/scheduler-plugins/test/util"
)

// TestName verifies the plugin name
func TestName(t *testing.T) {
	cs := &Coscheduling{}
	if cs.Name() != Name {
		t.Errorf("Expected plugin name %s, got %s", Name, cs.Name())
	}
}

// TestConstants verifies plugin constants
func TestConstants(t *testing.T) {
	if Name != "Coscheduling" {
		t.Errorf("Expected Name constant to be 'Coscheduling', got %s", Name)
	}

	if PodGroupName != "pod-group.scheduling.sigs.k8s.io/name" {
		t.Errorf("Unexpected PodGroupName constant: %s", PodGroupName)
	}

	if PodGroupMinAvailable != "pod-group.scheduling.sigs.k8s.io/min-available" {
		t.Errorf("Unexpected PodGroupMinAvailable constant: %s", PodGroupMinAvailable)
	}
}

// TestPermitWithFramework tests Permit() with proper framework mocking
func TestPermitWithFramework(t *testing.T) {
	tests := []struct {
		name           string
		pod            *v1.Pod
		existingPods   []*v1.Pod
		expectedStatus fwk.Code
		expectedWait   bool
		description    string
	}{
		{
			name: "Pod without gang annotation - immediate allow",
			pod: testutil.MakePod("standalone-pod", "default", "",
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
				nil, nil),
			existingPods:   nil,
			expectedStatus: fwk.Success,
			expectedWait:   false,
			description:    "Non-gang pods should be allowed immediately",
		},
		{
			name: "First pod in gang - should wait",
			pod: testutil.MakePod("gang-pod-1", "default", "",
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
				map[string]string{
					PodGroupName:         "training-job",
					PodGroupMinAvailable: "3",
				},
				nil),
			existingPods:   nil,
			expectedStatus: fwk.Wait,
			expectedWait:   true,
			description:    "First pod in gang should wait for others",
		},
		{
			name: "Partial gang - should wait",
			pod: testutil.MakePod("gang-pod-2", "default", "",
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
				map[string]string{
					PodGroupName:         "training-job",
					PodGroupMinAvailable: "3",
				},
				nil),
			existingPods: []*v1.Pod{
				testutil.MakePod("gang-pod-1", "default", "",
					v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
					map[string]string{
						PodGroupName:         "training-job",
						PodGroupMinAvailable: "3",
					},
					nil),
			},
			expectedStatus: fwk.Wait,
			expectedWait:   true,
			description:    "Should wait until minAvailable is met",
		},
		{
			name: "Complete gang - should allow all",
			pod: testutil.MakePod("gang-pod-3", "default", "",
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
				map[string]string{
					PodGroupName:         "training-job",
					PodGroupMinAvailable: "3",
				},
				nil),
			existingPods: []*v1.Pod{
				testutil.MakePod("gang-pod-1", "default", "test-node-1",
					v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
					map[string]string{
						PodGroupName:         "training-job",
						PodGroupMinAvailable: "3",
					},
					nil),
				testutil.MakePod("gang-pod-2", "default", "test-node-2",
					v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
					map[string]string{
						PodGroupName:         "training-job",
						PodGroupMinAvailable: "3",
					},
					nil),
			},
			expectedStatus: fwk.Success,
			expectedWait:   false,
			description:    "All pods should be allowed when minAvailable is met",
		},
		{
			name: "Invalid minAvailable annotation",
			pod: testutil.MakePod("invalid-gang-pod", "default", "",
				v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
				map[string]string{
					PodGroupName:         "bad-gang",
					PodGroupMinAvailable: "invalid",
				},
				nil),
			existingPods:   nil,
			expectedStatus: fwk.Success,
			expectedWait:   false,
			description:    "Invalid minAvailable should allow pod through",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create plugin with fake pod lister containing existing pods
			plugin := &Coscheduling{
				podLister:       testutil.NewFakePodLister(tt.existingPods),
				podGroupManager: utils.NewPodGroupManager(testutil.NewFakePodLister(tt.existingPods)),
				frameworkHandle: nil, // No framework handle needed for basic Permit tests
			}

			// For tests that need IterateOverWaitingPods, we need a handle
			// but our fake one doesn't support it, so those will use recovery

			state := framework.NewCycleState()

			// Call Permit
			status, timeout := plugin.Permit(context.Background(), state, tt.pod, "test-node")

			if status.Code() != tt.expectedStatus {
				t.Errorf("%s: Expected status %v, got %v", tt.description, tt.expectedStatus, status.Code())
			}

			if tt.expectedWait && timeout == 0 {
				t.Errorf("%s: Expected non-zero timeout for Wait status", tt.description)
			}
			if !tt.expectedWait && timeout != 0 {
				t.Errorf("%s: Expected zero timeout for non-Wait status", tt.description)
			}
		})
	}
}

// TestPreFilterWithFramework tests PreFilter() with framework mocking
func TestPreFilterWithFramework(t *testing.T) {
	gangPods := []*v1.Pod{
		testutil.MakePod("gang-pod-1", "default", "",
			v1.ResourceList{v1.ResourceCPU: resource.MustParse("2")},
			map[string]string{
				PodGroupName:         "training-job",
				PodGroupMinAvailable: "2",
			},
			nil),
		testutil.MakePod("gang-pod-2", "default", "",
			v1.ResourceList{v1.ResourceCPU: resource.MustParse("2")},
			map[string]string{
				PodGroupName:         "training-job",
				PodGroupMinAvailable: "2",
			},
			nil),
	}

	// Create plugin with fake pod lister
	plugin := &Coscheduling{
		podLister:       testutil.NewFakePodLister(gangPods),
		podGroupManager: utils.NewPodGroupManager(testutil.NewFakePodLister(gangPods)),
		frameworkHandle: nil,
	}

	state := framework.NewCycleState()

	// Test PreFilter on gang pod
	result, status := plugin.PreFilter(context.Background(), state, gangPods[0], nil)
	if !status.IsSuccess() {
		t.Errorf("PreFilter failed: %v", status.AsError())
	}
	if result == nil {
		t.Error("PreFilter should return non-nil PreFilterResult for gang pods")
	}

	// Test PreFilter on non-gang pod
	nonGangPod := testutil.MakePod("standalone", "default", "",
		v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
		nil, nil)
	_, status = plugin.PreFilter(context.Background(), state, nonGangPod, nil)
	if !status.IsSuccess() {
		t.Errorf("PreFilter should succeed for non-gang pods: %v", status.AsError())
	}
}

// TestTimeoutHandling tests gang scheduling timeout behavior
func TestTimeoutHandling(t *testing.T) {
	pod := testutil.MakePod("gang-pod-timeout", "default", "",
		v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
		map[string]string{
			PodGroupName:         "timeout-job",
			PodGroupMinAvailable: "10",
		}, nil) // Require 10 pods, but only 1 will come

	// Create plugin with empty pod lister
	plugin := &Coscheduling{
		podLister:       testutil.NewFakePodLister(nil),
		podGroupManager: utils.NewPodGroupManager(testutil.NewFakePodLister(nil)),
		frameworkHandle: nil,
	}

	state := framework.NewCycleState()

	// Call Permit - should wait
	status, timeout := plugin.Permit(context.Background(), state, pod, "test-node")

	if status.Code() != fwk.Wait {
		t.Errorf("Expected Wait status, got %v", status.Code())
	}

	// Verify timeout is set (default 10s)
	if timeout <= 0 || timeout > 60*time.Second {
		t.Errorf("Expected reasonable timeout (0 < t <= 60s), got %v", timeout)
	}

	t.Logf("Gang scheduling timeout set to: %v", timeout)
}

// TestMultipleGangGroups tests handling multiple independent gang groups
func TestMultipleGangGroups(t *testing.T) {
	pods := []*v1.Pod{
		// Gang group 1
		testutil.MakePod("gang1-pod1", "default", "",
			v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
			map[string]string{
				PodGroupName:         "job-1",
				PodGroupMinAvailable: "2",
			}, nil),
		testutil.MakePod("gang1-pod2", "default", "",
			v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
			map[string]string{
				PodGroupName:         "job-1",
				PodGroupMinAvailable: "2",
			}, nil),
		// Gang group 2
		testutil.MakePod("gang2-pod1", "default", "",
			v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
			map[string]string{
				PodGroupName:         "job-2",
				PodGroupMinAvailable: "2",
			}, nil),
	}

	// Create plugin with fake pod lister
	plugin := &Coscheduling{
		podLister:       testutil.NewFakePodLister(pods),
		podGroupManager: utils.NewPodGroupManager(testutil.NewFakePodLister(pods)),
		frameworkHandle: nil,
	}

	// Gang 1 should succeed (2/2 pods ready)
	state1 := framework.NewCycleState()
	status1, _ := plugin.Permit(context.Background(), state1, pods[0], "test-node")
	if status1.Code() != fwk.Success {
		t.Errorf("Gang group 1 should succeed, got status: %v", status1.Code())
	}

	// Gang 2 should wait (1/2 pods ready)
	state2 := framework.NewCycleState()
	status2, timeout := plugin.Permit(context.Background(), state2, pods[2], "test-node")
	if status2.Code() != fwk.Wait {
		t.Errorf("Gang group 2 should wait, got status: %v", status2.Code())
	}
	if timeout == 0 {
		t.Error("Gang group 2 should have non-zero timeout")
	}

	t.Logf("Gang group 1: Success, Gang group 2: Waiting (%v)", timeout)
}
