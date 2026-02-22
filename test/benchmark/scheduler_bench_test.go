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

// Package benchmark contains performance benchmarks for KubeNexus scheduler plugins.
//
// Run with: go test -bench=. -benchmem -benchtime=10s ./test/benchmark
package benchmark

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/scheduler-plugins/pkg/workload"
)

// BenchmarkWorkloadClassification benchmarks pod classification performance
func BenchmarkWorkloadClassification(b *testing.B) {
	pods := []struct {
		name string
		pod  *v1.Pod
	}{
		{
			name: "Spark",
			pod:  makeSparkPod("spark-driver-123"),
		},
		{
			name: "TensorFlow",
			pod:  makeTensorFlowPod("tf-worker-0"),
		},
		{
			name: "Service",
			pod:  makeServicePod("web-server-abc"),
		},
		{
			name: "BatchJob",
			pod:  makeBatchPod("job-pod-456"),
		},
	}

	for _, tc := range pods {
		b.Run(tc.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = workload.ClassifyPod(tc.pod)
			}
		})
	}
}

// BenchmarkWorkloadClassificationParallel tests classification under concurrent load
func BenchmarkWorkloadClassificationParallel(b *testing.B) {
	pod := makeSparkPod("spark-driver-bench")

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = workload.ClassifyPod(pod)
		}
	})
}

// BenchmarkGangSchedulingPermit benchmarks gang scheduling permit decision
func BenchmarkGangSchedulingPermit(b *testing.B) {
	b.Skip("Requires full scheduler framework setup - implement after integration tests")

	// This should benchmark:
	// - Small gangs (2-4 pods)
	// - Medium gangs (8-16 pods)
	// - Large gangs (32-64 pods)
	// - Very large gangs (128-256 pods)
}

// BenchmarkTopologyScoring benchmarks GPU topology scoring
func BenchmarkTopologyScoring(b *testing.B) {
	b.Skip("Requires node topology data - implement after NUMA plugin refactor")

	// This should benchmark:
	// - Single GPU pod scoring
	// - Multi-GPU pod scoring (2, 4, 8 GPUs)
	// - Complex topology (NVLink, PCIe switches)
}

// BenchmarkSchedulingLatency measures end-to-end scheduling latency
func BenchmarkSchedulingLatency(b *testing.B) {
	b.Skip("Requires full scheduler setup - implement in E2E benchmarks")

	// This should measure:
	// - Single pod scheduling latency
	// - Gang scheduling latency (all pods)
	// - Mixed workload scheduling
	// - Under load (100+ pods/sec)
}

// BenchmarkMemoryUsage benchmarks memory consumption
func BenchmarkMemoryUsage(b *testing.B) {
	// Test memory usage with increasing number of pods
	podCounts := []int{10, 100, 1000, 10000}

	for _, count := range podCounts {
		b.Run(fmt.Sprintf("Pods_%d", count), func(b *testing.B) {
			pods := make([]*v1.Pod, count)
			for i := 0; i < count; i++ {
				pods[i] = makeSparkPod(fmt.Sprintf("pod-%d", i))
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				for _, pod := range pods {
					_ = workload.ClassifyPod(pod)
				}
			}
		})
	}
}

// BenchmarkConcurrentGangs benchmarks multiple concurrent gang scheduling operations
func BenchmarkConcurrentGangs(b *testing.B) {
	b.Skip("Requires full scheduler framework - implement after integration tests")

	// This should benchmark:
	// - 10 concurrent small gangs (4 pods each)
	// - 5 concurrent medium gangs (16 pods each)
	// - 2 concurrent large gangs (64 pods each)
}

// Helper functions to create test pods

func makeSparkPod(name string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				"spark-role":         "driver",
				"spark-app-selector": "spark-pi",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "spark",
					Image: "spark:3.5.0",
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("4"),
							v1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				},
			},
		},
	}
}

func makeTensorFlowPod(name string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				"app":      "tensorflow",
				"job-role": "worker",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "tensorflow",
					Image: "tensorflow/tensorflow:latest-gpu",
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("8"),
							v1.ResourceMemory: resource.MustParse("32Gi"),
							"nvidia.com/gpu":  resource.MustParse("4"),
						},
					},
				},
			},
		},
	}
}

func makeServicePod(name string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				"app": "web-server",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "nginx",
					Image: "nginx:latest",
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("500m"),
							v1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			},
		},
	}
}

func makeBatchPod(name string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "batch/v1",
					Kind:       "Job",
					Name:       "batch-job-123",
				},
			},
		},
		Spec: v1.PodSpec{
			RestartPolicy: v1.RestartPolicyNever,
			Containers: []v1.Container{
				{
					Name:    "worker",
					Image:   "busybox:latest",
					Command: []string{"sh", "-c", "echo processing && sleep 60"},
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("2"),
							v1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
				},
			},
		},
	}
}

// BenchmarkComparisonWithDefaultScheduler compares KubeNexus with default scheduler
func BenchmarkComparisonWithDefaultScheduler(b *testing.B) {
	b.Skip("Requires side-by-side scheduler setup - implement in E2E benchmarks")

	// This should compare:
	// - Single pod latency: KubeNexus vs default
	// - Gang scheduling: KubeNexus vs default + coscheduling plugin
	// - Throughput: pods/second under load
	// - Memory usage: KubeNexus vs default
}
