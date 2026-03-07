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

	// VRAM Scheduling Metrics

	// VRAMPlacementDecisions tracks VRAM placement decisions
	VRAMPlacementDecisions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubenexus_vram_placement_decisions_total",
			Help: "Total number of VRAM placement decisions by outcome",
		},
		[]string{"outcome", "workload_type", "data_source"},
	)

	// VRAMNodeUtilization tracks VRAM utilization percentage per node
	VRAMNodeUtilization = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubenexus_vram_node_utilization_percent",
			Help: "VRAM utilization percentage per node",
		},
		[]string{"node", "gpu_model"},
	)

	// VRAMRequestedBytes tracks VRAM requested by pods
	VRAMRequestedBytes = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kubenexus_vram_requested_bytes",
			Help:    "VRAM requested by pods in bytes",
			Buckets: []float64{8e9, 16e9, 24e9, 40e9, 48e9, 80e9, 160e9, 320e9}, // 8GB to 320GB
		},
		[]string{"namespace", "workload_type"},
	)

	// TopologyDecisions tracks topology-aware placement decisions
	TopologyDecisions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubenexus_topology_decisions_total",
			Help: "Total GPU topology placement decisions",
		},
		[]string{"topology_type", "success", "constraint_type"},
	)

	// TopologyQualityScore tracks the quality of topology placements
	TopologyQualityScore = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kubenexus_topology_quality_score",
			Help:    "Quality score of GPU topology placements (0-100)",
			Buckets: []float64{0, 25, 50, 75, 90, 95, 100},
		},
		[]string{"topology_type"},
	)

	// FragmentationEvents tracks fragmentation prevention events
	FragmentationEvents = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubenexus_fragmentation_events_total",
			Help: "Fragmentation prevention events",
		},
		[]string{"event_type", "prevented"},
	)

	// DataSourceUsage tracks which data source was used for GPU discovery
	DataSourceUsage = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubenexus_gpu_data_source_usage_total",
			Help: "GPU topology data source usage (DRA, NFD, manual labels)",
		},
		[]string{"source"},
	)

	// SchedulingLatency tracks plugin execution time
	SchedulingLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kubenexus_plugin_latency_seconds",
			Help:    "Plugin execution latency in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
		},
		[]string{"plugin", "operation"},
	)

	// GPUAllocationSuccess tracks successful GPU allocations
	GPUAllocationSuccess = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubenexus_gpu_allocation_success_total",
			Help: "Successful GPU allocations by workload type",
		},
		[]string{"workload_type", "gpu_count", "topology_aware"},
	)

	// GPUAllocationFailures tracks GPU allocation failures
	GPUAllocationFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubenexus_gpu_allocation_failures_total",
			Help: "GPU allocation failures by reason",
		},
		[]string{"reason", "workload_type"},
	)

	// Gang Scheduling (Coscheduling) Metrics

	// GangSchedulingDecisions tracks gang scheduling outcomes
	GangSchedulingDecisions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubenexus_gang_scheduling_decisions_total",
			Help: "Gang scheduling decisions by outcome",
		},
		[]string{"outcome", "namespace"},
	)

	// GangWaitingTime tracks how long gang members wait for the full gang
	GangWaitingTime = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kubenexus_gang_waiting_time_seconds",
			Help:    "Time gang members wait for full gang formation",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600},
		},
		[]string{"namespace", "pod_group"},
	)

	// GangStarvationPreventions tracks starvation prevention priority boosts
	GangStarvationPreventions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubenexus_gang_starvation_preventions_total",
			Help: "Number of times starvation prevention boosted gang priority",
		},
		[]string{"namespace", "pod_group"},
	)

	// GangCompletionLatency tracks end-to-end gang scheduling time
	GangCompletionLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kubenexus_gang_completion_latency_seconds",
			Help:    "Time from first pod to complete gang formation",
			Buckets: []float64{5, 15, 30, 60, 120, 300, 600, 1200},
		},
		[]string{"namespace", "pod_group", "gang_size"},
	)

	// NUMA Topology Metrics

	// NumaPlacementDecisions tracks NUMA placement outcomes
	NumaPlacementDecisions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubenexus_numa_placement_decisions_total",
			Help: "NUMA placement decisions by policy and outcome",
		},
		[]string{"policy", "outcome", "workload_type"},
	)

	// NumaFitQuality tracks NUMA fit quality scores
	NumaFitQuality = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kubenexus_numa_fit_quality_score",
			Help:    "NUMA fit quality score (0-100)",
			Buckets: []float64{0, 25, 50, 75, 90, 95, 100},
		},
		[]string{"policy"},
	)

	// NumaMemoryBandwidthPressure tracks memory bandwidth utilization
	NumaMemoryBandwidthPressure = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubenexus_numa_memory_bandwidth_pressure_percent",
			Help: "NUMA memory bandwidth pressure percentage",
		},
		[]string{"pressure_level"},
	)

	// NumaDistancePenalty tracks cross-NUMA placement penalties
	NumaDistancePenalty = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubenexus_numa_distance_penalty_total",
			Help: "Cross-NUMA distance penalties applied",
		},
		[]string{"distance_level"},
	)

	// NumaGangAffinitySuccess tracks successful gang NUMA co-location
	NumaGangAffinitySuccess = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubenexus_numa_gang_affinity_success_total",
			Help: "Successful gang NUMA co-location events",
		},
		[]string{"gang_policy", "numa_id"},
	)

	// Resource Fragmentation Metrics

	// IslandProtectionEvents tracks fragmentation prevention actions
	IslandProtectionEvents = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubenexus_island_protection_events_total",
			Help: "Island fragmentation protection actions taken",
		},
		[]string{"protection_type", "prevented"},
	)

	// PerfectFitPlacements tracks GPU island perfect fit placements
	PerfectFitPlacements = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubenexus_perfect_fit_placements_total",
			Help: "GPU island perfect fit placements",
		},
		[]string{"island_size", "topology_type"},
	)

	// IslandCompletions tracks GPU island completion events
	IslandCompletions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kubenexus_island_completions_total",
			Help: "GPU island completion events (fully utilized islands)",
		},
		[]string{"island_size", "topology_quality"},
	)

	// FragmentationScoreDistribution tracks fragmentation score distribution
	FragmentationScoreDistribution = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kubenexus_fragmentation_score",
			Help:    "Resource fragmentation score distribution",
			Buckets: []float64{0, 10, 20, 30, 50, 75, 90, 100},
		},
		[]string{"resource_type"},
	)

	// IslandQualityDistribution tracks GPU island quality distribution
	IslandQualityDistribution = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kubenexus_island_quality_score",
			Help:    "GPU island topology quality score distribution",
			Buckets: []float64{30, 50, 80, 100},
		},
		[]string{"topology_type"},
	)
)
