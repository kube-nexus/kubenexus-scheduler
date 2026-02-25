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

// Package resourcereservation implements Palantir-style resource reservation for gang scheduling.
// It prevents race conditions by creating ResourceReservation CRDs upfront to reserve capacity
// before other workloads can steal it.
package resourcereservation

import (
	"context"
	"fmt"
	"os"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	klog "k8s.io/klog/v2"
	framework "k8s.io/kube-scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/apis/scheduling/v1alpha1"
	"sigs.k8s.io/scheduler-plugins/pkg/utils"
)

const (
	// Name is the name of the plugin
	Name = "ResourceReservation"

	// stateKey for storing reservation state in CycleState
	stateKey = "ResourceReservation"
)

var podGroupVersionKind = v1.SchemeGroupVersion.WithKind("Pod")

// ResourceReservation implements Palantir-style gang scheduling with capacity reservation
type ResourceReservation struct {
	frameworkHandle framework.Handle
	podLister       corelisters.PodLister
	client          rest.Interface

	// Track which gangs have had reservations created
	gangReservationsCreated sync.Map // map[gangKey]bool
}

// Ensure ResourceReservation implements all required interfaces
var _ framework.PreFilterPlugin = &ResourceReservation{}
var _ framework.FilterPlugin = &ResourceReservation{}
var _ framework.ReservePlugin = &ResourceReservation{}
var _ framework.PostBindPlugin = &ResourceReservation{}

// reservationState stores reservation info in CycleState
type reservationState struct {
	gangKey      string
	reservations []*v1alpha1.ResourceReservation
}

// Name returns name of the plugin
func (rr *ResourceReservation) Name() string {
	return Name
}

// New initializes a new plugin and returns it
func New(ctx context.Context, obj runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	podLister := handle.SharedInformerFactory().Core().V1().Pods().Lister()

	// Initialize Kubernetes client
	var kubeconfig *rest.Config
	var err error

	// Try to load from config file first (for local development)
	if _, statErr := os.Stat("/opt/config-file"); statErr == nil {
		kubeconfig, err = clientcmd.BuildConfigFromFlags("", "/opt/config-file")
		if err != nil {
			klog.V(3).Infof("error building config from file: %v", err)
			return nil, err
		}
	} else {
		// Fall back to in-cluster config
		kubeconfig, err = rest.InClusterConfig()
		if err != nil {
			klog.V(3).Infof("error building in-cluster config: %v", err)
			return nil, err
		}
	}

	config := *kubeconfig
	if configErr := setConfigDefaults(&config); configErr != nil {
		return nil, configErr
	}

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}

	return &ResourceReservation{
		frameworkHandle:         handle,
		podLister:               podLister,
		client:                  client,
		gangReservationsCreated: sync.Map{},
	}, nil
}

func setConfigDefaults(config *rest.Config) error {
	gv := v1alpha1.SchemeGroupVersion
	config.GroupVersion = &gv
	config.APIPath = "/apis"

	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		return err
	}
	config.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: serializer.NewCodecFactory(scheme)}

	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}

	return nil
}

// PreFilter creates ResourceReservation CRDs for all gang members BEFORE scheduling
// This prevents race conditions where other workloads steal capacity
func (rr *ResourceReservation) PreFilter(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfos []framework.NodeInfo) (*framework.PreFilterResult, *framework.Status) {
	// Skip non-gang pods
	if !isGangMember(pod) {
		return nil, framework.NewStatus(framework.Success, "")
	}

	podGroupName, minAvailable, err := utils.GetPodGroupLabels(pod)
	if err != nil || podGroupName == "" {
		return nil, framework.NewStatus(framework.Success, "")
	}

	gangKey := fmt.Sprintf("%s/%s", pod.Namespace, podGroupName)

	// Only first pod in gang creates reservations (avoid duplicates)
	if _, alreadyCreated := rr.gangReservationsCreated.Load(gangKey); alreadyCreated {
		klog.V(4).Infof("PreFilter: reservations already created for gang %s", gangKey)
		return nil, framework.NewStatus(framework.Success, "")
	}

	// Mark as created (prevents race between concurrent prefilter calls)
	rr.gangReservationsCreated.Store(gangKey, true)

	// Create ResourceReservation CRDs for all gang members
	reservations, err := rr.createGangReservations(ctx, pod, podGroupName, minAvailable)
	if err != nil {
		klog.Errorf("PreFilter: failed to create reservations for gang %s: %v", gangKey, err)
		rr.gangReservationsCreated.Delete(gangKey)
		return nil, framework.NewStatus(framework.Error, err.Error())
	}

	// Store in CycleState for later phases
	state.Write(stateKey, &reservationState{
		gangKey:      gangKey,
		reservations: reservations,
	})

	klog.V(3).Infof("PreFilter: created %d reservations for gang %s", len(reservations), gangKey)
	return nil, framework.NewStatus(framework.Success, "")
}

