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
}

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

	return &Coscheduling{
		frameworkHandle:    handle,
		podLister:          podLister,
		podGroupManager:    podGroupManager,
		schedulingAttempts: make(map[string]int),
	}, nil
}

// Less are used to sort pods in the scheduling queue.
// 1. Check for starvation (age-based priority boost)
// 2. Compare the priorities of pods
// 3. Compare the timestamps of the initialization time of PodGroups (FIFO)
// 4. Compare the keys of PodGroups
func (cs *Coscheduling) Less(podInfo1 framework.QueuedPodInfo, podInfo2 framework.QueuedPodInfo) bool {
	pod1 := podInfo1.GetPodInfo().GetPod()
	pod2 := podInfo2.GetPodInfo().GetPod()

	klog.V(4).Infof("QueueSort: comparing pods %s/%s and %s/%s",
		pod1.Namespace, pod1.Name,
		pod2.Namespace, pod2.Name)

	pgInfo1 := cs.getPodGroupInfoFromQueued(podInfo1)
	pgInfo2 := cs.getPodGroupInfoFromQueued(podInfo2)

	// 1. STARVATION PREVENTION: Boost priority if waiting too long
	now := time.Now()
	age1 := now.Sub(pgInfo1.timestamp)
	age2 := now.Sub(pgInfo2.timestamp)

	starving1 := age1 > StarvationThreshold
	starving2 := age2 > StarvationThreshold

	if starving1 && !starving2 {
		klog.V(3).Infof("QueueSort: pod group %s/%s is starving (age: %v), boosting priority",
			pod1.Namespace, pgInfo1.name, age1)
		return true // starving1 goes first
	}
	if !starving1 && starving2 {
		klog.V(3).Infof("QueueSort: pod group %s/%s is starving (age: %v), boosting priority",
			pod2.Namespace, pgInfo2.name, age2)
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
	klog.Infof("PreFilter CALLED for pod %s/%s with labels: %v", p.Namespace, p.Name, p.Labels)

	// Check ProfileClassifier first for gang membership
	profile, err := profileclassifier.GetProfile(&state)
	isGang := false
	if err == nil && profile != nil {
		isGang = profile.IsGang
		klog.V(4).Infof("PreFilter: pod %s/%s gang status from ProfileClassifier: %v",
			p.Namespace, p.Name, isGang)
		if !isGang {
			// ProfileClassifier says not a gang, skip gang scheduling
			klog.V(5).Infof("PreFilter: pod %s/%s not a gang per ProfileClassifier, allowing",
				p.Namespace, p.Name)
			return nil, framework.NewStatus(framework.Success, "")
		}
	} else {
		klog.V(5).Infof("PreFilter: ProfileClassifier unavailable for pod %s/%s, using local gang detection",
			p.Namespace, p.Name)
	}

	podGroupName, minAvailable, err := utils.GetPodGroupLabels(p)
	if err != nil {
		klog.Errorf("PreFilter ERROR getting pod group labels for %s/%s: %v", p.Namespace, p.Name, err)
		return nil, framework.NewStatus(framework.Error, err.Error())
	}

	klog.Infof("PreFilter: pod %s/%s has podGroupName=%s, minAvailable=%d", p.Namespace, p.Name, podGroupName, minAvailable)

	// If ProfileClassifier didn't classify it as gang, check local heuristics
	if !isGang && (podGroupName == "" || minAvailable <= 1) {
		klog.Infof("PreFilter: pod %s/%s is not part of a gang (name=%s, min=%d), allowing", p.Namespace, p.Name, podGroupName, minAvailable)
		return nil, framework.NewStatus(framework.Success, "")
	}

	total := cs.calculateTotalPods(podGroupName, p.Namespace)
	klog.Infof("PreFilter: podGroup %s/%s has %d pods, needs %d (pod: %s)", p.Namespace, podGroupName, total, minAvailable, p.Name)

	// Note: We don't reject here even if gang is incomplete
	// The Permit phase will handle waiting for all gang members
	klog.V(4).Infof("PreFilter: allowing pod %s/%s to proceed to Permit phase (gang: %d/%d)",
		p.Namespace, p.Name, total, minAvailable)

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
		klog.V(4).Infof("Permit: pod %s/%s has invalid gang labels, allowing immediately: %v", p.Namespace, p.Name, err)
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

	klog.V(4).Infof("Permit: podGroup %s/%s - running: %d, waiting: %d, current: %d, minAvailable: %d",
		namespace, podGroupName, running, waiting, current, minAvailable)

	if current < minAvailable {
		klog.V(3).Infof("Permit: podGroup %s/%s waiting for more pods (%d/%d)",
			namespace, podGroupName, current, minAvailable)

		// Emit event for visibility
		if cs.frameworkHandle != nil && cs.frameworkHandle.EventRecorder() != nil {
			cs.frameworkHandle.EventRecorder().Eventf(p, nil, v1.EventTypeNormal, "GangScheduling", "Waiting",
				"Waiting for gang members: %d/%d pods ready (need %d more)", current, minAvailable, minAvailable-current)
		}

		return framework.NewStatus(framework.Wait, ""), PermitWaitingTime
	}

	// All required pods are here, allow the entire group
	klog.V(3).Infof("Permit: podGroup %s/%s ready to schedule (%d/%d)",
		namespace, podGroupName, current, minAvailable)

	// Emit event when gang is complete
	if cs.frameworkHandle != nil && cs.frameworkHandle.EventRecorder() != nil {
		cs.frameworkHandle.EventRecorder().Eventf(p, nil, v1.EventTypeNormal, "GangScheduling", "GangComplete",
			"All gang members ready: %d/%d pods", current, minAvailable)
	}

	// Safely call IterateOverWaitingPods with recovery for test frameworks
	if cs.frameworkHandle != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					klog.V(5).Infof("IterateOverWaitingPods not available (test mode?): %v", r)
				}
			}()

			cs.frameworkHandle.IterateOverWaitingPods(func(waitingPod framework.WaitingPod) {
				if waitingPod.GetPod().Namespace == namespace {
					waitingPodGroupName, _, _ := utils.GetPodGroupLabels(waitingPod.GetPod()) //nolint:errcheck // Error ignored intentionally
					if waitingPodGroupName == podGroupName {
						klog.V(4).Infof("Permit: allowing pod %s/%s", namespace, waitingPod.GetPod().Name)
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
	klog.V(4).Infof("Reserve: pod %s/%s on node %s", p.Namespace, p.Name, nodeName)
	return framework.NewStatus(framework.Success, "")
}

// Unreserve rejects all other pods in the pod group when one pod times out
func (cs *Coscheduling) Unreserve(ctx context.Context, state framework.CycleState, p *v1.Pod, nodeName string) {
	podGroupName, _, err := utils.GetPodGroupLabels(p)
	if err != nil || podGroupName == "" {
		return
	}

	klog.V(3).Infof("Unreserve: rejecting pods in group %s/%s", p.Namespace, podGroupName)

	cs.frameworkHandle.IterateOverWaitingPods(func(waitingPod framework.WaitingPod) {
		if waitingPod.GetPod().Namespace == p.Namespace {
			waitingPodGroupName, _, _ := utils.GetPodGroupLabels(waitingPod.GetPod()) //nolint:errcheck // Error ignored intentionally
			if waitingPodGroupName == podGroupName {
				klog.V(4).Infof("Unreserve: rejecting pod %s/%s", p.Namespace, waitingPod.GetPod().Name)
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
			klog.Errorf("calculateTotalPods: error listing pods: %v", err)
			return 0
		}
	}
	return len(pods)
}

func (cs *Coscheduling) calculateRunningPodsExcluding(podGroupName, namespace string, excludeName string) int {
	// Try new label first
	selector := labels.Set{"pod-group.scheduling.kubenexus.io/name": podGroupName}.AsSelector()
	pods, err := cs.podLister.Pods(namespace).List(selector)
	if err != nil || len(pods) == 0 {
		// Fallback to old label for backward compatibility
		selector = labels.Set{PodGroupName: podGroupName}.AsSelector()
		pods, err = cs.podLister.Pods(namespace).List(selector)
		if err != nil {
			klog.Errorf("calculateRunningPods: error: %v", err)
			return 0
		}
	}

	running := 0
	for _, pod := range pods {
		// Skip the pod being excluded (current pod being scheduled)
		if excludeName != "" && pod.Name == excludeName {
			continue
		}
		// Count pods that are running, succeeded, scheduled, or in any active state
		// For gang scheduling, we count all non-failed, non-succeeded pods as "active"
		if pod.Status.Phase == v1.PodRunning ||
			pod.Status.Phase == v1.PodPending ||
			pod.Status.Phase == v1.PodSucceeded ||
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
			klog.V(5).Infof("IterateOverWaitingPods not available (test mode?): %v", r)
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
