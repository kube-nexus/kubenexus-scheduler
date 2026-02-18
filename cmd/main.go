package main

import (
	"os"

	"k8s.io/kubernetes/cmd/kube-scheduler/app"

	// Register scheduler plugins
	backfill "sigs.k8s.io/scheduler-plugins/pkg/plugins/backfill"
	coscheduling "sigs.k8s.io/scheduler-plugins/pkg/plugins/coscheduling"
	numatopology "sigs.k8s.io/scheduler-plugins/pkg/plugins/numatopology"
	gangpreemption "sigs.k8s.io/scheduler-plugins/pkg/plugins/preemption"
	resourcereservation "sigs.k8s.io/scheduler-plugins/pkg/plugins/resourcereservation"
	topologyspread "sigs.k8s.io/scheduler-plugins/pkg/plugins/topologyspread"
	workloadaware "sigs.k8s.io/scheduler-plugins/pkg/plugins/workloadaware"
)

func main() {
	command := app.NewSchedulerCommand(
		// Gang scheduling and coordination
		app.WithPlugin(coscheduling.Name, coscheduling.New),

		// Gang-aware preemption
		app.WithPlugin(gangpreemption.Name, gangpreemption.New),

		// Resource reservation to prevent starvation
		app.WithPlugin(resourcereservation.Name, resourcereservation.New),

		// Workload-aware scoring: bin packing for batch, spreading for services
		app.WithPlugin(workloadaware.Name, workloadaware.New),

		// Zone-aware topology spreading for high availability
		app.WithPlugin(topologyspread.Name, topologyspread.New),

		// NUMA-aware scheduling for high-performance workloads
		app.WithPlugin(numatopology.Name, numatopology.New),

		// Backfill scoring: fills idle capacity with low-priority interruptible pods
		app.WithPlugin(backfill.Name, backfill.New),
	)

	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}
