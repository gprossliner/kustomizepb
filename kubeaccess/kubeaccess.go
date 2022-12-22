package kubeaccess

import (
	"context"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Options are the processed options for the caller
type KubeAccess struct {
	KubeRest       *rest.Config
	KubeDynClient  *dynamic.DynamicClient
	KubeDiscClient *discovery.DiscoveryClient
	KubeClientset  *kubernetes.Clientset
}

func NewKubeAccess(kubeconfig string, kubecontext string) (*KubeAccess, error) {

	options := &KubeAccess{}

	// use the current context in kubeconfig
	if kubecontext == "" {
		kconfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
		options.KubeRest = kconfig
	} else {
		kconfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig},
			&clientcmd.ConfigOverrides{
				CurrentContext: kubecontext,
			}).ClientConfig()

		if err != nil {
			return nil, err
		}
		options.KubeRest = kconfig
	}

	// build a dynamicclient
	dynClient, err := dynamic.NewForConfig(options.KubeRest)
	if err != nil {
		return nil, err
	}
	options.KubeDynClient = dynClient

	cs, err := kubernetes.NewForConfig(options.KubeRest)
	if err != nil {
		return nil, err
	}
	options.KubeClientset = cs

	options.KubeDiscClient = discovery.NewDiscoveryClient(cs.RESTClient())

	return options, nil
}

func (ka *KubeAccess) HasNode(ctx context.Context, name string) (bool, error) {
	_, err := ka.KubeClientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})

	if err != nil {
		if kerrors.IsNotFound(err) {
			return false, nil
		} else {
			return false, err
		}
	}

	return true, nil
}

func (ka *KubeAccess) GetObject(ctx context.Context, gvr schema.GroupVersionResource, namespace string, name string) (*unstructured.Unstructured, error) {

	var ri dynamic.ResourceInterface

	h := ka.KubeDynClient.Resource(gvr)

	if namespace != "" {
		ri = h.Namespace(namespace)
	} else {
		ri = h
	}

	obj, err := ri.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return obj, nil
}

// GroupVersionResourceFromApiVersion tries to find the resource based on GVK.
// If no matching resource can be found, nil is returned
func (ka *KubeAccess) TryGetGroupVersionResource(apiVersion string, kind string) (*schema.GroupVersionResource, error) {

	// based on https://github.com/kubernetes/kubectl/blob/master/pkg/cmd/apiresources/apiresources.go#L157

	gvk := schema.FromAPIVersionAndKind(apiVersion, kind)

	lists, err := ka.KubeDiscClient.ServerPreferredResources()
	if err != nil {
		return nil, err
	}

	for _, list := range lists {
		if list.GroupVersion != apiVersion {
			continue
		}

		for _, resource := range list.APIResources {

			if resource.Kind == gvk.Kind {
				return &schema.GroupVersionResource{
					Group:    gvk.Group,
					Version:  gvk.Version,
					Resource: resource.Name,
				}, nil
			}

		}
	}

	return nil, nil
}

func (ka *KubeAccess) HasCustomResourceName(ctx context.Context, customResourceName string) (bool, error) {
	// get crds
	crds, err := ka.KubeDynClient.
		Resource(schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}).
		List(ctx, metav1.ListOptions{})

	if err != nil {
		return false, err
	}

	for _, crd := range crds.Items {
		if crd.GetName() == customResourceName {
			return true, nil
		}
	}

	return false, nil
}

func (ka *KubeAccess) IsServiceReady(ctx context.Context, name, namespace string) (bool, error) {

	// get the service
	// service, err := ka.KubeClientset.CoreV1().
	// 	Services(namespace).
	// 	Get(ctx, name, metav1.GetOptions{})

	// if err != nil {
	// 	return false, err
	// }

	// find the endpoints by name
	// TODO: check if it's really guaranteed that we always have an endpoint for
	// a given service with the same name
	ep, err := ka.KubeClientset.CoreV1().Endpoints(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	// check if there are endpoints
	hasActiveEP := len(ep.Subsets) > 0 && len(ep.Subsets[0].Addresses) > 0

	return hasActiveEP, nil
}
