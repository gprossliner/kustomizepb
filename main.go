package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"time"

	"k8s.io/client-go/util/homedir"

	"gopkg.in/yaml.v3"

	"github.com/gprossliner/kustomizepb/knownerror"
	"github.com/gprossliner/kustomizepb/kubeaccess"
	"github.com/gprossliner/kustomizepb/output"
	"github.com/gprossliner/kustomizepb/playbook"
	"github.com/joho/godotenv"
)

// https://caiorcferreira.github.io/post/the-kubernetes-dynamic-client/

type RunComponent struct {
	playbook.Component
	Run            *Run
	InCurrentBatch bool
	Applied        bool
}

type Run struct {
	Directory             string
	KustomizationFilePath string
	Components            []RunComponent
}

const (
	PlaybookFileName = "kustomizationplaybook.yaml"

	// KustomizationFileName is the name of the generated Kustomization File
	// Is is not allowed to exist upfront
	// according to https://github.com/kubernetes-sigs/kustomize/blob/ef60d5f9bb3228ff84ad7b44b066ba76de086310/api/konfig/general.go#L10
	// this is the file the Kustomize uses if there are multiple files found
	KustomizationFileName = "kustomization.yaml"
)

func loadRun(ctx context.Context, options *Options) (*Run, error) {

	var err error

	// check directory
	directory := options.Directory

	stat, err := os.Stat(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, knownerror.NewKnownError("Directoy %s doesn't exist", directory)
		}
		return nil, err
	}

	if !stat.IsDir() {
		return nil, knownerror.NewKnownError("%s is not a directory", directory)
	}

	playbookFile := path.Join(directory, PlaybookFileName)
	stat, err = os.Stat(playbookFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, knownerror.NewKnownError("File %s doesn't exist", playbookFile)
		}
		return nil, err
	} else if !stat.Mode().IsRegular() {
		return nil, knownerror.NewKnownError("Path %s is not a regular file", directory)
	}

	// generate output file
	kustomizationFile := path.Join(directory, KustomizationFileName)
	if pathExists(kustomizationFile) {
		return nil, knownerror.NewKnownError("There is already a file %s, which is not supported", KustomizationFileName)
	}

	// deserialize file
	data, err := os.ReadFile(playbookFile)
	if err != nil {
		return nil, err
	}

	if err != nil {
		return nil, err
	}

	// load
	playbook, err := playbook.Unmarshal(data)
	if err != nil {
		return nil, err
	}

	// validate
	errs := playbook.Validate()
	if len(errs) > 0 {
		for _, err := range errs {
			fmt.Println(err.Error())
		}
		os.Exit(1)
	}

	// perform envsubst
	err = playbook.EnvSubst(options.Envs)
	if err != nil {
		return nil, err
	}

	// validate prerequisites
	for _, pr := range playbook.Prerequisites {
		isff, err := pr.IsFulfilled(ctx, options.KubeAccess)
		if err != nil {
			return nil, err
		}

		if !isff {
			msg := pr.Message
			if msg == "" {
				msg = "Prerequisite check failed"
			}

			return nil, knownerror.NewKnownError(msg)
		}
	}

	run := &Run{
		Directory:             directory,
		KustomizationFilePath: kustomizationFile,
	}

	components := make([]RunComponent, len(playbook.Components))
	for i, c := range playbook.Components {
		components[i] = RunComponent{Component: c, Run: run}
	}

	run.Components = components

	return run, nil

}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		} else {
			panic(err)
		}
	}

	return true
}

func (run *Run) AllComponentsApplied() bool {
	for _, c := range run.Components {
		if !c.Applied {
			return false
		}
	}

	return true
}

func (run *Run) componentApplied(name string) bool {
	for _, c := range run.Components {
		if c.Name == name {
			return c.Applied
		}
	}

	panic("We should never get here, names are pre-validated")
}

func (run *Run) Run(ctx context.Context, options *Options) error {
	run.Iteration(ctx, options)
	return nil
}

