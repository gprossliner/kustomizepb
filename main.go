package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"

	"k8s.io/client-go/util/homedir"

	"github.com/gprossliner/kustomizepb/execution"
	"github.com/gprossliner/kustomizepb/knownerror"
	"github.com/gprossliner/kustomizepb/kubeaccess"
	"github.com/gprossliner/kustomizepb/output"
	"github.com/joho/godotenv"
)

func main() {
	err := themain(context.Background())
	knownError, isKnownError := err.(*knownerror.KnownError)

	if err != nil {
		if isKnownError {
			output.Error(knownError.Error())
			os.Exit(1)
		} else {
			panic(err)
		}
	}
}

func themain(ctx context.Context) error {

	var kubeconfig, envfile, knownNode string

	flag.StringVar(&kubeconfig, "kubeconfig", filepath.Join(homedir.HomeDir(), ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	flag.StringVar(&envfile, "envfile", "", "file for envsubst")
	flag.StringVar(&knownNode, "knownNode", "", "specify the name of a cluster node that must exist")

	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	ka, err := kubeaccess.NewKubeAccess(kubeconfig)
	if err != nil {
		return err
	}

	options := &execution.Options{
		KubeAccess: ka,
		Directory:  flag.Arg(0),
	}

	// validate knownNode
	if knownNode != "" {
		err = validateKnownNode(ctx, knownNode, options)
		if err != nil {
			return err
		}
	}

	// process envfile
	if envfile != "" {
		envMap, err := godotenv.Read(envfile)
		if err != nil {
			if os.IsNotExist(err) {
				return knownerror.NewKnownError("envfile %s doesn't exist", envfile)
			}
			return err
		}

		options.Envs = envMap
	}

	run, err := execution.LoadRun(ctx, options)
	if err != nil {
		return err
	}

	events := make(chan execution.RunEvent)
	go func() {

		for {

			switch event := <-events; event.ID {
			case execution.EV_ComponentStarted:
				output.HeadingF("Processing component '%s'", event.Component.Name)

			case execution.EV_ComponentApplying:
				output.InfoF("Applying component")

			case execution.EV_TestReadiness:
				output.InfoF("Testing readiness")

			case execution.EV_ComponentReady:
				output.InfoF("Component ready")

			default:
				return
			}

		}
	}()

	defer close(events)

	err = run.Run(ctx, options, events)
	if err != nil {
		return err
	}

	return nil
}

func validateKnownNode(ctx context.Context, nodeName string, options *execution.Options) error {

	hasNode, err := options.KubeAccess.HasNode(ctx, nodeName)
	if err != nil {
		return err
	}

	if !hasNode {
		return knownerror.NewKnownError("The known Node %s was not found", nodeName)
	}

	return nil

}
