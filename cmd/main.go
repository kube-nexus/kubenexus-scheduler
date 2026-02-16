package main

import (
	"os"

	"k8s.io/kubernetes/cmd/kube-scheduler/app"
	
	// Register scheduler plugins
	coscheduling "sigs.k8s.io/scheduler-plugins/pkg/plugins/coscheduling"
	resourcereservation "sigs.k8s.io/scheduler-plugins/pkg/plugins/resourcereservation"
	"sigs.k8s.io/scheduler-plugins/pkg/plugins/scoring"
)

func main() {
	command := app.NewSchedulerCommand(
		app.WithPlugin(coscheduling.Name, coscheduling.New),
		app.WithPlugin(resourcereservation.Name, resourcereservation.New),
		app.WithPlugin(scoring.Name, scoring.New),
		app.WithPlugin(scoring.PluginName, scoring.NewTopologySpreadScore),
	)

	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}
