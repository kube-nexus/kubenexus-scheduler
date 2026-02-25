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

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	// TenantTierLabel is the namespace label for tenant tier
	TenantTierLabel = "tenant.kubenexus.io/tier"

	// GPUClassLabel is the node label for GPU class (used in nodeSelector)
	GPUClassLabel = "gpu.nvidia.com/class"

	// Tenant tiers
	TierGold   = "gold"
	TierSilver = "silver"
	TierBronze = "bronze"

	// GPU classes (hardware tiers)
	GPUClassH100 = "h100"
	GPUClassA100 = "a100"
	GPUClassL4   = "l4"
)

// PodMutator handles pod mutation for deterministic autoscaling
type PodMutator struct {
	clientset kubernetes.Interface
}

// NewPodMutator creates a new PodMutator
func NewPodMutator(clientset kubernetes.Interface) *PodMutator {
	return &PodMutator{
		clientset: clientset,
	}
}

// Handle processes admission requests
func (pm *PodMutator) Handle(w http.ResponseWriter, r *http.Request) {
	klog.V(4).InfoS("Received admission request", "method", r.Method, "url", r.URL.Path)

	// Parse admission review request
	var admissionReview admissionv1.AdmissionReview
	if err := json.NewDecoder(r.Body).Decode(&admissionReview); err != nil {
		klog.ErrorS(err, "Failed to decode admission review request")
		http.Error(w, fmt.Sprintf("could not decode body: %v", err), http.StatusBadRequest)
		return
	}

	// Process the request
	admissionResponse := pm.mutate(admissionReview.Request)

	// Construct response
	responseAdmissionReview := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: admissionResponse,
	}

	// Set response UID to match request
	if admissionReview.Request != nil {
		responseAdmissionReview.Response.UID = admissionReview.Request.UID
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(responseAdmissionReview); err != nil {
		klog.ErrorS(err, "Failed to encode admission review response")
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
	}
}

// mutate processes the admission request and returns admission response
func (pm *PodMutator) mutate(req *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse {
	if req == nil {
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Message: "admission request is nil",
			},
		}
	}

	// Only mutate Pod objects
	if req.Kind.Kind != "Pod" {
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}

	// Parse pod
	var pod v1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		klog.ErrorS(err, "Failed to unmarshal pod", "namespace", req.Namespace, "name", req.Name)
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Message: fmt.Sprintf("could not unmarshal pod: %v", err),
			},
		}
	}

	klog.V(4).InfoS("Processing pod mutation",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"generateName", pod.GenerateName)

	// Check if pod requests GPUs
	if !requestsGPUs(&pod) {
		klog.V(5).InfoS("Pod does not request GPUs, skipping mutation",
			"pod", pod.Name,
			"namespace", pod.Namespace)
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}

	// Check if nodeSelector already has GPU class (user override)
	if hasGPUClassNodeSelector(&pod) {
		klog.V(4).InfoS("Pod already has gpu.nvidia.com/class nodeSelector, skipping mutation",
			"pod", pod.Name,
			"namespace", pod.Namespace,
			"gpuClass", pod.Spec.NodeSelector[GPUClassLabel])
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}

	// Get namespace tier
	tier, err := pm.getNamespaceTier(req.Namespace)
	if err != nil {
		klog.V(4).InfoS("Failed to get namespace tier, allowing without mutation",
			"namespace", req.Namespace,
			"error", err)
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}

	// Determine target GPU class based on tier
	gpuClass := getTierGPUClass(tier)
	if gpuClass == "" {
		klog.V(4).InfoS("Unknown tier, allowing without mutation",
			"namespace", req.Namespace,
			"tier", tier)
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}

	// Create JSON patch to inject nodeSelector
	patches := pm.createNodeSelectorPatches(&pod, gpuClass)
	if len(patches) == 0 {
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}

	// Marshal patches
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		klog.ErrorS(err, "Failed to marshal patches")
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Message: fmt.Sprintf("could not marshal patches: %v", err),
			},
		}
	}

	klog.InfoS("Injecting GPU class nodeSelector for deterministic autoscaling",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"tenantTier", tier,
		"gpuClass", gpuClass)

	patchType := admissionv1.PatchTypeJSONPatch
	return &admissionv1.AdmissionResponse{
		Allowed:   true,
		Patch:     patchBytes,
		PatchType: &patchType,
	}
}

// getNamespaceTier retrieves the tenant tier from namespace labels
func (pm *PodMutator) getNamespaceTier(namespace string) (string, error) {
	ns, err := pm.clientset.CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get namespace: %w", err)
	}

	tier, ok := ns.Labels[TenantTierLabel]
	if !ok || tier == "" {
		return "", fmt.Errorf("namespace %s does not have %s label", namespace, TenantTierLabel)
	}

	return strings.ToLower(tier), nil
}

// getTierGPUClass maps tenant tier to GPU class for deterministic autoscaling
func getTierGPUClass(tier string) string {
	switch strings.ToLower(tier) {
	case TierGold:
		return GPUClassH100 // Gold → H100 (highest performance)
	case TierSilver:
		return GPUClassA100 // Silver → A100 (balanced)
	case TierBronze:
		return GPUClassL4 // Bronze → L4 (cost-effective)
	default:
		return ""
	}
}

// requestsGPUs checks if pod requests GPU resources
func requestsGPUs(pod *v1.Pod) bool {
	gpuResourceNames := []string{
		"nvidia.com/gpu",
		"amd.com/gpu",
		"intel.com/gpu",
	}

	for _, container := range pod.Spec.Containers {
		for _, gpuResource := range gpuResourceNames {
			if qty, ok := container.Resources.Requests[v1.ResourceName(gpuResource)]; ok && !qty.IsZero() {
				return true
			}
			if qty, ok := container.Resources.Limits[v1.ResourceName(gpuResource)]; ok && !qty.IsZero() {
				return true
			}
		}
	}

	return false
}

// hasGPUClassNodeSelector checks if pod already has gpu.nvidia.com/class nodeSelector
func hasGPUClassNodeSelector(pod *v1.Pod) bool {
	if pod.Spec.NodeSelector == nil {
		return false
	}
	_, exists := pod.Spec.NodeSelector[GPUClassLabel]
	return exists
}

// createNodeSelectorPatches creates JSON patches to inject nodeSelector
func (pm *PodMutator) createNodeSelectorPatches(pod *v1.Pod, gpuClass string) []map[string]interface{} {
	var patches []map[string]interface{}

	if pod.Spec.NodeSelector == nil {
		// Create nodeSelector map and add GPU class
		patches = append(patches, map[string]interface{}{
			"op":   "add",
			"path": "/spec/nodeSelector",
			"value": map[string]string{
				GPUClassLabel: gpuClass,
			},
		})
	} else {
		// Add GPU class to existing nodeSelector
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/spec/nodeSelector/" + escapeJSONPointer(GPUClassLabel),
			"value": gpuClass,
		})
	}

	return patches
}

// escapeJSONPointer escapes special characters in JSON pointer path
// For gpu.nvidia.com/class, we need to escape the '/' character
func escapeJSONPointer(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	s = strings.ReplaceAll(s, "/", "~1")
	return s
}
