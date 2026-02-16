package resourceReservation

import (
	"context"
	"os"
	"k8s.io/klog"
	"k8s.io/client-go/tools/clientcmd"
	
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1beta1 "github.com/palantir/k8s-spark-scheduler-lib/pkg/apis/sparkscheduler/v1beta1"
	"github.com/palantir/k8s-spark-scheduler-lib/pkg/client/clientset/versioned/scheme"
	rest "k8s.io/client-go/rest" // this is v0.18.6
	//"k8s.io/kubernetes/pkg/api/v1/pod"
	corelisters "k8s.io/client-go/listers/core/v1"
	//"k8s.io/klog"
	framework "k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"
	//"k8s.io/kubernetes/pkg/scheduler/nodeinfo"
	//"github.com/palantir/k8s-spark-scheduler-lib/pkg/apis/sparkscheduler/v1beta1"

)

var podGroupVersionKind = v1.SchemeGroupVersion.WithKind("Pod")

type stateData struct {
	data string
}

func (s *stateData) Clone() framework.StateData {
	copy := &stateData{
		data: s.data,
	}
	return copy
}

// Name is the name of the plugin used in the plugin registry and configurations.
const (
	Name = "resourceReservation"
	PodGroupName = "pod-group.scheduling.sigs.k8s.io/name"
)

// resourceReservation is a plugin that implements QoS class based sorting.
type resourceReservation struct {
	frameworkHandle framework.FrameworkHandle
	podLister       corelisters.PodLister
	client 			rest.Interface
	// add more extender configs here
}

var _ framework.ReservePlugin = &resourceReservation{}
var _ framework.UnreservePlugin = &resourceReservation{}

// Name returns name of the plugin.
func (pl *resourceReservation) Name() string {
	return Name
}

// New initializes a new plugin and returns it.
func New(_ *runtime.Unknown, handle framework.FrameworkHandle) (framework.Plugin, error) {
	// initialize all the extender related configs here and
	// so you would be using something like rr.c.client.Post()
	podLister := handle.SharedInformerFactory().Core().V1().Pods().Lister()
	// the client-go client initialization comes here
	var kubeconfig *rest.Config
	var err error
	// kubeconfig, err = rest.InClusterConfig()

	// copy this while building docker image
	if _, err := os.Stat("/opt/config-file"); err == nil || os.IsExist(err) {
		kubeconfig, err = clientcmd.BuildConfigFromFlags("", "/opt/config-file")
		if err != nil {
			klog.V(3).Infof("error building config from kube-config/ inside goutham")
			return nil, err
		}
	} else {
		kubeconfig, err = rest.InClusterConfig()
		if err != nil {
			klog.V(3).Infof("error building in-cluster kube-config/ inside goutham")
			return nil, err
		}
	}

	config := *kubeconfig

	if err := setConfigDefaults(&config); err != nil {
		return nil, err
	}
	
	client, err := rest.RESTClientFor(&config) // vs NewForConfig
	if err != nil {
		return nil, err
	}

	return &resourceReservation{frameworkHandle: handle,
		podLister: podLister,
		client: client,
	}, nil
}

func setConfigDefaults(config *rest.Config) error {
	gv := v1beta1.SchemeGroupVersion
	config.GroupVersion = &gv
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()

	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}

	return nil
}

// Reserve is the function invoked by the framework at "reserve" extension point.
func (rr *resourceReservation) Reserve(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) *framework.Status {
	//TODO: (gkotapalle) Create reservations using the extender's CRD (import these packages from k8s-scheduler-extender-lib)
	//TODO: (gkotapalle) getResourceReservation to check for existing reservations

	rres := newResourceReservation(nodeName, pod)
	_, err := rr.create(ctx, rres, pod) //bsc
	if err != nil {
		return nil //framework.error
	}

	if pod == nil {
		return framework.NewStatus(framework.Error, "pod cannot be nil")
	}
	if pod.Name == "my-test-pod" {
		state.Lock()
		state.Write(framework.StateKey(pod.Name), &stateData{data: "never bind"})
		state.Unlock()
	}
	return nil // nil is not an error
}

// Unreserve is the function invoked by the framework when any error happens
// during "reserve" extension point or later.
func (rr *resourceReservation) Unreserve(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) {
	// TODO: (gkotapalle) delete the reservations if a falure occurs in the reserve stage or binding cycle
	if pod.Name == "my-test-pod" {
		state.Lock()
		// The pod is at the end of its lifecycle -- let's clean up the allocated
		// resources. In this case, our clean up is simply deleting the key written
		// in the Reserve operation.
		state.Delete(framework.StateKey(pod.Name))
		state.Unlock()
	}
}

func newResourceReservation(driverNode string, driver *v1.Pod) *v1beta1.ResourceReservation {
	reservations := make(map[string]v1beta1.Reservation, 1)
	cpu, _ := resource.ParseQuantity("1")
	mem, _ := resource.ParseQuantity("750M")
	reservations[driver.Name] = v1beta1.Reservation{
		Node:   driverNode,
		CPU:    cpu, //replace with actual pod.cpu,
		Memory: mem, //replace with actual pod.memory,
	}
	// for idx, nodeName := range executorNodes {
	// 	reservations[executorReservationName(idx)] = v1beta1.Reservation{
	// 		Node:   nodeName,
	// 		CPU:    executorResources.CPU,
	// 		Memory: executorResources.Memory,
	// 	}
	// }
	return &v1beta1.ResourceReservation{
		ObjectMeta: metav1.ObjectMeta{
			Name:            driver.Labels[driver.Name],
			Namespace:       driver.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(driver, podGroupVersionKind)},
			Labels: map[string]string{
				v1beta1.AppIDLabel: driver.Labels[driver.Name],
			},
		},
		Spec: v1beta1.ResourceReservationSpec{
			Reservations: reservations,
		},
		Status: v1beta1.ResourceReservationStatus{
			Pods: map[string]string{"driver": driver.Name}, //change this
		},
	}
}

func (rr *resourceReservation) create(ctx context.Context, resourceReservation *v1beta1.ResourceReservation, pod *v1.Pod) (result *v1beta1.ResourceReservation, err error) { //caps to share
	result = &v1beta1.ResourceReservation{}// result has the type of by this
	// get the correct client here --> mock resourceReservations.client here. Replace c.client with rr.client
	// this is compatible with the go client. So the k8s-lib uses the newer client under the hood
	err = rr.client.Post().
		Namespace(pod.Namespace).
		Resource("resourcereservations").
		Body(resourceReservation).
		Do(ctx).
		Into(result) //err has the type returned by this
	return //man return otherwise
} //just like yaml