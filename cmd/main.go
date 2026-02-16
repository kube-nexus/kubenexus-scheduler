package main

import (
	"os"

	"k8s.io/kubernetes/cmd/kube-scheduler/app"
	
	// Register coscheduling plugin
	coscheduling "sigs.k8s.io/scheduler-plugins/pkg/coscheduling"
)

func main() {
	command := app.NewSchedulerCommand(
		app.WithPlugin(coscheduling.Name, coscheduling.New),
	)

	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}
