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

// Package utils provides common utilities for the scheduler.
package utils

import (
	"context"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	corelisters "k8s.io/client-go/listers/core/v1"
	klog "k8s.io/klog/v2"
)

// PodGroupManager manages pod group state
type PodGroupManager struct {
	podLister corelisters.PodLister
}

// NewPodGroupManager creates a new PodGroupManager
func NewPodGroupManager(podLister corelisters.PodLister) *PodGroupManager {
	return &PodGroupManager{
		podLister: podLister,
	}
}

// GetPodGroupSize returns the total number of pods in a pod group
func (m *PodGroupManager) GetPodGroupSize(namespace, podGroupName string) (int, error) {
	selector := labels.Set{
		"pod-group.scheduling.kubenexus.io/name": podGroupName,
	}.AsSelector()

	pods, err := m.podLister.Pods(namespace).List(selector)
	if err != nil {
		// Try old label for compatibility
		selector = labels.Set{
			"pod-group.scheduling.sigs.k8s.io/name": podGroupName,
		}.AsSelector()
		pods, err = m.podLister.Pods(namespace).List(selector)
		if err != nil {
			return 0, err
		}
	}

	return len(pods), nil
}

// GetRunningPods returns the number of running pods in a pod group
func (m *PodGroupManager) GetRunningPods(namespace, podGroupName string) (int, error) {
	selector := labels.Set{
		"pod-group.scheduling.kubenexus.io/name": podGroupName,
	}.AsSelector()

	pods, err := m.podLister.Pods(namespace).List(selector)
	if err != nil {
		// Try old label for compatibility
		selector = labels.Set{
			"pod-group.scheduling.sigs.k8s.io/name": podGroupName,
		}.AsSelector()
		pods, err = m.podLister.Pods(namespace).List(selector)
		if err != nil {
			return 0, err
		}
	}

	running := 0
	for _, pod := range pods {
		if pod.Status.Phase == v1.PodRunning {
			running++
		}
	}

	return running, nil
}

// RetryWithBackoff retries a function with exponential backoff
func RetryWithBackoff(ctx context.Context, attempts int, initialDelay time.Duration, fn func() error) error {
	delay := initialDelay
	var err error

	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}

		klog.V(4).Infof("Attempt %d/%d failed: %v, retrying in %v", i+1, attempts, err, delay)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			delay *= 2
		}
	}

	return err
}
