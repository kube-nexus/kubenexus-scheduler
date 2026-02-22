package main

import (
	"os"

	klog "k8s.io/klog/v2"
	"k8s.io/kubernetes/cmd/kube-scheduler/app"

	// Register scheduler plugins
	"sigs.k8s.io/scheduler-plugins/pkg/plugins/backfill"
	"sigs.k8s.io/scheduler-plugins/pkg/plugins/coscheduling"
	"sigs.k8s.io/scheduler-plugins/pkg/plugins/networkfabric"
	"sigs.k8s.io/scheduler-plugins/pkg/plugins/numatopology"
	"sigs.k8s.io/scheduler-plugins/pkg/plugins/preemption"
	"sigs.k8s.io/scheduler-plugins/pkg/plugins/resourcefragmentation"
	"sigs.k8s.io/scheduler-plugins/pkg/plugins/resourcereservation"
	"sigs.k8s.io/scheduler-plugins/pkg/plugins/tenanthardware"
	"sigs.k8s.io/scheduler-plugins/pkg/plugins/topologyspread"
	"sigs.k8s.io/scheduler-plugins/pkg/plugins/workloadaware"
)

func main() {
	klog.InfoS("KubeNexus Scheduler starting", "version", "v0.1.0")

	command := app.NewSchedulerCommand(
		// Core scheduling plugins
		app.WithPlugin(coscheduling.Name, coscheduling.New),
		app.WithPlugin(resourcereservation.Name, resourcereservation.New),

		// Scoring plugins
		app.WithPlugin(workloadaware.Name, workloadaware.New),
		app.WithPlugin(topologyspread.TopologyScoringName, topologyspread.NewTopologySpreadScore),
		app.WithPlugin(backfill.Name, backfill.New),
		app.WithPlugin(resourcefragmentation.Name, resourcefragmentation.New),
		app.WithPlugin(tenanthardware.Name, tenanthardware.New),
		app.WithPlugin(networkfabric.Name, networkfabric.New),

		// Advanced plugins
		app.WithPlugin(numatopology.Name, numatopology.New),
		app.WithPlugin(preemption.Name, preemption.New),
	)

	klog.InfoS("Executing scheduler command")
	if err := command.Execute(); err != nil {
		klog.ErrorS(err, "Scheduler command failed")
		os.Exit(1)
	}
	klog.InfoS("Scheduler command completed")
}
