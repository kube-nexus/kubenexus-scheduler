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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ResourceReservation represents a resource reservation for a pod group
type ResourceReservation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourceReservationSpec   `json:"spec"`
	Status ResourceReservationStatus `json:"status,omitempty"`
}

// ResourceReservationSpec defines the desired state of ResourceReservation
// +k8s:deepcopy-gen=true
type ResourceReservationSpec struct {
	// Reservations maps pod names to their resource reservations
	Reservations map[string]Reservation `json:"reservations"`
}

// Reservation represents resources reserved for a single pod
// +k8s:deepcopy-gen=true
type Reservation struct {
	// Node is the name of the node where resources are reserved
	Node string `json:"node"`

	// CPU is the amount of CPU reserved
	CPU resource.Quantity `json:"cpu"`

	// Memory is the amount of memory reserved
	Memory resource.Quantity `json:"memory"`
}

// ResourceReservationStatus defines the observed state of ResourceReservation
// +k8s:deepcopy-gen=true
type ResourceReservationStatus struct {
	// Pods maps pod names to their current state
	Pods map[string]string `json:"pods,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ResourceReservationList contains a list of ResourceReservation
type ResourceReservationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceReservation `json:"items"`
}

const (
	// AppIDLabel is the label key for application ID
	AppIDLabel = "scheduling.kubenexus.io/app-id"

	// PodGroupLabel is the label key for pod group name
	PodGroupLabel = "pod-group.scheduling.sigs.k8s.io/name"
)