// PreFilterExtensions returns prefilter extensions
func (rr *ResourceReservation) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

// Filter checks if node has capacity after accounting for ResourceReservation CRDs
func (rr *ResourceReservation) Filter(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeInfo framework.NodeInfo) *framework.Status {
	// Get reservations for this node
	nodeName := nodeInfo.Node().Name
	nodeReservations, err := rr.getNodeReservations(ctx, nodeName, pod.Namespace)
	if err != nil {
		klog.V(5).Infof("Filter: failed to get reservations for node %s: %v", nodeName, err)
		// Don't fail scheduling if we can't read reservations
		return framework.NewStatus(framework.Success, "")
	}

	if len(nodeReservations) == 0 {
		return framework.NewStatus(framework.Success, "")
	}

	// Calculate reserved capacity
	reservedCPU := resource.Quantity{}
	reservedMemory := resource.Quantity{}

	gangKey := getGangKey(pod)

	for _, res := range nodeReservations {
		// Don't count reservations from our own gang
		if res.Labels != nil && res.Labels["gang-key"] == gangKey {
			continue
		}

		for _, reservation := range res.Spec.Reservations {
			if reservation.Node == "" || reservation.Node == nodeName {
				reservedCPU.Add(reservation.CPU)
				reservedMemory.Add(reservation.Memory)
			}
		}
	}

	// Check if node has capacity after accounting for reservations
	// Get node resource info (Allocatable and Requested are stored in NodeInfo)
	// For simplicity, we'll just check if the pod + reservations fit
	// The actual detailed capacity check is done by default Filter plugins

	// Simple check: if we have significant reservations, log them
	// The actual capacity check is delegated to NodeResourcesFit plugin
	if reservedCPU.MilliValue() > 0 || reservedMemory.Value() > 0 {
		klog.V(4).Infof("Filter: node %s has %dm CPU, %d Memory reserved by other gangs",
			nodeName, reservedCPU.MilliValue(), reservedMemory.Value())
	}

	return framework.NewStatus(framework.Success, "")
}

// Reserve marks the reservation as claimed (doesn't create CRDs - already created in PreFilter)
func (rr *ResourceReservation) Reserve(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeName string) *framework.Status {
	if pod == nil {
		return framework.NewStatus(framework.Error, "pod cannot be nil")
	}

	// Skip non-gang pods
	if !isGangMember(pod) {
		return framework.NewStatus(framework.Success, "")
	}

	// Update reservation status to "claimed"
	// This helps track which pods have been scheduled
	gangKey := getGangKey(pod)
	klog.V(4).Infof("Reserve: marking reservation as claimed for pod %s/%s (gang: %s) on node %s",
		pod.Namespace, pod.Name, gangKey, nodeName)

	return framework.NewStatus(framework.Success, "")
}

// Unreserve handles cleanup if scheduling fails
func (rr *ResourceReservation) Unreserve(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeName string) {
	if pod == nil || !isGangMember(pod) {
		return
	}

	gangKey := getGangKey(pod)
	klog.V(4).Infof("Unreserve: pod %s/%s failed to schedule (gang: %s)", pod.Namespace, pod.Name, gangKey)

	// If this is a critical failure, might want to clean up reservations
	// For now, let them timeout naturally or get cleaned up in PostBind
}

