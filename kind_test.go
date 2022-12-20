package main

// this file tests kubernetes functions based on an empty kind cluster

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gprossliner/kustomizepb/kubeaccess"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/kind/pkg/cluster"
)

const kindClusterName = "kustomizepbtest"
const knownNodeName = kindClusterName + "-control-plane"
const recreateClusterIfExists = false

var kubeconfigpath string

var ka *kubeaccess.KubeAccess

func TestMain(m *testing.M) {
	var err error

	kubeconfigpath = filepath.Join(os.TempDir(), "kind-kustomizepb")

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

	ka, err = kubeaccess.NewKubeAccess(kubeconfigpath)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	rc := m.Run()
	os.Exit(rc)

}

func Test_getnode(t *testing.T) {
	var err error

	ka, err := kubeaccess.NewKubeAccess(kubeconfigpath)
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
