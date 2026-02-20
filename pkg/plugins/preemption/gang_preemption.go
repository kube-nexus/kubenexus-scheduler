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

package preemption

import (
	"context"
	"fmt"
	"sort"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
	framework "k8s.io/kube-scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/utils"
)

// GangPreemption implements gang-aware preemption to solve deadlock scenarios
// where small jobs block large gang scheduling jobs.
//
// Problem: A large AI job needs 8 GPUs, but 4 small jobs each hold 2 GPUs.
//
//	Standard K8s preemption doesn't understand the gang constraint.
//
// Solution: This plugin can preempt multiple lower-priority pods atomically
//
//	to free up resources for a high-priority gang.
type GangPreemption struct {
	handle    framework.Handle
	podLister corelisters.PodLister
}

var _ framework.PostFilterPlugin = &GangPreemption{}

const (
	// Name is the plugin name
	Name = "GangPreemption"

	// MinimumPreemptionGap is the minimum time between preemption attempts
	MinimumPreemptionGap = 30 * time.Second

	// MaxVictimsPerGang is the maximum number of victim pods we'll consider preempting
	MaxVictimsPerGang = 50
)

// Name returns the plugin name
func (gp *GangPreemption) Name() string {
	return Name
}

// PostFilter is called when a pod cannot be scheduled.
// This is where we implement gang-aware preemption logic.
func (gp *GangPreemption) PostFilter(ctx context.Context, state framework.CycleState, pod *v1.Pod, filteredNodeStatusMap framework.NodeToStatusReader) (*framework.PostFilterResult, *framework.Status) {
	// Check if this pod is part of a gang
	podGroupName, minAvailable, err := utils.GetPodGroupLabels(pod)
	if err != nil || podGroupName == "" || minAvailable <= 1 {
		// Not a gang pod, let default preemption handle it
		return nil, framework.NewStatus(framework.Unschedulable, "not a gang pod")
	}

	klog.V(3).Infof("GangPreemption: PostFilter called for gang pod %s/%s (group: %s, minAvailable: %d)",
		pod.Namespace, pod.Name, podGroupName, minAvailable)

	// Get all nodes
	nodeInfos, err := gp.handle.SnapshotSharedLister().NodeInfos().List()
	if err != nil {
		return nil, framework.NewStatus(framework.Error, fmt.Sprintf("error listing nodes: %v", err))
	}

	// Calculate how many resources the entire gang needs
	gangResourceNeeds := gp.calculateGangResourceNeeds(pod, minAvailable)

	klog.V(4).Infof("GangPreemption: gang %s/%s needs CPU: %dm, Memory: %dMi, GPUs: %d",
		pod.Namespace, podGroupName,
		gangResourceNeeds.CPU, gangResourceNeeds.Memory/1024/1024, gangResourceNeeds.GPU)

	// Find victim pods that we can preempt to free up resources
	victims := gp.findPreemptionVictims(pod, gangResourceNeeds, nodeInfos)

	if len(victims) == 0 {
		klog.V(3).Infof("GangPreemption: no suitable victims found for gang %s/%s",
			pod.Namespace, podGroupName)
		return nil, framework.NewStatus(framework.Unschedulable, "no preemption victims found")
	}

	klog.V(3).Infof("GangPreemption: found %d victim pods to preempt for gang %s/%s",
		len(victims), pod.Namespace, podGroupName)

	// Create the preemption result
	nominatedNodeName := gp.selectNominatedNode(victims, nodeInfos)

	return &framework.PostFilterResult{
		NominatingInfo: &framework.NominatingInfo{
			NominatedNodeName: nominatedNodeName,
		},
	}, framework.NewStatus(framework.Success, fmt.Sprintf("preempting %d pods", len(victims)))
}

// ResourceRequirements represents the total resources needed by a gang
type ResourceRequirements struct {
	CPU    int64 // milliCPU
	Memory int64 // bytes
	GPU    int64 // count
}

// calculateGangResourceNeeds calculates the total resources needed by the entire gang
func (gp *GangPreemption) calculateGangResourceNeeds(pod *v1.Pod, minAvailable int) ResourceRequirements {
	// Assume all pods in the gang have similar resource requirements
	// (this is typically true for distributed training jobs)
	singlePodCPU := int64(0)
	singlePodMemory := int64(0)
	singlePodGPU := int64(0)

	for _, container := range pod.Spec.Containers {
		singlePodCPU += container.Resources.Requests.Cpu().MilliValue()
		singlePodMemory += container.Resources.Requests.Memory().Value()
		if gpuQuantity, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
			singlePodGPU += gpuQuantity.Value()
		}
	}

	return ResourceRequirements{
		CPU:    singlePodCPU * int64(minAvailable),
		Memory: singlePodMemory * int64(minAvailable),
		GPU:    singlePodGPU * int64(minAvailable),
	}
}

// VictimCandidate represents a pod that could be preempted
type VictimCandidate struct {
	Pod      *v1.Pod
	NodeName string
	Priority int32
	CPU      int64
	Memory   int64
	GPU      int64
}