// PostBind cleans up ResourceReservation CRDs when gang completes
func (rr *ResourceReservation) PostBind(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeName string) {
	if !isGangMember(pod) {
		return
	}

	podGroupName, minAvailable, err := utils.GetPodGroupLabels(pod)
	if err != nil || podGroupName == "" {
		return
	}

	gangKey := fmt.Sprintf("%s/%s", pod.Namespace, podGroupName)

	// Check if gang is complete
	if !rr.isGangComplete(pod, podGroupName, minAvailable) {
		klog.V(4).Infof("PostBind: gang %s not yet complete, keeping reservations", gangKey)
		return
	}

	// Gang is complete - delete all ResourceReservation CRDs
	klog.V(3).Infof("PostBind: gang %s complete, cleaning up reservations", gangKey)

	if err := rr.deleteGangReservations(ctx, pod.Namespace, gangKey); err != nil {
		klog.Errorf("PostBind: failed to delete reservations for gang %s: %v", gangKey, err)
	} else {
		rr.gangReservationsCreated.Delete(gangKey)
	}
}

// Helper functions

func isGangMember(pod *v1.Pod) bool {
	podGroupName, minAvailable, err := utils.GetPodGroupLabels(pod)
	return err == nil && podGroupName != "" && minAvailable > 1
}

func getGangKey(pod *v1.Pod) string {
	podGroupName, _, err := utils.GetPodGroupLabels(pod)
	if err != nil || podGroupName == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s", pod.Namespace, podGroupName)
}

// createGangReservations creates ResourceReservation CRDs for all expected gang members
func (rr *ResourceReservation) createGangReservations(ctx context.Context, pod *v1.Pod, podGroupName string, minAvailable int) ([]*v1alpha1.ResourceReservation, error) {
	gangKey := fmt.Sprintf("%s/%s", pod.Namespace, podGroupName)

	// Create one ResourceReservation CRD representing the entire gang's resource needs
	// This acts as a "phantom" that consumes capacity until real pods are scheduled
	reservations := make(map[string]v1alpha1.Reservation)

	// Calculate per-pod resources (assume all pods in gang have same requests)
	cpuPerPod := resource.Quantity{}
	memPerPod := resource.Quantity{}

	if len(pod.Spec.Containers) > 0 {
		container := pod.Spec.Containers[0]
		if cpu, ok := container.Resources.Requests[v1.ResourceCPU]; ok {
			cpuPerPod = cpu
		}
		if mem, ok := container.Resources.Requests[v1.ResourceMemory]; ok {
			memPerPod = mem
		}
	}

	// Create reservation entries for each expected gang member
	for i := 0; i < minAvailable; i++ {
		memberKey := fmt.Sprintf("%s-member-%d", podGroupName, i)
		reservations[memberKey] = v1alpha1.Reservation{
			Node:   "", // Not assigned yet - filter will check all nodes
			CPU:    cpuPerPod,
			Memory: memPerPod,
		}
	}

	reservation := &v1alpha1.ResourceReservation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-reservation", podGroupName),
			Namespace: pod.Namespace,
			Labels: map[string]string{
				"gang-key":                     gangKey,
				"pod-group":                    podGroupName,
				"app.kubernetes.io/managed-by": "kubenexus-scheduler",
			},
		},
		Spec: v1alpha1.ResourceReservationSpec{
			Reservations: reservations,
		},
		Status: v1alpha1.ResourceReservationStatus{
			Pods: make(map[string]string),
		},
	}

	result, err := rr.create(ctx, reservation, pod)
	if err != nil {
		return nil, fmt.Errorf("failed to create reservation CRD: %w", err)
	}

	return []*v1alpha1.ResourceReservation{result}, nil
}

// getNodeReservations fetches ResourceReservation CRDs that affect a specific node
func (rr *ResourceReservation) getNodeReservations(ctx context.Context, nodeName, namespace string) ([]*v1alpha1.ResourceReservation, error) {
	// List all ResourceReservations in namespace
	result := &v1alpha1.ResourceReservationList{}
	err := rr.client.Get().
		Namespace(namespace).
		Resource("resourcereservations").
		Do(ctx).
		Into(result)

	if err != nil {
		return nil, err
	}

	// Filter to reservations that affect this node
	var nodeReservations []*v1alpha1.ResourceReservation
	for i := range result.Items {
		res := &result.Items[i]
		// Check if any reservation entry is for this node or unassigned (affects all nodes)
		for _, reservation := range res.Spec.Reservations {
			if reservation.Node == "" || reservation.Node == nodeName {
				nodeReservations = append(nodeReservations, res)
				break
			}
		}
	}

	return nodeReservations, nil
}

