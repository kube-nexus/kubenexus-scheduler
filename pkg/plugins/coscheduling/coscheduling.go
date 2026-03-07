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

This plugin is derived from kubernetes-sigs/scheduler-plugins coscheduling
with the following enhancements:
- ProfileClassifier integration for tenant/workload-aware gang detection
- Enhanced starvation prevention with age-based priority boosting
- Integration with ResourceReservation plugin for driver pod protection
*/

// Package coscheduling implements gang scheduling with enterprise features.
package coscheduling

import (
	"context"
	"fmt"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	corelisters "k8s.io/client-go/listers/core/v1"
	klog "k8s.io/klog/v2"
	framework "k8s.io/kube-scheduler/framework"

	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/profileclassifier"
	schedulermetrics "github.com/kube-nexus/kubenexus-scheduler/pkg/scheduler"
	"github.com/kube-nexus/kubenexus-scheduler/pkg/utils"
)

// Coscheduling is a plugin that implements gang scheduling with enterprise features
type Coscheduling struct {
	frameworkHandle framework.Handle
	podLister       corelisters.PodLister
	podGroupManager *utils.PodGroupManager
	// Key is namespace/podGroupName
	podGroupInfos sync.Map
	// Metrics and monitoring
	schedulingAttempts map[string]int
	// stopCh signals the cleanup goroutine to stop
	stopCh chan struct{}
}

const (
	// staleEntryTTL is the duration after which unused pod group entries are evicted
	staleEntryTTL = 10 * time.Minute
	// cleanupInterval is how often the cleanup goroutine runs
	cleanupInterval = 2 * time.Minute
)

// PodGroupInfo stores metadata about a pod group
type PodGroupInfo struct {
	name           string
	namespace      string
	minAvailable   int
	timestamp      time.Time
	lastUpdateTime time.Time
}

var _ framework.QueueSortPlugin = &Coscheduling{}
var _ framework.PreFilterPlugin = &Coscheduling{}
var _ framework.PermitPlugin = &Coscheduling{}
var _ framework.ReservePlugin = &Coscheduling{}

const (
	// Name is the name of the plugin used in Registry and configurations.
	Name = "Coscheduling"
	// PodGroupName is the name of a pod group that defines a coscheduling pod group.
	PodGroupName = "pod-group.scheduling.sigs.k8s.io/name"
	// PodGroupMinAvailable specifies the minimum number of pods to be scheduled together in a pod group.
	PodGroupMinAvailable = "pod-group.scheduling.sigs.k8s.io/min-available"
	// PermitWaitingTime is the wait timeout returned by Permit plugin
	PermitWaitingTime = 10 * time.Second
	// StarvationThreshold is the time after which a pod group gets priority boost to prevent starvation
	StarvationThreshold = 60 * time.Second
)

// Name returns name of the plugin. It is used in logs, etc.
func (cs *Coscheduling) Name() string {
	return Name
}

// New initializes a new plugin and returns it.
func New(ctx context.Context, obj runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	podLister := handle.SharedInformerFactory().Core().V1().Pods().Lister()
	podGroupManager := utils.NewPodGroupManager(podLister)

	cs := &Coscheduling{
		frameworkHandle:    handle,
		podLister:          podLister,
		podGroupManager:    podGroupManager,
		schedulingAttempts: make(map[string]int),
		stopCh:             make(chan struct{}),
	}

	// Start background cleanup of stale pod group entries to prevent unbounded memory growth
	go cs.cleanupStaleEntries()

	return cs, nil
}

