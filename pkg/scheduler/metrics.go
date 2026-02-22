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

// Package scheduler provides core scheduling types and metrics for KubeNexus.
package scheduler

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// SchedulingAttempts tracks the number of scheduling attempts
	SchedulingAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubenexus_scheduling_attempts_total",
			Help: "Total number of scheduling attempts",
		},
		[]string{"result", "plugin"},
	)

	// SchedulingDuration tracks the duration of scheduling operations
	SchedulingDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kubenexus_scheduling_duration_seconds",
			Help:    "Duration of scheduling operations in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation", "plugin"},
	)

	// PodGroupSize tracks the size of pod groups
	PodGroupSize = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kubenexus_pod_group_size",
			Help:    "Size of pod groups being scheduled",
			Buckets: []float64{1, 2, 5, 10, 20, 50, 100, 200, 500},
		},
		[]string{"namespace"},
	)

	// WaitingPodsCount tracks the number of pods waiting in permit stage
	WaitingPodsCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubenexus_waiting_pods",
			Help: "Number of pods waiting in permit stage",
		},
		[]string{"namespace", "pod_group"},
	)

	// ResourceReservations tracks the number of active resource reservations
	ResourceReservations = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubenexus_resource_reservations",
			Help: "Number of active resource reservations",
		},
		[]string{"namespace"},
	)
)
