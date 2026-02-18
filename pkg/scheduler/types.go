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

package scheduler

import "time"

const (
	// SchedulerName is the name of the KubeNexus scheduler
	SchedulerName = "kubenexus-scheduler"

	// Label keys for pod group scheduling
	PodGroupNameLabel         = "pod-group.scheduling.kubenexus.io/name"
	PodGroupMinAvailableLabel = "pod-group.scheduling.kubenexus.io/min-available"

	// Annotation keys for resource requirements
	AnnotationDriverCPU      = "kubenexus.io/driver-cpu"
	AnnotationDriverMemory   = "kubenexus.io/driver-memory"
	AnnotationExecutorCPU    = "kubenexus.io/executor-cpu"
	AnnotationExecutorMemory = "kubenexus.io/executor-memory"
	AnnotationExecutorCount  = "kubenexus.io/executor-count"

	// Dynamic allocation annotations
	AnnotationDynamicAllocationEnabled      = "kubenexus.io/dynamic-allocation-enabled"
	AnnotationDynamicAllocationMinExecutors = "kubenexus.io/min-executors"
	AnnotationDynamicAllocationMaxExecutors = "kubenexus.io/max-executors"

	// Spark specific labels (for compatibility)
	SparkAppIDLabel = "spark-app-id"
	SparkRoleLabel  = "spark-role"
)

// SchedulingConfig holds the configuration for the scheduler
type SchedulingConfig struct {
	// PermitWaitingTime is the default timeout for pods waiting in Permit stage
	PermitWaitingTime time.Duration

	// BinpackStrategy defines the node selection strategy
	BinpackStrategy string

	// EnableGangScheduling enables gang scheduling feature
	EnableGangScheduling bool

	// EnableResourceReservation enables resource reservation feature
	EnableResourceReservation bool

	// FIFOEnabled enables FIFO ordering for pod groups
	FIFOEnabled bool
}

// BinpackStrategy types
const (
	BinpackStrategyDistributeEvenly = "distribute-evenly"
	BinpackStrategyTightlyPack      = "tightly-pack"
)

// PodGroupStatus represents the status of a pod group
type PodGroupStatus string

const (
	PodGroupStatusPending    PodGroupStatus = "Pending"
	PodGroupStatusScheduling PodGroupStatus = "Scheduling"
	PodGroupStatusScheduled  PodGroupStatus = "Scheduled"
	PodGroupStatusFailed     PodGroupStatus = "Failed"
)
