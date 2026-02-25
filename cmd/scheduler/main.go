package main

import (
	"os"

	klog "k8s.io/klog/v2"
	"k8s.io/kubernetes/cmd/kube-scheduler/app"

	// Register scheduler plugins
	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/backfill"
	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/coscheduling"
	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/networkfabric"
	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/numatopology"
	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/preemption"
	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/profileclassifier"
	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/resourcefragmentation"
	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/resourcereservation"
	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/tenanthardware"
	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/topologyspread"
	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/vramscheduler"
	"github.com/kube-nexus/kubenexus-scheduler/pkg/plugins/workloadaware"
)

func main() {
	klog.InfoS("KubeNexus Scheduler starting", "version", "v0.1.0")

	command := app.NewSchedulerCommand(
		// Classification hub - MUST run first in PreFilter
		app.WithPlugin(profileclassifier.Name, profileclassifier.New),

		// Core scheduling plugins
		app.WithPlugin(coscheduling.Name, coscheduling.New),
		app.WithPlugin(resourcereservation.Name, resourcereservation.New),

		// Scoring plugins
		app.WithPlugin(workloadaware.Name, workloadaware.New),
		app.WithPlugin(topologyspread.TopologyScoringName, topologyspread.NewTopologySpreadScore),
		app.WithPlugin(backfill.Name, backfill.New),
		app.WithPlugin(resourcefragmentation.Name, resourcefragmentation.New),
		app.WithPlugin(tenanthardware.Name, tenanthardware.New),
		app.WithPlugin(vramscheduler.Name, vramscheduler.New),
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