func (run *Run) Iteration(ctx context.Context, options *Options) error {

	for i := range run.Components {
		c := &run.Components[i]

		output.HeadingF("Running component '%s'", c.Name)

		// check conditions
		if len(c.ApplyConditions) > 0 {
			isff, err := c.ApplyConditions.IsFulfilled(ctx, options.KubeAccess)
			if err != nil {
				return err
			}

			if !isff {
				output.InfoF("Conditions not fulfilled, component considered applied")
				c.Applied = true
				continue
			} else {
				output.InfoF("Conditions fulfilled")
			}

		}

		// check depends
		for _, dep := range c.DependsOn {
			if !run.componentApplied(dep.Name) {
				output.InfoF("Depends on '%s', which has not been applied", c.Name, dep.Name)
				continue
			}
		}

		output.InfoF("'%s' is to be applied", c.Name)
		err := c.apply(ctx, options)
		if err != nil {
			return err
		}

		if len(c.ReadinessConditions) == 0 {
			output.InfoF("No readinessConditions defined")
		} else {
			for i := 0; i < 10; i++ {
				output.InfoF("Test readinessConditions")

				time.Sleep(time.Duration(i) * time.Second)
				isff, err := c.ReadinessConditions.IsFulfilled(ctx, options.KubeAccess)
				if err != nil {
					return err
				}

				if isff {
					break
				} else {
					continue
				}
			}

			isff, err := c.ReadinessConditions.IsFulfilled(ctx, options.KubeAccess)
			if err != nil {
				return err
			}

			if isff {
				output.InfoF("readinessConditions fulfilled")
			} else {
				return knownerror.NewKnownError("RedinessConditions are not fulfilled")
			}
		}

		c.Applied = true
	}

	return nil
}

func (c *RunComponent) apply(ctx context.Context, options *Options) error {
	manifestData, err := buildKustomization(ctx, c.Kustomization, c.Run.KustomizationFilePath)
	if err != nil {
		return err
	}

	err = applyManifest(ctx, manifestData)
	if err != nil {
		return err
	}

	return nil
}

func applyManifest(ctx context.Context, manifest []byte) error {
	f, err := os.CreateTemp("", "")
	if err != nil {
		return err
	}

	defer f.Close()
	defer os.Remove(f.Name())

	_, err = f.Write(manifest)
	if err != nil {
		return err
	}

	// execute kubectl
	cmd := exec.Command("kubectl", "apply", "-f", f.Name())
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err = cmd.Run()

	if err != nil {
		return err
	}

	return nil
}

func buildKustomization(ctx context.Context, kustomization playbook.Kustomization, kustomizationFilePath string) ([]byte, error) {

	if pathExists(kustomizationFilePath) {
		panic("ASSERT failed, path was pre-validated to not exist")
	}

	// serialize
	data, err := yaml.Marshal(kustomization)
	if err != nil {
		return nil, err
	}

	// write to Kustomization file
	err = os.WriteFile(kustomizationFilePath, data, 0666)
	if err != nil {
		return nil, err
	}

	defer os.Remove(kustomizationFilePath)

	// execute kustomize build
	cmd := exec.Command("kustomize", "build", path.Dir(kustomizationFilePath))
	cmd.Stderr = os.Stderr

	var outbuff bytes.Buffer
	cmd.Stdout = &outbuff

	err = cmd.Run()

	if err != nil {
		return nil, err
	}

	return outbuff.Bytes(), nil
}

// Options are the processed options for the caller
type Options struct {
	KubeAccess *kubeaccess.KubeAccess
	Directory  string
	Envs       map[string]string
}

func (o *Options) ValidateKnownNode(ctx context.Context, nodeName string) error {

	hasNode, err := o.KubeAccess.HasNode(ctx, nodeName)
	if err != nil {
		return err
	}

	if !hasNode {
		return knownerror.NewKnownError("The known Node %s was not found", nodeName)
	}

	return nil

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

	options := &Options{
		KubeAccess: ka,
		Directory:  flag.Arg(0),
	}

	// validate knownNode
	if knownNode != "" {
		err = options.ValidateKnownNode(ctx, knownNode)
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

	run, err := loadRun(ctx, options)
	if err != nil {
		return err
	}

	err = run.Run(ctx, options)
	if err != nil {
		return err
	}

	return nil
}

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
