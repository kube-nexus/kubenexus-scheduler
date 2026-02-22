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

// Package util provides testing utilities for KubeNexus scheduler plugins,
// including fake listers, test frameworks, and helper functions for creating test fixtures.
package util

import (
	"context"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	clientsetfake "k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/events"
	klog "k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/defaultbinder"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/queuesort"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	tf "k8s.io/kubernetes/pkg/scheduler/testing/framework"
)

// NewFakeSharedLister creates a SharedLister from pods and nodes for testing
func NewFakeSharedLister(pods []*v1.Pod, nodes []*v1.Node) fwk.SharedLister {
	nodeInfoMap := createNodeInfoMap(pods, nodes)
	nodeInfos := make([]fwk.NodeInfo, 0, len(nodeInfoMap))
	havePodsWithAffinityNodeInfoList := make([]fwk.NodeInfo, 0, len(nodeInfoMap))
	havePodsWithRequiredAntiAffinityNodeInfoList := make([]fwk.NodeInfo, 0, len(nodeInfoMap))
	for _, v := range nodeInfoMap {
		nodeInfos = append(nodeInfos, v)
		if len(v.GetPodsWithAffinity()) > 0 {
			havePodsWithAffinityNodeInfoList = append(havePodsWithAffinityNodeInfoList, v)
		}
		if len(v.GetPodsWithRequiredAntiAffinity()) > 0 {
			havePodsWithRequiredAntiAffinityNodeInfoList = append(havePodsWithRequiredAntiAffinityNodeInfoList, v)
		}
	}
	return &fakeSharedLister{
		nodeInfos:                        nodeInfos,
		nodeInfoMap:                      nodeInfoMap,
		havePodsWithAffinityNodeInfoList: havePodsWithAffinityNodeInfoList,
		havePodsWithRequiredAntiAffinityNodeInfoList: havePodsWithRequiredAntiAffinityNodeInfoList,
	}
}

func createNodeInfoMap(pods []*v1.Pod, nodes []*v1.Node) map[string]fwk.NodeInfo {
	nodeNameToInfo := make(map[string]fwk.NodeInfo)
	for _, pod := range pods {
		nodeName := pod.Spec.NodeName
		if _, ok := nodeNameToInfo[nodeName]; !ok {
			nodeNameToInfo[nodeName] = framework.NewNodeInfo()
		}
		nodeNameToInfo[nodeName].(*framework.NodeInfo).AddPod(pod) //nolint:errcheck // AddPod doesn't return error
	}
	
	for _, node := range nodes {
		if _, ok := nodeNameToInfo[node.Name]; !ok {
			nodeNameToInfo[node.Name] = framework.NewNodeInfo()
		}
		nodeInfo := nodeNameToInfo[node.Name]
		nodeInfo.SetNode(node)
	}
	return nodeNameToInfo
}

// fakeSharedLister implements framework.SharedLister for testing
var _ fwk.SharedLister = &fakeSharedLister{}

type fakeSharedLister struct {
	nodeInfos                                    []fwk.NodeInfo
	nodeInfoMap                                  map[string]fwk.NodeInfo
	havePodsWithAffinityNodeInfoList             []fwk.NodeInfo
	havePodsWithRequiredAntiAffinityNodeInfoList []fwk.NodeInfo
}

func (f *fakeSharedLister) NodeInfos() fwk.NodeInfoLister {
	return f
}

func (f *fakeSharedLister) StorageInfos() fwk.StorageInfoLister {
	return nil
}

func (f *fakeSharedLister) List() ([]fwk.NodeInfo, error) {
	return f.nodeInfos, nil
}

func (f *fakeSharedLister) HavePodsWithAffinityList() ([]fwk.NodeInfo, error) {
	return f.havePodsWithAffinityNodeInfoList, nil
}

func (f *fakeSharedLister) HavePodsWithRequiredAntiAffinityList() ([]fwk.NodeInfo, error) {
	return f.havePodsWithRequiredAntiAffinityNodeInfoList, nil
}

func (f *fakeSharedLister) Get(nodeName string) (fwk.NodeInfo, error) {
	return f.nodeInfoMap[nodeName], nil
}

// NewTestFrameworkWithPods creates a framework.Handle with pods added to the fake clientset
func NewTestFrameworkWithPods(pods []*v1.Pod, nodes []*v1.Node, registeredPlugins []tf.RegisterPluginFunc) (fwk.Handle, error) {
	// Add default required plugins
	plugins := append([]tf.RegisterPluginFunc{
		tf.RegisterBindPlugin(defaultbinder.Name, defaultbinder.New),
		tf.RegisterQueueSortPlugin(queuesort.Name, queuesort.New),
	}, registeredPlugins...)

	// Create fake clientset with initial objects
	objects := []runtime.Object{}
	for _, pod := range pods {
		objects = append(objects, pod)
	}
	for _, node := range nodes {
		objects = append(objects, node)
	}
	cs := clientsetfake.NewSimpleClientset(objects...)

	informerFactory := informers.NewSharedInformerFactory(cs, 0)
	stopCh := make(chan struct{})
	informerFactory.Start(stopCh)
	informerFactory.WaitForCacheSync(stopCh)

	// Give informer cache a moment to fully populate
	time.Sleep(200 * time.Millisecond)

	options := []frameworkruntime.Option{
		frameworkruntime.WithClientSet(cs),
		frameworkruntime.WithInformerFactory(informerFactory),
		frameworkruntime.WithEventRecorder(&events.FakeRecorder{}),
		frameworkruntime.WithSnapshotSharedLister(NewFakeSharedLister(pods, nodes)),
	}

	return tf.NewFramework(context.Background(), plugins, "test-scheduler", options...)
}

