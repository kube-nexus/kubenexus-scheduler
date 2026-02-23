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

package profileclassifier

import (
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	klog "k8s.io/klog/v2"
	framework "k8s.io/kube-scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/workload"
)

const (
	// Name is the name of the plugin used in the plugin registry and configurations.
	Name = "ProfileClassifier"

	// stateKey is the key in CycleState where SchedulingProfile is stored
	stateKey = "ProfileClassifier"
)

// TenantTier represents the tenant priority tier
type TenantTier string

const (
	TierGold    TenantTier = "gold"
	TierSilver  TenantTier = "silver"
	TierBronze  TenantTier = "bronze"
	TierUnknown TenantTier = "unknown"
)

// WorkloadType represents the classification of workload
type WorkloadType string

const (
	WorkloadTraining    WorkloadType = "training"
	WorkloadInference   WorkloadType = "inference"
	WorkloadBatch       WorkloadType = "batch"
	WorkloadService     WorkloadType = "service"
	WorkloadInteractive WorkloadType = "interactive"
	WorkloadUnknown     WorkloadType = "unknown"
)

// SchedulingProfile contains the classification result for a pod
type SchedulingProfile struct {
	TenantTier    TenantTier
	TenantName    string
	WorkloadType  WorkloadType
	IsGang        bool
	IsPreemptible bool
	Priority      int32
	QoSClass      v1.PodQOSClass
}

// ProfileClassifier classifies pods into tenant tiers and workload types
type ProfileClassifier struct {
	handle framework.Handle
}

var _ framework.PreFilterPlugin = &ProfileClassifier{}

// Name returns name of the plugin.
func (pl *ProfileClassifier) Name() string {
	return Name
}

// New initializes a new plugin and returns it.
func New(_ context.Context, _ runtime.Object, h framework.Handle) (framework.Plugin, error) {
	return &ProfileClassifier{
		handle: h,
	}, nil
}

// PreFilter classifies the pod and stores the profile in CycleState
func (pl *ProfileClassifier) PreFilter(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfo []framework.NodeInfo) (*framework.PreFilterResult, *framework.Status) {
	profile := pl.classifyPod(ctx, pod)

	state.Write(stateKey, profile)

	klog.V(4).InfoS("Classified pod",
		"pod", klog.KObj(pod),
		"tenantTier", profile.TenantTier,
		"tenantName", profile.TenantName,
		"workloadType", profile.WorkloadType,
		"isGang", profile.IsGang,
		"isPreemptible", profile.IsPreemptible,
	)

	return nil, nil
}

// PreFilterExtensions returns prefilter extensions, pod add and remove.
func (pl *ProfileClassifier) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

// classifyPod performs the actual classification logic
func (pl *ProfileClassifier) classifyPod(ctx context.Context, pod *v1.Pod) *SchedulingProfile {
	profile := &SchedulingProfile{
		Priority: getPodPriority(pod),
		QoSClass: pod.Status.QOSClass,
	}

	profile.TenantTier, profile.TenantName = pl.classifyTenant(ctx, pod)
	profile.WorkloadType = pl.classifyWorkload(pod)
	profile.IsGang = isGangPod(pod)
	profile.IsPreemptible = isPreemptible(pod)

	return profile
}

// classifyTenant determines tenant tier and name
func (pl *ProfileClassifier) classifyTenant(ctx context.Context, pod *v1.Pod) (TenantTier, string) {
	if tier, name := pl.getTenantFromKueue(ctx, pod); tier != TierUnknown {
		return tier, name
	}

	if tier, name := pl.getTenantFromNamespace(ctx, pod); tier != TierUnknown {
		return tier, name
	}

	if tier := pl.getTenantFromPriority(pod); tier != TierUnknown {
		return tier, pod.Namespace
	}

	if tier := getTenantFromAnnotations(pod); tier != TierUnknown {
		return tier, pod.Namespace
	}

	return TierBronze, pod.Namespace
}

