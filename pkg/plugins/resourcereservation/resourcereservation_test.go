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

package resourcereservation

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kube-nexus/kubenexus-scheduler/pkg/apis/scheduling/v1alpha1"
)

func TestPluginName(t *testing.T) {
	plugin := &ResourceReservation{}
	if plugin.Name() != Name {
		t.Errorf("Expected plugin name %s, got %s", Name, plugin.Name())
	}
}

func TestConstants(t *testing.T) {
	if Name != "ResourceReservation" {
		t.Errorf("Expected Name to be 'ResourceReservation', got %s", Name)
	}
}

func TestNewResourceReservation(t *testing.T) {
	tests := []struct {
		name       string
		pod        *v1.Pod
		driverNode string
		wantCPU    string
		wantMemory string
	}{
		{
			name:       "pod with no resource requests",
			driverNode: "node-1",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "container",
							Image: "test:latest",
						},
					},
				},
			},
			wantCPU:    "1",
			wantMemory: "750M",
		},
		{
			name:       "pod with custom resource requests",
			driverNode: "node-2",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "spark-driver",
					Namespace: "default",
					Labels: map[string]string{
						v1alpha1.AppIDLabel: "spark-job-123",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "spark",
							Image: "spark:latest",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("4"),
									v1.ResourceMemory: resource.MustParse("8Gi"),
								},
							},
						},
					},
				},
			},
			wantCPU:    "4",
			wantMemory: "8Gi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reservation := newResourceReservation(tt.driverNode, tt.pod)

			if reservation == nil {
				t.Fatal("newResourceReservation returned nil")
			}

			if reservation.Namespace != tt.pod.Namespace {
				t.Errorf("Expected namespace %s, got %s", tt.pod.Namespace, reservation.Namespace)
			}

			// Check if reservation has the pod
			if len(reservation.Spec.Reservations) == 0 {
				t.Error("Expected at least one reservation")
			}

			res := reservation.Spec.Reservations[tt.pod.Name]
			if res.Node != tt.driverNode {
				t.Errorf("Expected node %s, got %s", tt.driverNode, res.Node)
			}

			if res.CPU.String() != tt.wantCPU {
				t.Errorf("Expected CPU %s, got %s", tt.wantCPU, res.CPU.String())
			}

			if res.Memory.String() != tt.wantMemory {
				t.Errorf("Expected Memory %s, got %s", tt.wantMemory, res.Memory.String())
			}
		})
	}
}

func TestNewResourceReservation_WithAppIDLabel(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "spark-driver",
			Namespace: "default",
			Labels: map[string]string{
				v1alpha1.AppIDLabel: "my-spark-app",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{Name: "spark"}},
		},
	}

	reservation := newResourceReservation("node-1", pod)

	// Name should be app-id from label
	if reservation.Name != "my-spark-app" {
		t.Errorf("Expected reservation name 'my-spark-app', got %s", reservation.Name)
	}

	// Label should be set
	if reservation.Labels[v1alpha1.AppIDLabel] != "my-spark-app" {
		t.Errorf("Expected app-id label 'my-spark-app', got %s", reservation.Labels[v1alpha1.AppIDLabel])
	}
}

func TestNewResourceReservation_OwnerReference(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{Name: "test"}},
		},
	}

	reservation := newResourceReservation("node-1", pod)

	// Should have owner reference to the pod
	if len(reservation.OwnerReferences) == 0 {
		t.Fatal("Expected owner reference to be set")
	}

	ownerRef := reservation.OwnerReferences[0]
	if ownerRef.Kind != "Pod" {
		t.Errorf("Expected owner kind 'Pod', got %s", ownerRef.Kind)
	}
}

func TestReserve_NilPod(t *testing.T) {
	rr := &ResourceReservation{}
	status := rr.Reserve(context.TODO(), nil, nil, "node-1")

	if status.IsSuccess() {
		t.Error("Reserve should fail with nil pod")
	}

	if status.Message() != "pod cannot be nil" {
		t.Errorf("Expected error message 'pod cannot be nil', got %s", status.Message())
	}
}

func TestUnreserve_NilPod(t *testing.T) {
	rr := &ResourceReservation{}
	// Should not panic with nil pod
	rr.Unreserve(context.TODO(), nil, nil, "node-1")
	// If we get here without panic, test passes
}