// NewTestFramework creates a framework.Handle for testing plugins
func NewTestFramework(registeredPlugins []tf.RegisterPluginFunc, profiles ...frameworkruntime.Option) (fwk.Handle, error) {
	// Add default required plugins
	plugins := append([]tf.RegisterPluginFunc{
		tf.RegisterBindPlugin(defaultbinder.Name, defaultbinder.New),
		tf.RegisterQueueSortPlugin(queuesort.Name, queuesort.New),
	}, registeredPlugins...)

	cs := clientsetfake.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(cs, 0)

	options := []frameworkruntime.Option{
		frameworkruntime.WithClientSet(cs),
		frameworkruntime.WithInformerFactory(informerFactory),
		frameworkruntime.WithEventRecorder(&events.FakeRecorder{}),
	}
	options = append(options, profiles...)

	return tf.NewFramework(context.Background(), plugins, "test-scheduler", options...)
}

// nominator is a structure that stores pods nominated to run on nodes.
// This is copied from upstream scheduler for testing purposes.
type nominator struct {
	podLister          corelisters.PodLister
	nominatedPods      map[string][]*framework.PodInfo
	nominatedPodToNode map[types.UID]string
	lock               sync.RWMutex
}

// NewPodNominator creates a nominator as a backing of framework.PodNominator.
// A podLister is passed in so as to check if the pod exists
// before adding its nominatedNode info.
func NewPodNominator(podLister corelisters.PodLister) fwk.PodNominator {
	return &nominator{
		podLister:          podLister,
		nominatedPods:      make(map[string][]*framework.PodInfo),
		nominatedPodToNode: make(map[types.UID]string),
	}
}

// AddNominatedPod adds a pod to the nominated pods list.
func (npm *nominator) AddNominatedPod(logger klog.Logger, pi fwk.PodInfo, nominatingInfo *fwk.NominatingInfo) {
	npm.lock.Lock()
	defer npm.lock.Unlock()

	// Always add the nominatedNodeName back to the pod.
	nnn := nominatingInfo.NominatedNodeName

	// Convert interface to concrete type
	concretePi, ok := pi.(*framework.PodInfo)
	if !ok {
		// Can't work with non-concrete PodInfo in test code
		return
	}

	pod := concretePi.Pod
	if pod.Status.NominatedNodeName != nnn {
		podCopy := pod.DeepCopy()
		podCopy.Status.NominatedNodeName = nnn
		var err error
		concretePi, err = framework.NewPodInfo(podCopy)
		if err != nil {
			return
		}
	}

	// delete any existing entry for this pod
	if oldNode, ok := npm.nominatedPodToNode[pod.UID]; ok {
		npm.deleteFromNominatedPods(pod, oldNode)
	}

	npm.nominatedPods[nnn] = append(npm.nominatedPods[nnn], concretePi)
	npm.nominatedPodToNode[pod.UID] = nnn
}

func (npm *nominator) deleteFromNominatedPods(pod *v1.Pod, nodeName string) {
	nps := npm.nominatedPods[nodeName]
	for i, np := range nps {
		if np.Pod.UID == pod.UID {
			npm.nominatedPods[nodeName] = append(nps[:i], nps[i+1:]...)
			if len(npm.nominatedPods[nodeName]) == 0 {
				delete(npm.nominatedPods, nodeName)
			}
			break
		}
	}
	delete(npm.nominatedPodToNode, pod.UID)
}

// DeleteNominatedPodIfExists deletes <pod> from nominatedPods.
func (npm *nominator) DeleteNominatedPodIfExists(pod *v1.Pod) {
	npm.lock.Lock()
	defer npm.lock.Unlock()
	npm.deleteNominatedPodIfExistsUnlocked(pod)
}

func (npm *nominator) deleteNominatedPodIfExistsUnlocked(pod *v1.Pod) {
	nn, ok := npm.nominatedPodToNode[pod.UID]
	if !ok {
		return
	}
	npm.deleteFromNominatedPods(pod, nn)
}