// getTenantFromKueue reads tenant info from Kueue labels
func (pl *ProfileClassifier) getTenantFromKueue(ctx context.Context, pod *v1.Pod) (TenantTier, string) {
	queueName, hasQueue := pod.Labels["kueue.x-k8s.io/queue-name"]
	if !hasQueue {
		return TierUnknown, ""
	}

	ns, err := pl.handle.ClientSet().CoreV1().Namespaces().Get(ctx, pod.Namespace, metav1.GetOptions{})
	if err != nil {
		klog.V(4).InfoS("Failed to get namespace for Kueue classification", "namespace", pod.Namespace, "error", err)
		return TierUnknown, ""
	}

	if tier := parseTenantTier(ns.Labels["tenant.kubenexus.io/tier"]); tier != TierUnknown {
		return tier, queueName
	}

	if tenantName, ok := ns.Labels["tenant.kubenexus.io/name"]; ok {
		if tier := inferTierFromName(tenantName); tier != TierUnknown {
			return tier, tenantName
		}
		return TierBronze, tenantName
	}

	return TierUnknown, ""
}

// getTenantFromNamespace reads tenant info from namespace labels
func (pl *ProfileClassifier) getTenantFromNamespace(ctx context.Context, pod *v1.Pod) (TenantTier, string) {
	ns, err := pl.handle.ClientSet().CoreV1().Namespaces().Get(ctx, pod.Namespace, metav1.GetOptions{})
	if err != nil {
		klog.V(5).InfoS("Failed to get namespace", "namespace", pod.Namespace, "error", err)
		return TierUnknown, ""
	}

	if tierStr, ok := ns.Labels["tenant.kubenexus.io/tier"]; ok {
		tier := parseTenantTier(tierStr)
		tenantName := ns.Labels["tenant.kubenexus.io/name"]
		if tenantName == "" {
			tenantName = pod.Namespace
		}
		return tier, tenantName
	}

	return TierUnknown, ""
}

// getTenantFromPriority infers tenant tier from PriorityClassName
func (pl *ProfileClassifier) getTenantFromPriority(pod *v1.Pod) TenantTier {
	if pod.Spec.PriorityClassName == "" {
		return TierUnknown
	}

	switch pod.Spec.PriorityClassName {
	case "high-priority", "system-cluster-critical", "system-node-critical":
		return TierGold
	case "medium-priority", "default-priority":
		return TierSilver
	case "low-priority", "best-effort":
		return TierBronze
	default:
		return TierUnknown
	}
}

// getTenantFromAnnotations reads tenant tier from pod annotations
func getTenantFromAnnotations(pod *v1.Pod) TenantTier {
	if tierStr, ok := pod.Annotations["tenant.kubenexus.io/tier"]; ok {
		return parseTenantTier(tierStr)
	}
	return TierUnknown
}

// classifyWorkload determines workload type
func (pl *ProfileClassifier) classifyWorkload(pod *v1.Pod) WorkloadType {
	if wlType, ok := pod.Labels["workload.kubenexus.io/type"]; ok {
		return parseWorkloadType(wlType)
	}

	if wlType, ok := pod.Annotations["workload.kubenexus.io/type"]; ok {
		return parseWorkloadType(wlType)
	}

	basicType := workload.ClassifyPod(pod)

	switch basicType {
	case workload.TypeBatch:
		if isTrainingWorkload(pod) {
			return WorkloadTraining
		}
		return WorkloadBatch
	case workload.TypeService:
		if isInferenceWorkload(pod) {
			return WorkloadInference
		}
		return WorkloadService
	default:
		return WorkloadUnknown
	}
}

// isTrainingWorkload detects training jobs
func isTrainingWorkload(pod *v1.Pod) bool {
	if _, ok := pod.Labels["training.kubeflow.org/job-name"]; ok {
		return true
	}
	if _, ok := pod.Labels["pytorch.org/job-name"]; ok {
		return true
	}
	if _, ok := pod.Labels["tensorflow.org/job-name"]; ok {
		return true
	}
	if isGangPod(pod) {
		return true
	}
	if gpuCount := getGPURequest(pod); gpuCount > 1 {
		return true
	}
	return false
}

