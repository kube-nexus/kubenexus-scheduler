/*
Copyright 2024 KubeNexus Authors.

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

package utils

import (
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// GetPodGroupLabels extracts pod group information from pod labels
func GetPodGroupLabels(pod *v1.Pod) (name string, minAvailable int, err error) {
	const (
		PodGroupNameLabel         = "pod-group.scheduling.kubenexus.io/name"
		PodGroupMinAvailableLabel = "pod-group.scheduling.kubenexus.io/min-available"
		// Backward compatibility with old labels
		OldPodGroupNameLabel         = "pod-group.scheduling.sigs.k8s.io/name"
		OldPodGroupMinAvailableLabel = "pod-group.scheduling.sigs.k8s.io/min-available"
	)

	// Try new labels first, fall back to old labels for compatibility
	name, exists := pod.Labels[PodGroupNameLabel]
	if !exists || name == "" {
		name, exists = pod.Labels[OldPodGroupNameLabel]
		if !exists || name == "" {
			return "", 0, nil
		}
	}

	minAvailableStr, exists := pod.Labels[PodGroupMinAvailableLabel]
	if !exists || minAvailableStr == "" {
		minAvailableStr, exists = pod.Labels[OldPodGroupMinAvailableLabel]
		if !exists || minAvailableStr == "" {
			return "", 0, nil
		}
	}

	minAvailable, err = strconv.Atoi(minAvailableStr)
	if err != nil {
		return "", 0, fmt.Errorf("failed to parse min-available value %q: %w", minAvailableStr, err)
	}

	if minAvailable < 1 {
		return "", 0, fmt.Errorf("min-available must be at least 1, got %d", minAvailable)
	}

	return name, minAvailable, nil
}

// GetPodGroupKey returns a unique key for a pod group
func GetPodGroupKey(namespace, podGroupName string) string {
	return fmt.Sprintf("%s/%s", namespace, podGroupName)
}

// GetPodResourceRequests returns the total resource requests for a pod
func GetPodResourceRequests(pod *v1.Pod) (cpu, memory resource.Quantity) {
	for _, container := range pod.Spec.Containers {
		if req, ok := container.Resources.Requests[v1.ResourceCPU]; ok {
			cpu.Add(req)
		}
		if req, ok := container.Resources.Requests[v1.ResourceMemory]; ok {
			memory.Add(req)
		}
	}
	return cpu, memory
}

// IsPodInPodGroup checks if a pod belongs to a pod group
func IsPodInPodGroup(pod *v1.Pod) bool {
	name, minAvailable, err := GetPodGroupLabels(pod)
	return err == nil && name != "" && minAvailable > 1
}

// GetSparkApplicationID extracts Spark application ID from pod labels
func GetSparkApplicationID(pod *v1.Pod) string {
	if appID, exists := pod.Labels["spark-app-id"]; exists {
		return appID
	}
	return ""
}

// GetSparkRole extracts Spark role (driver/executor) from pod labels
func GetSparkRole(pod *v1.Pod) string {
	if role, exists := pod.Labels["spark-role"]; exists {
		return role
	}
	return ""
}

// IsSparkDriver checks if the pod is a Spark driver
func IsSparkDriver(pod *v1.Pod) bool {
	return GetSparkRole(pod) == "driver"
}

// IsSparkExecutor checks if the pod is a Spark executor
func IsSparkExecutor(pod *v1.Pod) bool {
	return GetSparkRole(pod) == "executor"
}