// UpdateNominatedPod updates a pod in the nominated pods list.
func (npm *nominator) UpdateNominatedPod(logger klog.Logger, oldPod *v1.Pod, newPodInfo fwk.PodInfo) {
	npm.lock.Lock()
	defer npm.lock.Unlock()

	// In some cases, the pod may not be nominated by any node yet, so
	// oldPod.Status.NominatedNodeName could be empty.
	if len(oldPod.Status.NominatedNodeName) == 0 {
		return
	}

	// Convert interface to concrete type - this is safe because we control the nominator
	// and always store concrete types
	concretePi, ok := newPodInfo.(*framework.PodInfo)
	if !ok {
		// Interface doesn't have Pod() method, but we need the concrete type
		// In practice this shouldn't happen in our test code
		return
	}

	// We won't fall into below case if the update is for nominated node name.
	if oldPod.Status.NominatedNodeName == concretePi.Pod.Status.NominatedNodeName {
		npm.deleteNominatedPodIfExistsUnlocked(oldPod)
		npm.nominatedPods[concretePi.Pod.Status.NominatedNodeName] = append(
			npm.nominatedPods[concretePi.Pod.Status.NominatedNodeName], concretePi)
		npm.nominatedPodToNode[concretePi.Pod.UID] = concretePi.Pod.Status.NominatedNodeName
	}
}

// NominatedPodsForNode returns a copy of pods that are nominated to run on the given node,
// but they are waiting for other pods to be removed from the node.
func (npm *nominator) NominatedPodsForNode(nodeName string) []fwk.PodInfo {
	npm.lock.RLock()
	defer npm.lock.RUnlock()
	// Make a copy of the nominated Pods so the caller can mutate safely.
	pods := make([]fwk.PodInfo, len(npm.nominatedPods[nodeName]))
	for i := 0; i < len(pods); i++ {
		pods[i] = npm.nominatedPods[nodeName][i]
	}
	return pods
}

// Ensure nominator implements PodNominator
var _ fwk.PodNominator = &nominator{}

// MakePod creates a test pod with given parameters
func MakePod(name, namespace, nodeName string, requests v1.ResourceList, labels, annotations map[string]string) *v1.Pod {
	pod := &v1.Pod{}
	pod.Name = name
	pod.Namespace = namespace
	pod.Spec.NodeName = nodeName
	pod.Labels = labels
	pod.Annotations = annotations
	if requests != nil {
		pod.Spec.Containers = []v1.Container{{
			Name: "container",
			Resources: v1.ResourceRequirements{
				Requests: requests,
			},
		}}
	}
	return pod
}

// MakeNode creates a test node with given name and labels
func MakeNode(name string, labels map[string]string, capacity v1.ResourceList) *v1.Node {
	node := &v1.Node{}
	node.Name = name
	node.Labels = labels
	if capacity != nil {
		node.Status.Capacity = capacity
		node.Status.Allocatable = capacity
	}
	return node
}

// WaitForCacheSync waits for informer caches to sync with a timeout
func WaitForCacheSync(factory informers.SharedInformerFactory) bool {
	factory.Start(nil)
	factory.WaitForCacheSync(make(chan struct{}))
	// Give it a moment to fully sync
	time.Sleep(100 * time.Millisecond)
	return true
}

// NewFakePodLister creates a fake pod lister for testing
func NewFakePodLister(pods []*v1.Pod) corelisters.PodLister {
	return &fakePodLister{pods: pods}
}

// fakePodLister is a fake implementation of PodLister for testing
type fakePodLister struct {
	pods []*v1.Pod
}

func (f *fakePodLister) List(selector labels.Selector) ([]*v1.Pod, error) {
	var filtered []*v1.Pod
	for _, pod := range f.pods {
		// If selector is nil, return all pods
		if selector == nil || selector.Matches(labels.Set(pod.Labels)) {
			filtered = append(filtered, pod)
		}
	}
	return filtered, nil
}

func (f *fakePodLister) Pods(namespace string) corelisters.PodNamespaceLister {
	return &fakePodNamespaceLister{parent: f, namespace: namespace}
}

type fakePodNamespaceLister struct {
	parent    *fakePodLister
	namespace string
}

func (f *fakePodNamespaceLister) List(selector labels.Selector) ([]*v1.Pod, error) {
	var filtered []*v1.Pod
	for _, pod := range f.parent.pods {
		if pod.Namespace == f.namespace && selector.Matches(labels.Set(pod.Labels)) {
			filtered = append(filtered, pod)
		}
	}
	return filtered, nil
}

func (f *fakePodNamespaceLister) Get(name string) (*v1.Pod, error) {
	for _, pod := range f.parent.pods {
		if pod.Namespace == f.namespace && pod.Name == name {
			return pod, nil
		}
	}
	return nil, nil
}

// NewFakeNodeLister creates a fake node lister for testing
func NewFakeNodeLister(nodes []*v1.Node) corelisters.NodeLister {
	return &fakeNodeLister{nodes: nodes}
}

// fakeNodeLister is a fake implementation of NodeLister for testing
type fakeNodeLister struct {
	nodes []*v1.Node
}

func (f *fakeNodeLister) List(selector labels.Selector) ([]*v1.Node, error) {
	var filtered []*v1.Node
	for _, node := range f.nodes {
		if selector.Matches(labels.Set(node.Labels)) {
			filtered = append(filtered, node)
		}
	}
	return filtered, nil
}

func (f *fakeNodeLister) Get(name string) (*v1.Node, error) {
	for _, node := range f.nodes {
		if node.Name == name {
			return node, nil
		}
	}
	return nil, nil
}
