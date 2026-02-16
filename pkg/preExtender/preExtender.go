package preExtender

import (
	"context"
	//"time"
	// "math/rand"
	// "strconv"
	
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	//"k8s.io/kubernetes/pkg/api/v1/pod"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog"
	framework "k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"
	"k8s.io/kubernetes/pkg/scheduler/nodeinfo"

)

// Name is the name of the plugin used in the plugin registry and configurations.
const (
	Name = "preExtender"
	PodGroupName = "pod-group.scheduling.sigs.k8s.io/name"
)

// PreExtender is a plugin that implements QoS class based sorting.
type PreExtender struct {
	frameworkHandle framework.FrameworkHandle
	podLister       corelisters.PodLister
}

var _ framework.PreFilterPlugin = &PreExtender{}
var _ framework.FilterPlugin = &PreExtender{}

// Name returns name of the plugin.
func (pl *PreExtender) Name() string {
	return Name
}

// PreFilter gets all the pods belonging to a particular replicaset/deployment and injects a random "id" label for
// the modified extender to use for reservations
// can move the logic to any extension point before score as the extender webhook is called post that
func (cs *PreExtender) PreFilter(ctx context.Context, state *framework.CycleState, pod *v1.Pod) *framework.Status {
	klog.V(3).Infof("Inside prefilter at preExtender")
	//podGroupName, exist := p.Labels[PodGroupName]
	appid, exist := pod.Labels["spark-app-id"]
	// rand.Seed(time.Now().UnixNano())
	// random_int := rand.Intn(900000)

	//pods need not have a podgroup as per the current logic
	if !exist || appid == "" {
		//return framework.NewStatus(framework.Error, err.Error())
		pod.Labels["spark-app-id"] = pod.Name
		klog.V(3).Infof("inside if")
	} else {
		klog.V(3).Infof("inside else")
		if pod.Labels["spark-app-id"] == pod.Name {
			//return framework.NewStatus(framework.Success, "")
			//do nothing as pod's app-id is set correctly - takes care of redundant cases
		} else {
			pod.Labels["spark-app-id"] = pod.Name
			klog.V(3).Infof("setting the app-id to name for uniqueness")
		}
	}
	klog.V(3).Infof("label at pre="+pod.Labels["spark-app-id"])
	//return framework.NewStatus(framework.Unschedulable, "less than minAvailable")
	return framework.NewStatus(framework.Success, "")
}

func (cs *PreExtender) Filter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeInfo *nodeinfo.NodeInfo) *framework.Status {
	klog.V(3).Infof("Inside the Filter function of preExtender")
	klog.V(3).Infof("label at filter="+pod.Labels["spark-app-id"])

	return framework.NewStatus(framework.Success, "")
}

// PreFilterExtensions returns nil
func (cs *PreExtender) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}


// New initializes a new plugin and returns it.
func New(_ *runtime.Unknown, handle framework.FrameworkHandle) (framework.Plugin, error) {
	podLister := handle.SharedInformerFactory().Core().V1().Pods().Lister()
	return &PreExtender{frameworkHandle: handle,
		podLister: podLister,
	}, nil
}