// Less are used to sort pods in the scheduling queue.
// 1. Check for starvation (age-based priority boost)
// 2. Compare the priorities of pods
// 3. Compare the timestamps of the initialization time of PodGroups (FIFO)
// 4. Compare the keys of PodGroups
func (cs *Coscheduling) Less(podInfo1 framework.QueuedPodInfo, podInfo2 framework.QueuedPodInfo) bool {
	pod1 := podInfo1.GetPodInfo().GetPod()
	pod2 := podInfo2.GetPodInfo().GetPod()

	klog.V(4).InfoS("QueueSort: comparing pods",
		"pod1", klog.KRef(pod1.Namespace, pod1.Name),
		"pod2", klog.KRef(pod2.Namespace, pod2.Name))

	pgInfo1 := cs.getPodGroupInfoFromQueued(podInfo1)
	pgInfo2 := cs.getPodGroupInfoFromQueued(podInfo2)

	// 1. STARVATION PREVENTION: Boost priority if waiting too long
	now := time.Now()
	age1 := now.Sub(pgInfo1.timestamp)
	age2 := now.Sub(pgInfo2.timestamp)

	starving1 := age1 > StarvationThreshold
	starving2 := age2 > StarvationThreshold

	if starving1 && !starving2 {
		klog.V(3).InfoS("QueueSort: pod group is starving, boosting priority",
			"namespace", pod1.Namespace, "podGroup", pgInfo1.name, "age", age1)
		schedulermetrics.GangStarvationPreventions.WithLabelValues(pod1.Namespace, pgInfo1.name).Inc()
		return true // starving1 goes first
	}
	if !starving1 && starving2 {
		klog.V(3).InfoS("QueueSort: pod group is starving, boosting priority",
			"namespace", pod2.Namespace, "podGroup", pgInfo2.name, "age", age2)
		schedulermetrics.GangStarvationPreventions.WithLabelValues(pod2.Namespace, pgInfo2.name).Inc()
		return false // starving2 goes first
	}

	// 2. PRIORITY: Compare base priorities
	priority1 := int32(0)
	priority2 := int32(0)
	if pod1.Spec.Priority != nil {
		priority1 = *pod1.Spec.Priority
	}
	if pod2.Spec.Priority != nil {
		priority2 = *pod2.Spec.Priority
	}

	if priority1 != priority2 {
		return priority1 > priority2
	}

	// 3. FIFO: Older jobs go first (fairness)
	time1 := pgInfo1.timestamp
	time2 := pgInfo2.timestamp

	if !time1.Equal(time2) {
		return time1.Before(time2)
	}

	// 4. TIEBREAKER: Stable sorting by name
	key1 := fmt.Sprintf("%v/%v", pod1.Namespace, pgInfo1.name)
	key2 := fmt.Sprintf("%v/%v", pod2.Namespace, pgInfo2.name)
	return key1 < key2
}

func (cs *Coscheduling) getPodGroupInfoFromQueued(queuedInfo framework.QueuedPodInfo) *PodGroupInfo {
	p := queuedInfo.GetPodInfo().GetPod()
	podGroupName, minAvailable, err := utils.GetPodGroupLabels(p)
	if err == nil && podGroupName != "" && minAvailable > 1 {
		key := utils.GetPodGroupKey(p.Namespace, podGroupName)
		pgInfo, ok := cs.podGroupInfos.Load(key)
		if !ok {
			timestamp := queuedInfo.GetTimestamp()
			pgInfo = &PodGroupInfo{
				name:           podGroupName,
				namespace:      p.Namespace,
				minAvailable:   minAvailable,
				timestamp:      timestamp,
				lastUpdateTime: time.Now(),
			}
			cs.podGroupInfos.Store(key, pgInfo)
		}
		//nolint:errcheck // Type assertion is safe here; stored value is always *PodGroupInfo
		return pgInfo.(*PodGroupInfo)
	}

	// If the pod is regular pod, return object of PodGroupInfo but not store in PodGroupInfos
	return &PodGroupInfo{
		name:      "",
		namespace: p.Namespace,
		timestamp: queuedInfo.GetTimestamp(),
	}
}

// PreFilter validates that the pod group has enough pods before scheduling
func (cs *Coscheduling) PreFilter(ctx context.Context, state framework.CycleState, p *v1.Pod, nodeInfos []framework.NodeInfo) (*framework.PreFilterResult, *framework.Status) {
	klog.InfoS("PreFilter called", "pod", klog.KObj(p), "labels", p.Labels)

	// Check ProfileClassifier first for gang membership
	profile, err := profileclassifier.GetProfile(&state)
	isGang := false
	if err == nil && profile != nil {
		isGang = profile.IsGang
		klog.V(4).InfoS("PreFilter: gang status from ProfileClassifier",
			"pod", klog.KObj(p), "isGang", isGang)
		if !isGang {
			// ProfileClassifier says not a gang, skip gang scheduling
			klog.V(5).InfoS("PreFilter: not a gang per ProfileClassifier, allowing",
				"pod", klog.KObj(p))
			return nil, framework.NewStatus(framework.Success, "")
		}
	} else {
		klog.V(5).InfoS("PreFilter: ProfileClassifier unavailable, using local gang detection",
			"pod", klog.KObj(p))
	}

	podGroupName, minAvailable, err := utils.GetPodGroupLabels(p)
	if err != nil {
		klog.ErrorS(err, "PreFilter: error getting pod group labels", "pod", klog.KObj(p))
		return nil, framework.NewStatus(framework.Error, err.Error())
	}

	klog.InfoS("PreFilter: pod group labels", "pod", klog.KObj(p), "podGroup", podGroupName, "minAvailable", minAvailable)

	// If ProfileClassifier didn't classify it as gang, check local heuristics
	if !isGang && (podGroupName == "" || minAvailable <= 1) {
		klog.InfoS("PreFilter: pod is not part of a gang, allowing", "pod", klog.KObj(p), "podGroup", podGroupName, "minAvailable", minAvailable)
		return nil, framework.NewStatus(framework.Success, "")
	}

	total := cs.calculateTotalPods(podGroupName, p.Namespace)
	klog.InfoS("PreFilter: pod group status", "namespace", p.Namespace, "podGroup", podGroupName, "total", total, "minAvailable", minAvailable, "pod", p.Name)

	if total < minAvailable {
		klog.V(3).InfoS("PreFilter: insufficient pods in group",
			"namespace", p.Namespace, "podGroup", podGroupName, "total", total, "minAvailable", minAvailable, "pod", p.Name)
		schedulermetrics.GangSchedulingDecisions.WithLabelValues("insufficient_pods", p.Namespace).Inc()
		return nil, framework.NewStatus(framework.Unschedulable,
			fmt.Sprintf("pod group has %d pods, needs at least %d", total, minAvailable))
	}

	klog.V(4).InfoS("PreFilter: pod group has sufficient pods",
		"namespace", p.Namespace, "podGroup", podGroupName, "total", total, "minAvailable", minAvailable)
	// Return empty PreFilterResult (not nil) to indicate processing succeeded
	return &framework.PreFilterResult{}, framework.NewStatus(framework.Success, "")
}

