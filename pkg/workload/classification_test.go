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

// TestClassifyPod_EdgeCases tests edge cases in classification
func TestClassifyPod_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		pod      *v1.Pod
		wantType Type
	}{
		{
			name: "pod with multiple indicators (gang + spark) - gang wins",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"pod-group.scheduling.sigs.k8s.io/name": "spark-gang",
						"spark-role":                            "driver",
					},
				},
			},
			wantType: TypeBatch,
		},
		{
			name: "pod with empty labels map",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
				},
			},
			wantType: TypeService,
		},
		{
			name: "pod with no metadata",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{Name: "test"},
					},
				},
			},
			wantType: TypeService,
		},
		{
			name: "DaemonSet pod",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "DaemonSet",
							Name:       "monitoring-agent",
						},
					},
				},
			},
			wantType: TypeService,
		},
		{
			name: "StatefulSet pod",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "StatefulSet",
							Name:       "database",
						},
					},
				},
			},
			wantType: TypeService,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyPod(tt.pod)
			if got != tt.wantType {
				t.Errorf("ClassifyPod() = %v, want %v", got, tt.wantType)
			}
		})
	}
}

// TestClassifyPod_AllFrameworks tests all supported ML frameworks
func TestClassifyPod_AllFrameworks(t *testing.T) {
	frameworks := []struct {
		name  string
		label string
		value string
	}{
		{"TensorFlow", "tf-replica-type", "worker"},
		{"PyTorch", "pytorch-replica-type", "worker"},
		{"Ray", "ray.io/node-type", "worker"},
		{"Spark", "spark-role", "executor"},
		{"MPI", "mpi-job-role", "worker"},
	}

	for _, fw := range frameworks {
		t.Run(fw.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						fw.label: fw.value,
					},
				},
			}

			if ClassifyPod(pod) != TypeBatch {
				t.Errorf("%s pod should be classified as batch", fw.name)
			}
		})
	}
}

// BenchmarkClassifyPod benchmarks pod classification performance
func BenchmarkClassifyPod(b *testing.B) {
	testCases := []struct {
		name string
		pod  *v1.Pod
	}{
		{
			name: "Service",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "nginx",
					},
				},
			},
		},
		{
			name: "Spark",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"spark-role": "driver",
					},
				},
			},
		},
		{
			name: "Gang",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"pod-group.scheduling.sigs.k8s.io/name": "gang-123",
					},
				},
			},
		},
		{
			name: "Job",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "batch/v1",
							Kind:       "Job",
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_ = ClassifyPod(tc.pod)
			}
		})
	}
}

// BenchmarkClassifyPod_Parallel benchmarks concurrent classification
func BenchmarkClassifyPod_Parallel(b *testing.B) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"spark-role": "executor",
			},
		},
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = ClassifyPod(pod)
		}
	})
}