// isInferenceWorkload detects inference serving
func isInferenceWorkload(pod *v1.Pod) bool {
	if _, ok := pod.Labels["serving.kubeflow.org/inferenceservice"]; ok {
		return true
	}
	if _, ok := pod.Labels["serving.kserve.io/inferenceservice"]; ok {
		return true
	}
	if _, ok := pod.Labels["seldon.io/deployment"]; ok {
		return true
	}
	if role, ok := pod.Labels["app.kubernetes.io/component"]; ok {
		if role == "predictor" || role == "inference" || role == "serving" {
			return true
		}
	}
	return false
}

// isGangPod checks if pod is part of gang scheduling
func isGangPod(pod *v1.Pod) bool {
	if _, ok := pod.Annotations["scheduling.kubenexus.io/min-available"]; ok {
		return true
	}
	if _, ok := pod.Annotations["scheduling.x-k8s.io/pod-group"]; ok {
		return true
	}
	if _, ok := pod.Labels["gang.scheduling.kubenexus.io/name"]; ok {
		return true
	}
	return false
}

// isPreemptible checks if workload is preemptible
func isPreemptible(pod *v1.Pod) bool {
	if preemptible, ok := pod.Labels["workload.kubenexus.io/preemptible"]; ok {
		return preemptible == "true"
	}

	if pod.Spec.PriorityClassName == "low-priority" || pod.Spec.PriorityClassName == "best-effort" {
		return true
	}

	if pod.Spec.Priority != nil && *pod.Spec.Priority <= 100 {
		return true
	}

	return false
}

// Helper functions

func parseTenantTier(s string) TenantTier {
	switch strings.ToLower(s) {
	case "gold":
		return TierGold
	case "silver":
		return TierSilver
	case "bronze":
		return TierBronze
	default:
		return TierUnknown
	}
}

func parseWorkloadType(s string) WorkloadType {
	switch strings.ToLower(s) {
	case "training":
		return WorkloadTraining
	case "inference":
		return WorkloadInference
	case "batch":
		return WorkloadBatch
	case "service":
		return WorkloadService
	case "interactive":
		return WorkloadInteractive
	default:
		return WorkloadUnknown
	}
}

func inferTierFromName(name string) TenantTier {
	nameLower := strings.ToLower(name)
	if strings.Contains(nameLower, "prod") {
		return TierGold
	}
	if strings.Contains(nameLower, "staging") {
		return TierSilver
	}
	if strings.Contains(nameLower, "dev") {
		return TierBronze
	}
	return TierUnknown
}

func getPodPriority(pod *v1.Pod) int32 {
	if pod.Spec.Priority != nil {
		return *pod.Spec.Priority
	}
	return 0
}

func getGPURequest(pod *v1.Pod) int64 {
	var totalGPU int64
	for _, container := range pod.Spec.Containers {
		if gpu, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
			totalGPU += gpu.Value()
		}
	}
	return totalGPU
}

// GetProfile retrieves the SchedulingProfile from CycleState
func GetProfile(state *framework.CycleState) (*SchedulingProfile, error) {
	if state == nil {
		return nil, fmt.Errorf("cycleState is nil")
	}

	c, err := (*state).Read(stateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to read %q from cycleState: %w", stateKey, err)
	}

	profile, ok := c.(*SchedulingProfile)
	if !ok {
		return nil, fmt.Errorf("invalid SchedulingProfile type in cycleState")
	}

	return profile, nil
}

// Clone creates a deep copy of the SchedulingProfile
func (p *SchedulingProfile) Clone() framework.StateData {
	return &SchedulingProfile{
		TenantTier:    p.TenantTier,
		TenantName:    p.TenantName,
		WorkloadType:  p.WorkloadType,
		IsGang:        p.IsGang,
		IsPreemptible: p.IsPreemptible,
		Priority:      p.Priority,
		QoSClass:      p.QoSClass,
	}
}
