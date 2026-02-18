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
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/scheduler-plugins/pkg/utils"
)

// Type represents the type of workload
type Type int

const (
	// TypeService represents normal service workloads (APIs, webapps, databases)
	TypeService Type = iota
	// TypeBatch represents batch workloads (Spark, ML training, data processing)
	TypeBatch
)

// String returns the string representation of the workload type
func (t Type) String() string {
	switch t {
	case TypeService:
		return "service"
	case TypeBatch:
		return "batch"
	default:
		return "unknown"
	}
}

// ClassifyPod determines the workload type of a pod
func ClassifyPod(pod *v1.Pod) Type {
	if pod == nil {
		return TypeService
	}

	// Check for gang scheduling annotations (definitive indicator of batch workload)
	name, minAvailable, err := utils.GetPodGroupLabels(pod)
	if err == nil && name != "" && minAvailable > 0 {
		// Has valid gang scheduling annotations
		return TypeBatch
	}

	// Check for explicit workload type label
	if workloadType, exists := pod.Labels["workload.kubenexus.io/type"]; exists {
		if workloadType == "batch" {
			return TypeBatch
		}
		return TypeService
	}

	// Heuristics: Check common batch workload labels
	batchIndicators := []string{
		"spark-role",                   // Apache Spark
		"spark-app-id",                 // Apache Spark
		"tf-replica-type",              // TensorFlow
		"pytorch-replica-type",         // PyTorch
		"mpi-job-role",                 // MPI jobs
		"ray.io/node-type",             // Ray
		"kubeflow.org/component",       // Kubeflow
		"batch.kubernetes.io/job-name", // Kubernetes batch jobs
	}

	for _, indicator := range batchIndicators {
		if _, exists := pod.Labels[indicator]; exists {
			return TypeBatch
		}
	}

	// Check if pod belongs to a Job or CronJob (batch indicators)
	for _, ownerRef := range pod.OwnerReferences {
		if ownerRef.Kind == "Job" || ownerRef.Kind == "CronJob" {
			return TypeBatch
		}
	}

	// Default to service workload (safer default - fast scheduling)
	return TypeService
}

// IsBatch returns true if the pod is a batch workload
func IsBatch(pod *v1.Pod) bool {
	return ClassifyPod(pod) == TypeBatch
}

// IsService returns true if the pod is a service workload
func IsService(pod *v1.Pod) bool {
	return ClassifyPod(pod) == TypeService
}
