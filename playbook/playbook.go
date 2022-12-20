package playbook

import (
	"bytes"
	"context"
	"strings"
	"text/template"

	"github.com/drone/envsubst"
	"github.com/gprossliner/kustomizepb/knownerror"
	"github.com/gprossliner/kustomizepb/kubeaccess"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/util/validation"
)

func Unmarshal(data []byte) (*Playbook, error) {
	playbook := &Playbook{}

	if err := yaml.Unmarshal(data, playbook); err != nil {
		err, _ := err.(*yaml.TypeError)
		for _, err := range err.Errors {
			return nil, knownerror.NewKnownError("Error parsing yaml: %s", err)
		}
	}

	return playbook, nil
}

func (pb *Playbook) tryFindComponent(name string) *Component {
	for _, c := range pb.Components {
		if c.Name == name {
			return &c
		}
	}

	return nil
}

func (pb *Playbook) Validate() []error {

	var errs []error

	if pb.ApiVersion != ApiVersion {
		errs = append(errs, knownerror.NewKnownError("apiVersion must be '%s', not '%s", ApiVersion, pb.ApiVersion))
	}

	if pb.Kind != Kind {
		errs = append(errs, knownerror.NewKnownError("kind must be '%s', not '%s", Kind, pb.Kind))
	}

	for _, c := range pb.Components {
		err := IsValidComponentName(c.Name)
		if err != nil {
			errs = append(errs, err)
		}

		for _, dp := range c.DependsOn {
			err := IsValidComponentName(dp.Name)
			if err != nil {
				errs = append(errs, err)
			}

			hasCp := pb.tryFindComponent(dp.Name)
			if hasCp == nil {
				errs = append(errs, knownerror.NewKnownError("Dependency '%s' of component '%s' is not defined ", dp.Name, c.Name))
			}
		}
	}

	return errs

}

func (pb *Playbook) EnvSubst(vars map[string]string) error {
	for i := range pb.Components {
		c := &pb.Components[i]
		if c.Envsubst {
			repl, err := c.Kustomization.EnvSubst(vars)
			if err != nil {
				return err
			}

			c.Kustomization = repl
		}
	}

	return nil
}

func (cs *ConditionSlice) IsFulfilled(ctx context.Context, ka *kubeaccess.KubeAccess) (bool, error) {
	for _, c := range *cs {
		ff, err := c.IsFulfilled(ctx, ka)
		if err != nil {
			return false, err
		}

		if !ff {
			return false, nil
		}
	}

	return true, nil
}

func (c *Conditions) IsFulfilled(ctx context.Context, ka *kubeaccess.KubeAccess) (bool, error) {
	if c.CustomResourceDefinition != nil {
		return c.CustomResourceDefinition.IsFulfilled(ctx, ka)
	}

	if c.Compare != nil {
		return c.Compare.IsFulfilled(ctx, ka)
	}

	if c.ServiceReady != nil {
		return c.ServiceReady.IsFulfilled(ctx, ka)
	}

	// TODO: put this in Validate Playbook too!
	return false, knownerror.NewKnownError("A condition needs to have customResourceDefinition or compare!")
}

func (src *ServiceReadyCondition) IsFulfilled(ctx context.Context, ka *kubeaccess.KubeAccess) (bool, error) {
	return ka.IsServiceReady(ctx, src.Name, src.Namespace)
}

func IsValidComponentName(name string) error {
	errs := validation.IsDNS1123Subdomain(name)
	if len(errs) > 0 {
		return knownerror.NewKnownError("Invalid component name '%s': %s", name, strings.Join(errs, "/"))
	}

	return nil
}

func (c CustomResourceDefinitionCondition) IsFulfilled(ctx context.Context, ka *kubeaccess.KubeAccess) (bool, error) {
	hasCRD, err := ka.HasCustomResourceName(ctx, c.Name)
	return hasCRD, err
}

func (c CompareCondition) IsFulfilled(ctx context.Context, ka *kubeaccess.KubeAccess) (bool, error) {
	opValue, err := c.Value.GetValue(ctx, ka)
	if err != nil {
		return false, err
	}

	opWith, err := c.With.GetValue(ctx, ka)
	if err != nil {
		return false, err
	}

	isEqual := opValue.(string) == opWith.(string)
	return isEqual, nil
}

func (op CompareOperant) GetValue(ctx context.Context, ka *kubeaccess.KubeAccess) (interface{}, error) {

	if op.ScalarValue != nil {
		return op.ScalarValue, nil
	}

	if op.ObjectValue != nil {
		return op.ObjectValue.GetValue(ctx, ka)
	}

	// TODO: include this in validation, so we should not get there
	return "", knownerror.NewKnownError("Neigher scalarValue nor objectValue defined")
}

func (ov ObjectValueOperant) GetValue(ctx context.Context, ka *kubeaccess.KubeAccess) (interface{}, error) {

	gvr, err := ka.TryGetGroupVersionResource(ov.ApiVersion, ov.Kind)
	if err != nil {
		return nil, err
	}

	if gvr == nil {
		return nil, knownerror.NewKnownError("Resource apiVersion: %s, kind: %s not found", ov.ApiVersion, ov.Kind)
	}

	obj, err := ka.GetObject(ctx, *gvr, ov.Namespace, ov.Name)
	if err != nil {
		return nil, err
	}

	res, err := ov.GoTemplate.Evaluate(obj.Object)
	if err != nil {
		return nil, err
	}

	return res, nil

}

func (gts GoTemplateSpec) Evaluate(obj interface{}) (string, error) {
	templates, err := template.New("template").Parse(string(gts))
	if err != nil {
		return "", err
	}

	var b bytes.Buffer
	err = templates.Execute(&b, obj)
	if err != nil {
		return "", err
	}

	res := b.String()
	return res, nil
}

// EnvSubst performs envsubst like substitution on all string
// values in the k
func (k Kustomization) EnvSubst(vars map[string]string) (Kustomization, error) {
	data, err := yaml.Marshal(k)
	if err != nil {
		return nil, err
	}

	res, err := envsubst.Eval(string(data), func(s string) string { return vars[s] })
	if err != nil {
		return nil, err
	}

	newk := &Kustomization{}
	err = yaml.Unmarshal([]byte(res), newk)
	if err != nil {
		return nil, err
	}

	return *newk, nil
}
