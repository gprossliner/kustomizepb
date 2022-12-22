package execution

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/gprossliner/kustomizepb/knownerror"
	"github.com/gprossliner/kustomizepb/kubeaccess"
	"github.com/gprossliner/kustomizepb/playbook"
	"gopkg.in/yaml.v2"
)

const (
	PlaybookFileName = "kustomizationplaybook.yaml"

	// KustomizationFileName is the name of the generated Kustomization File
	// Is is not allowed to exist upfront
	// according to https://github.com/kubernetes-sigs/kustomize/blob/ef60d5f9bb3228ff84ad7b44b066ba76de086310/api/konfig/general.go#L10
	// this is the file the Kustomize uses if there are multiple files found
	KustomizationFileName = "kustomization.yaml"
)

type Options struct {
	KubeAccess  *kubeaccess.KubeAccess
	KubeConfig  string
	KubeContext string
	Directory   string
	Envs        map[string]string
}

type RunComponent struct {
	playbook.Component
	Run *Run

	Applied bool
	Ready   bool
}

type Run struct {
	Directory             string
	KustomizationFilePath string
	Components            []RunComponent
}

func LoadRun(ctx context.Context, options *Options) (*Run, error) {

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

func (run *Run) GetComponent(name string) *RunComponent {
	for i := range run.Components {
		c := &run.Components[i]
		if c.Name == name {
			return c
		}
	}

	return nil
}

type EventID int

const (
	EV_ComponentStarted EventID = iota
	EV_TestApplyConditions
	EV_ApplyConditionsNotFulfilled
	EV_ComponentApplying
	EV_ComponentApplyRetry
	EV_TestReadiness
	EV_ComponentReady
)

type RunEvent struct {
	ID        EventID
	Component *playbook.Component
}

func (run *Run) Run(ctx context.Context, options *Options, events chan<- RunEvent) error {

	for i := range run.Components {
		c := &run.Components[i]

		events <- RunEvent{EV_ComponentStarted, &c.Component}

		// check conditions
		if len(c.ApplyConditions) > 0 {
			events <- RunEvent{EV_TestApplyConditions, &c.Component}
			isff, err := c.ApplyConditions.IsFulfilled(ctx, options.KubeAccess)
			if err != nil {
				return err
			}

			if !isff {
				events <- RunEvent{EV_ApplyConditionsNotFulfilled, &c.Component}
				continue
			}

		}

		events <- RunEvent{EV_ComponentApplying, &c.Component}

		for rcnt := 0; ; rcnt++ {
			err := c.Apply(ctx, options)
			if err != nil {
				if rcnt == 15 {
					return err
				} else {
					events <- RunEvent{EV_ComponentApplyRetry, &c.Component}
					time.Sleep(time.Duration(rcnt) * time.Second)
				}
			} else {
				break
			}
		}

		if len(c.ReadinessConditions) == 0 {
			events <- RunEvent{EV_ComponentReady, &c.Component}
		} else {
			for i := 0; i < 40; i++ {
				events <- RunEvent{EV_TestReadiness, &c.Component}

				time.Sleep(time.Duration(i) * time.Second)
				isff, err := c.CheckReadiness(ctx, options.KubeAccess)
				if err != nil {
					return err
				}

				if isff {
					break
				} else {
					continue
				}
			}

			if c.Ready {
				events <- RunEvent{EV_ComponentReady, &c.Component}
			} else {
				return knownerror.NewKnownError("RedinessConditions are not fulfilled")
			}
		}

		c.Applied = true
	}

	return nil
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

func (c *RunComponent) CheckReadiness(ctx context.Context, ka *kubeaccess.KubeAccess) (bool, error) {
	ready, err := c.ReadinessConditions.IsFulfilled(ctx, ka)

	if err != nil {
		return false, err
	}

	c.Ready = ready
	return ready, nil
}

func (c *RunComponent) Apply(ctx context.Context, options *Options) error {
	manifestData, err := buildKustomization(ctx, c.Kustomization, c.Run.KustomizationFilePath)
	if err != nil {
		return err
	}

	err = applyManifest(ctx, manifestData, options)
	if err != nil {
		return err
	}

	c.Applied = true

	return nil
}

func applyManifest(ctx context.Context, manifest []byte, options *Options) error {
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
	args := []string{"--kubeconfig", options.KubeConfig}
	if options.KubeContext != "" {
		args = append(args, "--context", options.KubeContext)
	}
	args = append(args, "apply", "-f", f.Name())
	cmd := exec.Command("kubectl", args...)
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