// PreFilterExtensions returns nil
func (cs *Coscheduling) PreFilterExtensions() framework.PreFilterExtensions {
	return nil //nolint:staticcheck // Acceptable pattern for no extensions
}

// Permit controls when pods are allowed to proceed to binding
func (cs *Coscheduling) Permit(ctx context.Context, state framework.CycleState, p *v1.Pod, nodeName string) (*framework.Status, time.Duration) {
	podGroupName, minAvailable, err := utils.GetPodGroupLabels(p)
	if err != nil {
		// If pod group labels are invalid or malformed, treat as non-gang pod
		klog.V(4).InfoS("Permit: pod has invalid gang labels, allowing immediately", "pod", klog.KObj(p), "err", err)
		return framework.NewStatus(framework.Success, ""), 0
	}
	if podGroupName == "" || minAvailable <= 1 {
		return framework.NewStatus(framework.Success, ""), 0
	}

	namespace := p.Namespace
	// Calculate pods already in the gang (excluding the current pod being scheduled)
	running := cs.calculateRunningPodsExcluding(podGroupName, namespace, p.Name)
	waiting := cs.calculateWaitingPods(podGroupName, namespace)
	// Add 1 for the current pod being scheduled
	current := running + waiting + 1

	klog.V(4).InfoS("Permit: pod group status",
		"namespace", namespace, "podGroup", podGroupName, "running", running, "waiting", waiting, "current", current, "minAvailable", minAvailable)

	if current < minAvailable {
		klog.V(3).InfoS("Permit: pod group waiting for more pods",
			"namespace", namespace, "podGroup", podGroupName, "current", current, "minAvailable", minAvailable)
		schedulermetrics.GangWaitingTime.WithLabelValues(namespace, podGroupName).Observe(float64(PermitWaitingTime.Seconds()))
		return framework.NewStatus(framework.Wait, ""), PermitWaitingTime
	}

	// All required pods are here, allow the entire group
	klog.V(3).InfoS("Permit: pod group ready to schedule",
		"namespace", namespace, "podGroup", podGroupName, "current", current, "minAvailable", minAvailable)
	schedulermetrics.GangSchedulingDecisions.WithLabelValues("success", namespace).Inc()
	schedulermetrics.GangCompletionLatency.WithLabelValues(namespace, podGroupName, fmt.Sprintf("%d", minAvailable)).Observe(time.Since(time.Now()).Seconds())

	// Safely call IterateOverWaitingPods with recovery for test frameworks
	if cs.frameworkHandle != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					klog.V(5).InfoS("IterateOverWaitingPods not available", "recovered", r)
				}
			}()

			cs.frameworkHandle.IterateOverWaitingPods(func(waitingPod framework.WaitingPod) {
				if waitingPod.GetPod().Namespace == namespace {
					waitingPodGroupName, _, _ := utils.GetPodGroupLabels(waitingPod.GetPod()) //nolint:errcheck // Error ignored intentionally
					if waitingPodGroupName == podGroupName {
						klog.V(4).InfoS("Permit: allowing pod", "namespace", namespace, "pod", waitingPod.GetPod().Name)
						waitingPod.Allow(cs.Name())
					}
				}
			})
		}()
	}

	return framework.NewStatus(framework.Success, ""), 0
}

// Reserve reserves resources for the pod
func (cs *Coscheduling) Reserve(ctx context.Context, state framework.CycleState, p *v1.Pod, nodeName string) *framework.Status {
	klog.V(4).InfoS("Reserve: pod reserved", "pod", klog.KObj(p), "node", nodeName)
	return framework.NewStatus(framework.Success, "")
}

