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
	"context"
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	// PodGroupNameLabel is the label key for pod group name (KubeNexus)
	PodGroupNameLabel = "pod-group.scheduling.kubenexus.io/name"
	// PodGroupMinAvailableLabel is the label key for min available pods (KubeNexus)
	PodGroupMinAvailableLabel = "pod-group.scheduling.kubenexus.io/min-available"
	
	// Legacy labels for backward compatibility
	LegacyPodGroupNameLabel         = "pod-group.scheduling.sigs.k8s.io/name"
	LegacyPodGroupMinAvailableLabel = "pod-group.scheduling.sigs.k8s.io/min-available"
	
	// Native K8s 1.35+ Workload labels
	NativeWorkloadNameLabel = "scheduling.k8s.io/workload-name"
	NativePodGroupLabel     = "scheduling.k8s.io/pod-group"
)

// GetPodGroupLabels extracts pod group information from pod labels
// Supports label-based pod groups for backward compatibility
func GetPodGroupLabels(pod *v1.Pod) (name string, minAvailable int, err error) {
	if pod == nil {
		return "", 0, fmt.Errorf("pod is nil")
	}

	// Try new labels first, fall back to old labels for compatibility
	name, exists := pod.Labels[PodGroupNameLabel]
	if !exists || name == "" {
		name, exists = pod.Labels[LegacyPodGroupNameLabel]
		if !exists || name == "" {
			return "", 0, nil
		}
	}

	minAvailableStr, exists := pod.Labels[PodGroupMinAvailableLabel]
	if !exists || minAvailableStr == "" {
		minAvailableStr, exists = pod.Labels[LegacyPodGroupMinAvailableLabel]
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

// PodGroupInfo contains information about a pod group from either labels or native K8s Workload CRD
type PodGroupInfo struct {
	Name         string
	MinMember    int
	FromWorkloadCRD bool // true if from native K8s 1.35+ Workload CRD
	TimeoutSeconds int32
}

// GetPodGroupInfo extracts pod group information from either:
// 1. Labels (backward compatibility and simple approach)
// 2. Native K8s 1.35+ Workload CRD (scheduling.k8s.io/v1alpha1)
// Returns nil if the pod doesn't belong to any pod group
func GetPodGroupInfo(ctx context.Context, pod *v1.Pod, dynamicClient dynamic.Interface) (*PodGroupInfo, error) {
	if pod == nil {
		return nil, fmt.Errorf("pod is nil")
	}

	// First, try to get from labels (for backward compatibility and label-based approach)
	name, minAvailable, err := GetPodGroupLabels(pod)
	if err == nil && name != "" && minAvailable > 0 {
		return &PodGroupInfo{
			Name:            name,
			MinMember:       minAvailable,
			FromWorkloadCRD: false,
			TimeoutSeconds:  60, // default timeout
		}, nil
	}

	// If not found in labels and we have a dynamic client, try native K8s 1.35+ Workload CRD
	if dynamicClient != nil {
		workloadInfo, err := GetWorkloadPodGroupInfo(ctx, pod, dynamicClient)
		if err == nil && workloadInfo != nil {
			return workloadInfo, nil
		}
	}

	return nil, nil
}

// GetWorkloadPodGroupInfo fetches gang scheduling info from native K8s 1.35+ Workload CRD
// apiVersion: scheduling.k8s.io/v1alpha1
// kind: Workload
func GetWorkloadPodGroupInfo(ctx context.Context, pod *v1.Pod, dynamicClient dynamic.Interface) (*PodGroupInfo, error) {
	// Check if pod has the native workload labels
	workloadName := pod.Labels[NativeWorkloadNameLabel]
	podGroupName := pod.Labels[NativePodGroupLabel]
	
	if workloadName == "" || podGroupName == "" {
		return nil, nil // Pod doesn't belong to a native Workload
	}

	// Define GVR for native K8s Workload CRD (1.35+)
	gvr := schema.GroupVersionResource{
		Group:    "scheduling.k8s.io",
		Version:  "v1alpha1",
		Resource: "workloads",
	}

	// Try to get the Workload CRD
	unstructuredWorkload, err := dynamicClient.Resource(gvr).Namespace(pod.Namespace).Get(ctx, workloadName, metav1.GetOptions{})
	if err != nil {
		return nil, nil // Workload not found, not an error (might not be available in older K8s versions)
	}

	// Extract spec.podGroups
	spec, found, err := getNestedMap(unstructuredWorkload.Object, "spec")
	if !found || err != nil {
		return nil, fmt.Errorf("invalid Workload spec")
	}

	podGroups, found, err := getNestedSlice(spec, "podGroups")
	if !found || err != nil {
		return nil, fmt.Errorf("podGroups not found in Workload spec")
	}

	// Find the pod group that matches our pod
	for _, pg := range podGroups {
		pgMap, ok := pg.(map[string]interface{})
		if !ok {
			continue
		}

		name, _, _ := getNestedString(pgMap, "name")
		if name != podGroupName {
			continue
		}

		// Found the matching pod group, extract gang policy
		policy, found, _ := getNestedMap(pgMap, "policy")
		if !found {
			continue
		}

		gang, found, _ := getNestedMap(policy, "gang")
		if !found {
			continue
		}

		minCount, found, _ := getNestedInt64(gang, "minCount")
		if !found {
			continue
		}

		return &PodGroupInfo{
			Name:            podGroupName,
			MinMember:       int(minCount),
			FromWorkloadCRD: true,
			TimeoutSeconds:  60, // default, could extract from Workload if defined
		}, nil
	}

	return nil, nil
}

// Helper functions for extracting nested fields from unstructured objects
func getNestedMap(obj map[string]interface{}, key string) (map[string]interface{}, bool, error) {
	val, found := obj[key]
	if !found {
		return nil, false, nil
	}
	m, ok := val.(map[string]interface{})
	if !ok {
		return nil, false, fmt.Errorf("value is not a map")
	}
	return m, true, nil
}

func getNestedSlice(obj map[string]interface{}, key string) ([]interface{}, bool, error) {
	val, found := obj[key]
	if !found {
		return nil, false, nil
	}
	s, ok := val.([]interface{})
	if !ok {
		return nil, false, fmt.Errorf("value is not a slice")
	}
	return s, true, nil
}

func getNestedString(obj map[string]interface{}, key string) (string, bool, error) {
	val, found := obj[key]
	if !found {
		return "", false, nil
	}
	s, ok := val.(string)
	if !ok {
		return "", false, fmt.Errorf("value is not a string")
	}
	return s, true, nil
}

func getNestedInt64(obj map[string]interface{}, key string) (int64, bool, error) {
	val, found := obj[key]
	if !found {
		return 0, false, nil
	}
	
	switch v := val.(type) {
	case int64:
		return v, true, nil
	case int32:
		return int64(v), true, nil
	case int:
		return int64(v), true, nil
	case float64:
		return int64(v), true, nil
	default:
		return 0, false, fmt.Errorf("value is not numeric")
	}
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
