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

package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestResourceReservationDeepCopy verifies deepcopy works correctly
func TestResourceReservationDeepCopy(t *testing.T) {
	original := &ResourceReservation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-reservation",
			Namespace: "default",
		},
		Spec: ResourceReservationSpec{
			Reservations: map[string]Reservation{
				"driver": {
					Node:   "node-1",
					CPU:    resource.MustParse("2"),
					Memory: resource.MustParse("4Gi"),
				},
			},
		},
		Status: ResourceReservationStatus{
			Pods: map[string]string{
				"driver": "running",
			},
		},
	}

	// Test DeepCopy
	copied := original.DeepCopy()
	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied.ObjectMeta.Name != original.ObjectMeta.Name {
		t.Errorf("Name mismatch: got %s, want %s", copied.ObjectMeta.Name, original.ObjectMeta.Name)
	}

	// Modify original, ensure copy is unchanged
	reservation := original.Spec.Reservations["driver"]
	reservation.Node = "node-2"
	original.Spec.Reservations["driver"] = reservation

	if copied.Spec.Reservations["driver"].Node == "node-2" {
		t.Error("DeepCopy is not deep - modification affected copy")
	}
}

// TestConstants verifies label constants
func TestConstants(t *testing.T) {
	if AppIDLabel != "scheduling.kubenexus.io/app-id" {
		t.Errorf("Unexpected AppIDLabel: %s", AppIDLabel)
	}

	if PodGroupLabel != "pod-group.scheduling.sigs.k8s.io/name" {
		t.Errorf("Unexpected PodGroupLabel: %s", PodGroupLabel)
	}
}