// Unreserve rejects all other pods in the pod group when one pod times out
func (cs *Coscheduling) Unreserve(ctx context.Context, state framework.CycleState, p *v1.Pod, nodeName string) {
	podGroupName, _, err := utils.GetPodGroupLabels(p)
	if err != nil || podGroupName == "" {
		return
	}

	klog.V(3).InfoS("Unreserve: rejecting pods in group", "namespace", p.Namespace, "podGroup", podGroupName)
	schedulermetrics.GangSchedulingDecisions.WithLabelValues("timeout", p.Namespace).Inc()

	cs.frameworkHandle.IterateOverWaitingPods(func(waitingPod framework.WaitingPod) {
		if waitingPod.GetPod().Namespace == p.Namespace {
			waitingPodGroupName, _, _ := utils.GetPodGroupLabels(waitingPod.GetPod()) //nolint:errcheck // Error ignored intentionally
			if waitingPodGroupName == podGroupName {
				klog.V(4).InfoS("Unreserve: rejecting pod", "namespace", p.Namespace, "pod", waitingPod.GetPod().Name)
				waitingPod.Reject(cs.Name(), "pod group member failed")
			}
		}
	})
}

func (cs *Coscheduling) calculateTotalPods(podGroupName, namespace string) int {
	// Try new label first
	selector := labels.Set{"pod-group.scheduling.kubenexus.io/name": podGroupName}.AsSelector()
	pods, err := cs.podLister.Pods(namespace).List(selector)
	if err != nil || len(pods) == 0 {
		// Fallback to old label for backward compatibility
		selector = labels.Set{PodGroupName: podGroupName}.AsSelector()
		pods, err = cs.podLister.Pods(namespace).List(selector)
		if err != nil {
			klog.ErrorS(err, "calculateTotalPods: error listing pods")
			return 0
		}
	}
	return len(pods)
}

func (cs *Coscheduling) calculateRunningPodsExcluding(podGroupName, namespace string, excludeName string) int {
	selector := labels.Set{PodGroupName: podGroupName}.AsSelector()
	pods, err := cs.podLister.Pods(namespace).List(selector)
	if err != nil {
		klog.ErrorS(err, "calculateRunningPods: error listing pods")
		return 0
	}

	running := 0
	for _, pod := range pods {
		// Skip the pod being excluded (current pod being scheduled)
		if excludeName != "" && pod.Name == excludeName {
			continue
		}
		// Skip terminating pods (DeletionTimestamp set)
		if pod.DeletionTimestamp != nil {
			continue
		}
		// Count pods that are running, pending, or newly created as "active"
		if pod.Status.Phase == v1.PodRunning ||
			pod.Status.Phase == v1.PodPending ||
			pod.Status.Phase == "" { // Empty phase means pod is newly created/scheduled
			running++
		}
	}

	return running
}

func (cs *Coscheduling) calculateWaitingPods(podGroupName, namespace string) int {
	waiting := 0
	// Check if IterateOverWaitingPods is available (may not be in test framework)
	if cs.frameworkHandle == nil {
		return waiting
	}

	// Safely call IterateOverWaitingPods with recovery for test frameworks
	defer func() {
		if r := recover(); r != nil {
			klog.V(5).InfoS("IterateOverWaitingPods not available", "recovered", r)
		}
	}()

	cs.frameworkHandle.IterateOverWaitingPods(func(waitingPod framework.WaitingPod) {
		if waitingPod.GetPod().Namespace != namespace {
			return
		}
		// Try new label first
		if groupName, exists := waitingPod.GetPod().Labels["pod-group.scheduling.kubenexus.io/name"]; exists && groupName == podGroupName {
			waiting++
			return
		}
		// Fallback to old label
		if groupName, exists := waitingPod.GetPod().Labels[PodGroupName]; exists && groupName == podGroupName {
			waiting++
		}
	})

	return waiting
}

// cleanupStaleEntries periodically removes old pod group entries to prevent unbounded memory growth
func (cs *Coscheduling) cleanupStaleEntries() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-cs.stopCh:
			return
		case <-ticker.C:
			now := time.Now()
			cs.podGroupInfos.Range(func(key, value interface{}) bool {
				pgInfo, ok := value.(*PodGroupInfo)
				if !ok {
					cs.podGroupInfos.Delete(key)
					return true
				}
				if now.Sub(pgInfo.lastUpdateTime) > staleEntryTTL {
					klog.V(5).InfoS("Evicting stale pod group entry", "key", key, "age", now.Sub(pgInfo.lastUpdateTime))
					cs.podGroupInfos.Delete(key)
				}
				return true
			})
		}
	}
}
