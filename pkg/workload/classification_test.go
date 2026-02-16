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

package workload

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClassifyPod_GangScheduling(t *testing.T) {
	// Pod with gang scheduling annotations = batch
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"pod-group.scheduling.sigs.k8s.io/name":          "my-group",
				"pod-group.scheduling.sigs.k8s.io/min-available": "10",
			},
		},
	}

	if ClassifyPod(pod) != TypeBatch {
		t.Error("Pod with gang scheduling annotations should be classified as batch")
	}
}

func TestClassifyPod_ExplicitLabel(t *testing.T) {
	// Pod with explicit workload type label
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"workload.kubenexus.io/type": "batch",
			},
		},
	}

	if ClassifyPod(pod) != TypeBatch {
		t.Error("Pod with explicit batch label should be classified as batch")
	}
}

func TestClassifyPod_SparkJob(t *testing.T) {
	// Spark pod = batch
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "spark-driver",
			Namespace: "default",
			Labels: map[string]string{
				"spark-role":   "driver",
				"spark-app-id": "spark-12345",
			},
		},
	}

	if ClassifyPod(pod) != TypeBatch {
		t.Error("Spark pod should be classified as batch")
	}
}

func TestClassifyPod_TensorFlowJob(t *testing.T) {
	// TensorFlow pod = batch
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tf-worker",
			Namespace: "default",
			Labels: map[string]string{
				"tf-replica-type": "worker",
			},
		},
	}

	if ClassifyPod(pod) != TypeBatch {
		t.Error("TensorFlow pod should be classified as batch")
	}
}

func TestClassifyPod_KubernetesJob(t *testing.T) {
	// Pod owned by Job = batch
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "job-pod",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "Job",
					Name: "my-job",
				},
			},
		},
	}

	if ClassifyPod(pod) != TypeBatch {
		t.Error("Pod owned by Job should be classified as batch")
	}
}

func TestClassifyPod_ServiceDefault(t *testing.T) {
	// Pod with no batch indicators = service (default)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "webapp",
			Namespace: "default",
			Labels: map[string]string{
				"app": "nginx",
			},
		},
	}

	if ClassifyPod(pod) != TypeService {
		t.Error("Pod with no batch indicators should be classified as service")
	}
}

func TestClassifyPod_Nil(t *testing.T) {
	// Nil pod = service (safe default)
	if ClassifyPod(nil) != TypeService {
		t.Error("Nil pod should be classified as service")
	}
}

func TestIsBatch(t *testing.T) {
	batchPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"workload.kubenexus.io/type": "batch",
			},
		},
	}

	if !IsBatch(batchPod) {
		t.Error("IsBatch should return true for batch pod")
	}
}

func TestIsService(t *testing.T) {
	servicePod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": "nginx",
			},
		},
	}

	if !IsService(servicePod) {
		t.Error("IsService should return true for service pod")
	}
}
