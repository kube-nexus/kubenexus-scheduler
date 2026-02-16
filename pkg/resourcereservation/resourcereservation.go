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

package resourcereservation

import (
	"context"
	"os"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	framework "k8s.io/kube-scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/apis/scheduling/v1alpha1"
)

const (
	// Name is the name of the plugin
	Name = "ResourceReservation"
)

var podGroupVersionKind = v1.SchemeGroupVersion.WithKind("Pod")

// ResourceReservation is a plugin that creates resource reservations to prevent starvation
type ResourceReservation struct {
	frameworkHandle framework.Handle
	podLister       corelisters.PodLister
	client          rest.Interface
}

var _ framework.ReservePlugin = &ResourceReservation{}

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
	if _, err := os.Stat("/opt/config-file"); err == nil {
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
	if err := setConfigDefaults(&config); err != nil {
		return nil, err
	}

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}

	return &ResourceReservation{
		frameworkHandle: handle,
		podLister:       podLister,
		client:          client,
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

// Reserve creates resource reservations for the pod
func (rr *ResourceReservation) Reserve(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeName string) *framework.Status {
	if pod == nil {
		return framework.NewStatus(framework.Error, "pod cannot be nil")
	}

	// Create resource reservation
	reservation := newResourceReservation(nodeName, pod)
	_, err := rr.create(ctx, reservation, pod)
	if err != nil {
		klog.Errorf("Reserve: failed to create reservation for pod %s/%s: %v", pod.Namespace, pod.Name, err)
		return framework.NewStatus(framework.Error, err.Error())
	}

	klog.V(4).Infof("Reserve: created reservation for pod %s/%s on node %s", pod.Namespace, pod.Name, nodeName)
	return framework.NewStatus(framework.Success, "")
}

// Unreserve deletes resource reservations if scheduling fails
func (rr *ResourceReservation) Unreserve(ctx context.Context, state framework.CycleState, pod *v1.Pod, nodeName string) {
	if pod == nil {
		return
	}

	// Delete the reservation
	err := rr.delete(ctx, pod)
	if err != nil {
		klog.Errorf("Unreserve: failed to delete reservation for pod %s/%s: %v", pod.Namespace, pod.Name, err)
		return
	}

	klog.V(4).Infof("Unreserve: deleted reservation for pod %s/%s", pod.Namespace, pod.Name)
}

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
		Namespace(pod.Namespace).
		Resource("resourcereservations").
		Body(resourceReservation).
		Do(ctx).
		Into(result)
	return result, err
}

func (rr *ResourceReservation) delete(ctx context.Context, pod *v1.Pod) error {
	appID := pod.Name
	if id, ok := pod.Labels[v1alpha1.AppIDLabel]; ok {
		appID = id
	}

	return rr.client.Delete().
		Namespace(pod.Namespace).
		Resource("resourcereservations").
		Name(appID).
		Do(ctx).
		Error()
}
