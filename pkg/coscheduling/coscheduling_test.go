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
	"testing"
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

// TODO: Add integration tests with proper framework.Handle mocks
// Testing with the full scheduler framework requires complex mocking
// Consider using testify/mock or implementing integration tests
