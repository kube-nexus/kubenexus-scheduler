package main

import (
	"os"

	"k8s.io/klog/v2"
	"k8s.io/kubernetes/cmd/kube-scheduler/app"
	
	// Register scheduler plugins
	coscheduling "sigs.k8s.io/scheduler-plugins/pkg/plugins/coscheduling"
	resourcereservation "sigs.k8s.io/scheduler-plugins/pkg/plugins/resourcereservation"
	topologyspread "sigs.k8s.io/scheduler-plugins/pkg/plugins/topologyspread"
)

func main() {
	klog.InfoS("KubeNexus Scheduler starting", "version", "v0.1.0")
	
	command := app.NewSchedulerCommand(
		app.WithPlugin(coscheduling.Name, coscheduling.New),
		app.WithPlugin(resourcereservation.Name, resourcereservation.New),
		app.WithPlugin(topologyspread.TopologyScoringName, topologyspread.NewTopologySpreadScore),
	)

	klog.InfoS("Executing scheduler command")
	if err := command.Execute(); err != nil {
		klog.ErrorS(err, "Scheduler command failed")
		os.Exit(1)
	}
	klog.InfoS("Scheduler command completed")
}