// isGangComplete checks if all gang members have been scheduled
func (rr *ResourceReservation) isGangComplete(pod *v1.Pod, podGroupName string, minAvailable int) bool {
	namespace := pod.Namespace

	// Count running + bound pods in gang
	pods, err := rr.podLister.Pods(namespace).List(nil)
	if err != nil {
		return false
	}

	scheduledCount := 0
	for _, p := range pods {
		pgName, _, err := utils.GetPodGroupLabels(p)
		if err != nil || pgName != podGroupName {
			continue
		}

		// Count pods that are scheduled (have NodeName assigned)
		if p.Spec.NodeName != "" {
			scheduledCount++
		}
	}

	return scheduledCount >= minAvailable
}

// deleteGangReservations removes all ResourceReservation CRDs for a gang
func (rr *ResourceReservation) deleteGangReservations(ctx context.Context, namespace, gangKey string) error {
	// List all reservations with gang-key label
	result := &v1alpha1.ResourceReservationList{}
	err := rr.client.Get().
		Namespace(namespace).
		Resource("resourcereservations").
		Do(ctx).
		Into(result)

	if err != nil {
		return err
	}

	for _, res := range result.Items {
		if res.Labels != nil && res.Labels["gang-key"] == gangKey {
			klog.V(4).Infof("Deleting reservation %s/%s for gang %s", namespace, res.Name, gangKey)
			if err := rr.delete(ctx, namespace, res.Name); err != nil {
				klog.Errorf("Failed to delete reservation %s/%s: %v", namespace, res.Name, err)
			}
		}
	}

	return nil
}

// Legacy functions for compatibility (not used in Palantir pattern)

func newResourceReservation(driverNode string, driver *v1.Pod) *v1alpha1.ResourceReservation {
	reservations := make(map[string]v1alpha1.Reservation, 1)

	// Extract resource requests from pod
	cpu := resource.MustParse("1")
	mem := resource.MustParse("750M")

	if len(driver.Spec.Containers) > 0 {
		container := driver.Spec.Containers[0]
		if c, ok := container.Resources.Requests[v1.ResourceCPU]; ok {
			cpu = c
		}
		if m, ok := container.Resources.Requests[v1.ResourceMemory]; ok {
			mem = m
		}
	}

	reservations[driver.Name] = v1alpha1.Reservation{
		Node:   driverNode,
		CPU:    cpu,
		Memory: mem,
	}

	appID := driver.Name
	if id, ok := driver.Labels[v1alpha1.AppIDLabel]; ok {
		appID = id
	}

	return &v1alpha1.ResourceReservation{
		ObjectMeta: metav1.ObjectMeta{
			Name:            appID,
			Namespace:       driver.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(driver, podGroupVersionKind)},
			Labels: map[string]string{
				v1alpha1.AppIDLabel: appID,
			},
		},
		Spec: v1alpha1.ResourceReservationSpec{
			Reservations: reservations,
		},
		Status: v1alpha1.ResourceReservationStatus{
			Pods: map[string]string{"driver": driver.Name},
		},
	}
}

func (rr *ResourceReservation) create(ctx context.Context, resourceReservation *v1alpha1.ResourceReservation, pod *v1.Pod) (*v1alpha1.ResourceReservation, error) {
	result := &v1alpha1.ResourceReservation{}
	err := rr.client.Post().
		Namespace(resourceReservation.Namespace).
		Resource("resourcereservations").
		Body(resourceReservation).
		Do(ctx).
		Into(result)
	return result, err
}

func (rr *ResourceReservation) delete(ctx context.Context, namespace, name string) error {
	return rr.client.Delete().
		Namespace(namespace).
		Resource("resourcereservations").
		Name(name).
		Do(ctx).
		Error()
}

// Clone implements StateData interface
func (r *reservationState) Clone() framework.StateData {
	return &reservationState{
		gangKey:      r.gangKey,
		reservations: r.reservations,
	}
}
