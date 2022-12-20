package kubeaccess

// this file tests kubernetes functions based on an empty kind cluster

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/kind/pkg/cluster"
)

const kindClusterName = "kustomizepbtest"
const knownNodeName = kindClusterName + "-control-plane"
const recreateClusterIfExists = false

var kubeconfigpath string

var ka *KubeAccess

func TestMain(m *testing.M) {
	var err error

	kubeconfigpath = filepath.Join(os.TempDir(), "kind-kustomizepb")
	log.Printf("KUBECONFIG=%s\n", kubeconfigpath)

	prov := cluster.NewProvider()
	names, err := prov.List()
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	clusterExists := false

	for _, n := range names {
		if n == kindClusterName {
			clusterExists = true
			break
		}
	}

	if clusterExists && recreateClusterIfExists {
		log.Printf("delete kind cluster '%s'", kindClusterName)
		err = prov.Delete(kindClusterName, kubeconfigpath)
		if err != nil {
			log.Fatal(err)
			os.Exit(1)
		}

		clusterExists = false
	}

	if !clusterExists {
		log.Printf("create kind cluster '%v', kubeconfig '%v'", kindClusterName, kubeconfigpath)
		err = prov.Create(
			kindClusterName,
			cluster.CreateWithKubeconfigPath(kubeconfigpath),
			cluster.CreateWithWaitForReady(time.Minute),
		)

		if err != nil {
			log.Fatal(err)
			os.Exit(1)
		}

	}

	ka, err = NewKubeAccess(kubeconfigpath)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	rc := m.Run()
	os.Exit(rc)

}

func TestGetNodeObject(t *testing.T) {
	var err error

	ka, err := NewKubeAccess(kubeconfigpath)
	assert.NoError(t, err)

	gvr, err := ka.TryGetGroupVersionResource("v1", "Node")
	assert.NoError(t, err)
	assert.NotNil(t, gvr)

	node, err := ka.GetObject(context.Background(), *gvr, "", knownNodeName)
	assert.NoError(t, err)
	assert.NotNil(t, node)
	assert.Equal(t, knownNodeName, node.GetName())
	assert.Equal(t, "v1", node.GetAPIVersion())

}

func TestServiceReady(t *testing.T) {
	var err error
	ctx := context.Background()

	ka, err := NewKubeAccess(kubeconfigpath)
	assert.NoError(t, err)

	cv1 := ka.KubeClientset.CoreV1()

	// test for namespace
	nsn := "testserviceeready"
	deleteNsIfExists(t, ctx, ka, nsn)
	createNs(t, ctx, ka, nsn)

	// create the service
	srv := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: nsn,
		},
		Spec: v1.ServiceSpec{
			Selector: map[string]string{
				"label": "value",
			},
			Ports: []v1.ServicePort{
				{
					Port: 80,
				},
			},
		},
	}

	_, err = cv1.Services(nsn).Create(ctx, &srv, metav1.CreateOptions{})
	assert.NoError(t, err)

	// ServiceReady must return false, since there is no pod started
	ready, err := ka.IsServiceReady(ctx, "service", nsn)
	assert.NoError(t, err)
	assert.False(t, ready)

	// start a matching pod
	pod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: nsn,
			Labels: map[string]string{
				"label": "value",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "nginx",
					Image: "nginx",
				},
			},
		},
	}

	_, err = cv1.Pods(nsn).Create(ctx, &pod, metav1.CreateOptions{})
	assert.NoError(t, err)

	ready = false

	// it will take a while to start the pod, so we will wait for 20 seconds max
	for i := 0; i < 20; i++ {
		log.Printf("Try service ready, iteration=%d", i)
		ready, err = ka.IsServiceReady(ctx, "service", nsn)
		assert.NoError(t, err)

		if ready {
			break
		}

		time.Sleep(time.Second)
	}

	// ready now?
	assert.True(t, ready)
}

func deleteNsIfExists(t *testing.T, ctx context.Context, ka *KubeAccess, nsn string) {
	cv1 := ka.KubeClientset.CoreV1()
	_, err := cv1.Namespaces().Get(ctx, nsn, metav1.GetOptions{})

	if err != nil {
		if kerrors.IsNotFound(err) {
			return
		} else {
			assert.NoError(t, err)
		}
	}

	err = cv1.Namespaces().Delete(ctx, nsn, metav1.DeleteOptions{})
	assert.NoError(t, err)

	// wait until it's really gone
	for i := 0; i < 20; i++ {
		log.Printf("Wait for ns deletion, iteration=%d", i)
		_, err := cv1.Namespaces().Get(ctx, nsn, metav1.GetOptions{})

		if err != nil {
			if kerrors.IsNotFound(err) {
				return
			} else {
				assert.NoError(t, err)
			}
		}
		time.Sleep(time.Second)
	}
}

func createNs(t *testing.T, ctx context.Context, ka *KubeAccess, nsn string) {
	cv1 := ka.KubeClientset.CoreV1()

	_, err := cv1.Namespaces().Create(
		ctx,
		&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsn}},
		metav1.CreateOptions{},
	)

	assert.NoError(t, err)
}
