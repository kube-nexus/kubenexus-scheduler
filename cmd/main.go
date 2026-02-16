package main

import (
	"os"

	"k8s.io/kubernetes/cmd/kube-scheduler/app"
	
	// Register scheduler plugins
	coscheduling "sigs.k8s.io/scheduler-plugins/pkg/coscheduling"
	resourcereservation "sigs.k8s.io/scheduler-plugins/pkg/resourcereservation"
)

func main() {
	command := app.NewSchedulerCommand(
		app.WithPlugin(coscheduling.Name, coscheduling.New),
		app.WithPlugin(resourcereservation.Name, resourcereservation.New),
	)

	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}
