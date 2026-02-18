/*
Copyright 2020 The Kubernetes Authors.

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
	"k8s.io/klog/v2"
	framework "k8s.io/kube-scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/utils"
)

// Coscheduling implements intelligent workload-aware scheduling with support for:
//
//  1. GANG SCHEDULING (All-or-Nothing):
//     For distributed workloads (ML training, Spark, MPI) where ALL pods in a
//     group must be scheduled together atomically. Pods wait for each other.
//     Triggered by: pod-group.scheduling.kubenexus.io/name label + minAvailable > 1
//
//     Example: 8-worker ML training job - ALL 8 pods must be ready before any start.
//
//  2. INDEPENDENT SCHEDULING:
//     For stateless services (APIs, web apps, databases) where each pod schedules
//     independently for faster availability and rolling updates.
//     Triggered by: No gang labels OR minAvailable <= 1
//
//     Example: API with 10 replicas - each pod starts as soon as resources available.
//
// 3. INTELLIGENT QUEUE SORTING:
//   - Prevents starvation (age-based priority boost after 60s)
//   - Respects pod priorities
//   - FIFO fairness within same priority
//
// The plugin automatically detects which mode to use based on pod labels.
// Services get fast, independent scheduling. Batch gets gang semantics.
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
	// "Coscheduling" reflects coordinated scheduling of related pods, supporting both:
	// - Gang scheduling (all-or-nothing for distributed workloads)
	// - Independent scheduling (for stateless services)
	Name = "Coscheduling"

	// PodGroupName labels a pod as part of a gang (pod group).
	// Pods WITHOUT this label are scheduled independently (normal K8s behavior).
	// Pods WITH this label may use gang scheduling based on minAvailable value.
	PodGroupName = "pod-group.scheduling.sigs.k8s.io/name"

	// PodGroupMinAvailable specifies minimum pods needed for gang scheduling.
	// - If minAvailable <= 1: Pod schedules independently (no gang, fast deployment)
	// - If minAvailable > 1: Gang scheduling applies (all-or-nothing, waits for peers)
	PodGroupMinAvailable = "pod-group.scheduling.sigs.k8s.io/min-available"

	// PermitWaitingTime is the wait timeout returned by Permit plugin.
	// Pods in a gang will wait up to this duration for all members to be ready.
	// After timeout, the entire gang is rejected and retried later.
	PermitWaitingTime = 10 * time.Second

	// StarvationThreshold is the time after which a pod group gets priority boost.
	// This prevents long-waiting gangs from being starved by newer high-priority work.
	// After 60s of waiting, the gang gets bumped ahead in the queue.
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
		return pgInfo.(*PodGroupInfo)
	}

	// If the pod is regular pod, return object of PodGroupInfo but not store in PodGroupInfos.
	// Regular pods (services, deployments without gang labels) are treated independently.
	return &PodGroupInfo{
		name:      "",
		namespace: p.Namespace,
		timestamp: queuedInfo.GetTimestamp(),
	}
}

// PreFilter validates that the pod group has enough pods before scheduling.
//
// FOR GANG SCHEDULING (minAvailable > 1):
//   - Ensures all required pods exist before attempting to schedule any
//   - Prevents partial gang scheduling that would waste resources
//   - Returns Unschedulable if gang is incomplete
//
// FOR INDEPENDENT SCHEDULING (no labels or minAvailable <= 1):
//   - Skips gang validation, returns Success immediately
//   - Pod schedules independently without waiting for others
func (cs *Coscheduling) PreFilter(ctx context.Context, state framework.CycleState, p *v1.Pod, nodeInfos []framework.NodeInfo) (*framework.PreFilterResult, *framework.Status) {
	podGroupName, minAvailable, err := utils.GetPodGroupLabels(p)
	if err != nil {
		return nil, framework.NewStatus(framework.Error, err.Error())
	}
	if podGroupName == "" || minAvailable <= 1 {
		// Not a gang pod - schedule independently
		return nil, framework.NewStatus(framework.Success, "")
	}

	total := cs.calculateTotalPods(podGroupName, p.Namespace)
	if total < minAvailable {
		klog.V(3).Infof("PreFilter: podGroup %s/%s has %d pods, needs %d (pod: %s)",
			p.Namespace, podGroupName, total, minAvailable, p.Name)
		return nil, framework.NewStatus(framework.Unschedulable,
			fmt.Sprintf("pod group has %d pods, needs at least %d", total, minAvailable))
	}

	klog.V(4).Infof("PreFilter: podGroup %s/%s has sufficient pods (%d >= %d)",
		p.Namespace, podGroupName, total, minAvailable)
	return nil, framework.NewStatus(framework.Success, "")
}

// PreFilterExtensions returns nil
func (cs *Coscheduling) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

// Permit controls when pods are allowed to proceed to binding.
//
// FOR GANG SCHEDULING (minAvailable > 1):
//   - Waits until ALL required pods in the gang have passed scheduling
//   - Only allows binding when minAvailable pods are ready
//   - Atomic release: ALL pods in gang proceed together
//   - Timeout: If gang incomplete after PermitWaitingTime, reject all and retry
//
// FOR INDEPENDENT SCHEDULING (no labels or minAvailable <= 1):
//   - Returns Success immediately, no waiting
//   - Pod proceeds to binding right away
//   - Fast deployment for services
func (cs *Coscheduling) Permit(ctx context.Context, state framework.CycleState, p *v1.Pod, nodeName string) (*framework.Status, time.Duration) {
	podGroupName, minAvailable, err := utils.GetPodGroupLabels(p)
	if err != nil {
		return framework.NewStatus(framework.Error, err.Error()), 0
	}
	if podGroupName == "" || minAvailable <= 1 {
		// Not a gang pod - proceed immediately
		return framework.NewStatus(framework.Success, ""), 0
	}

	namespace := p.Namespace
	running := cs.calculateRunningPods(podGroupName, namespace)
	waiting := cs.calculateWaitingPods(podGroupName, namespace)
	current := running + waiting + 1

	klog.V(4).Infof("Permit: podGroup %s/%s - running: %d, waiting: %d, current: %d, minAvailable: %d",
		namespace, podGroupName, running, waiting, current, minAvailable)

	if current < minAvailable {
		klog.V(3).Infof("Permit: podGroup %s/%s waiting for more pods (%d/%d)",
			namespace, podGroupName, current, minAvailable)
		return framework.NewStatus(framework.Wait, ""), PermitWaitingTime
	}

	// All required pods are here, allow the entire group
	klog.V(3).Infof("Permit: podGroup %s/%s ready to schedule (%d/%d)",
		namespace, podGroupName, current, minAvailable)

	cs.frameworkHandle.IterateOverWaitingPods(func(waitingPod framework.WaitingPod) {
		if waitingPod.GetPod().Namespace == namespace {
			waitingPodGroupName, _, _ := utils.GetPodGroupLabels(waitingPod.GetPod())
			if waitingPodGroupName == podGroupName {
				klog.V(4).Infof("Permit: allowing pod %s/%s", namespace, waitingPod.GetPod().Name)
				waitingPod.Allow(cs.Name())
			}
		}
	})

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
			waitingPodGroupName, _, _ := utils.GetPodGroupLabels(waitingPod.GetPod())
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

func (cs *Coscheduling) calculateRunningPods(podGroupName, namespace string) int {
	selector := labels.Set{PodGroupName: podGroupName}.AsSelector()
	pods, err := cs.podLister.Pods(namespace).List(selector)
	if err != nil {
		klog.Errorf("calculateRunningPods: error: %v", err)
		return 0
	}

	running := 0
	for _, pod := range pods {
		if pod.Status.Phase == v1.PodRunning || pod.Status.Phase == v1.PodSucceeded {
			running++
		}
	}

	return running
}

func (cs *Coscheduling) calculateWaitingPods(podGroupName, namespace string) int {
	waiting := 0
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