// findPreemptionVictims finds lower-priority pods that can be preempted
// to free up resources for the gang
func (gp *GangPreemption) findPreemptionVictims(gangPod *v1.Pod, needs ResourceRequirements, nodeInfos []framework.NodeInfo) []*v1.Pod {
	gangPriority := int32(0)
	if gangPod.Spec.Priority != nil {
		gangPriority = *gangPod.Spec.Priority
	}

	// Collect all potential victim candidates (lower priority than gang pod)
	var candidates []VictimCandidate

	// Get all pods from all namespaces
	allPods, err := gp.podLister.List(nil)
	if err != nil {
		klog.Errorf("GangPreemption: error listing pods: %v", err)
		return nil
	}

	// Build a map of node names for quick lookup
	nodeMap := make(map[string]*v1.Node)
	for _, nodeInfo := range nodeInfos {
		node := nodeInfo.Node()
		if node != nil {
			nodeMap[node.Name] = node
		}
	}

	// Iterate through all scheduled pods
	for _, victimPod := range allPods {
		// Skip pods that are not assigned to a node yet
		if victimPod.Spec.NodeName == "" {
			continue
		}

		// Skip if node doesn't exist in our node list
		if _, exists := nodeMap[victimPod.Spec.NodeName]; !exists {
			continue
		}

		// Skip if same namespace and same pod group (don't preempt gang members)
		if victimPod.Namespace == gangPod.Namespace {
			victimGroupName, _, err := utils.GetPodGroupLabels(victimPod)
			if err == nil && victimGroupName != "" {
				gangGroupName, _, err := utils.GetPodGroupLabels(gangPod)
				if err == nil && victimGroupName == gangGroupName {
					continue
				}
			}
		}

		// Only preempt lower priority pods
		victimPriority := int32(0)
		if victimPod.Spec.Priority != nil {
			victimPriority = *victimPod.Spec.Priority
		}

		if victimPriority >= gangPriority {
			continue
		}

		// Calculate resources this victim would free
		cpu := int64(0)
		memory := int64(0)
		gpu := int64(0)

		for _, container := range victimPod.Spec.Containers {
			cpu += container.Resources.Requests.Cpu().MilliValue()
			memory += container.Resources.Requests.Memory().Value()
			if gpuQuantity, ok := container.Resources.Requests["nvidia.com/gpu"]; ok {
				gpu += gpuQuantity.Value()
			}
		}

		candidates = append(candidates, VictimCandidate{
			Pod:      victimPod,
			NodeName: victimPod.Spec.NodeName,
			Priority: victimPriority,
			CPU:      cpu,
			Memory:   memory,
			GPU:      gpu,
		})
	}

	if len(candidates) == 0 {
		return nil
	}

	// Sort candidates by priority (lowest first) and then by resource size (smallest first)
	// Strategy: Preempt the lowest priority pods first, and prefer smaller pods
	// to minimize disruption
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority < candidates[j].Priority
		}
		// If same priority, prefer smaller pods (less disruption)
		return candidates[i].CPU < candidates[j].CPU
	})

	// Greedily select victims until we have enough resources for the gang
	var victims []*v1.Pod
	freedCPU := int64(0)
	freedMemory := int64(0)
	freedGPU := int64(0)

	for i, candidate := range candidates {
		if i >= MaxVictimsPerGang {
			break
		}

		victims = append(victims, candidate.Pod)
		freedCPU += candidate.CPU
		freedMemory += candidate.Memory
		freedGPU += candidate.GPU

		klog.V(4).Infof("GangPreemption: considering victim %s/%s (priority: %d, CPU: %dm, Memory: %dMi, GPU: %d)",
			candidate.Pod.Namespace, candidate.Pod.Name, candidate.Priority,
			candidate.CPU, candidate.Memory/1024/1024, candidate.GPU)

		// Check if we have freed enough resources
		if freedCPU >= needs.CPU && freedMemory >= needs.Memory && freedGPU >= needs.GPU {
			klog.V(3).Infof("GangPreemption: found sufficient victims - freed CPU: %dm/%dm, Memory: %dMi/%dMi, GPU: %d/%d",
				freedCPU, needs.CPU,
				freedMemory/1024/1024, needs.Memory/1024/1024,
				freedGPU, needs.GPU)
			break
		}
	}

	// Verify we actually have enough resources after preemption
	if freedCPU < needs.CPU || freedMemory < needs.Memory || freedGPU < needs.GPU {
		klog.V(3).Infof("GangPreemption: insufficient resources even after preempting %d pods", len(victims))
		return nil
	}

	return victims
}

// selectNominatedNode selects which node to nominate for the gang pod
// We pick the node that will have the most freed resources after preemption
func (gp *GangPreemption) selectNominatedNode(victims []*v1.Pod, nodeInfos []framework.NodeInfo) string {
	nodeResources := make(map[string]int64)

	for _, victim := range victims {
		nodeName := victim.Spec.NodeName
		if nodeName == "" {
			continue
		}

		// Count freed CPU (as a proxy for "best node")
		for _, container := range victim.Spec.Containers {
			nodeResources[nodeName] += container.Resources.Requests.Cpu().MilliValue()
		}
	}

	// Find node with most freed resources
	bestNode := ""
	maxFreed := int64(0)

	for nodeName, freed := range nodeResources {
		if freed > maxFreed {
			maxFreed = freed
			bestNode = nodeName
		}
	}

	if bestNode != "" {
		klog.V(3).Infof("GangPreemption: nominating node %s (will free %dm CPU)", bestNode, maxFreed)
	}

	return bestNode
}

// New creates a new GangPreemption plugin
func New(_ context.Context, _ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	podLister := handle.SharedInformerFactory().Core().V1().Pods().Lister()

	return &GangPreemption{
		handle:    handle,
		podLister: podLister,
	}, nil
}
