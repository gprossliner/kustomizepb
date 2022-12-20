package playbook

import (
	"context"

	"github.com/gprossliner/kustomizepb/kubeaccess"
)

const (
	ApiVersion = "kustomizeplaybook.world-direct.at/v1beta1"
	Kind       = "KustomizationPlaybook"
)

type Playbook struct {
	ApiVersion    string         `yaml:"apiVersion"`
	Kind          string         `yaml:"kind"`
	Prerequisites ConditionSlice `yaml:"prerequisites"`
	Components    []Component    `yaml:"components"`
}

type ConditionSlice []Conditions

type Component struct {

	// Name is the mandatory name of the component
	Name string `yaml:"name"`

	// DependsOn is the list of component names this component depends on
	DependsOn []DependsSpec `yaml:"dependsOn"`

	// Kustomization is the kustomization definition for the component
	// If no kustomization is provided, the component is considered to be applied if the
	// ReadinessConditions are meet
	Kustomization Kustomization `yaml:"kustomization"`

	// Envsubst can be set to true to perform envsubst like substitutions from the --env-subst file
	Envsubst bool `yaml:"envsubst"`

	// ReadinessConditions specify all conditions that need to be meet so that the component is considered
	// to be ready, and dependent components will be applied
	ReadinessConditions ConditionSlice `yaml:"readinessConditions"`

	// ApplyConditions are the conditions that need to be fulfulled upfront.
	// If the conditions are not fulfulled, the component will be skipped
	ApplyConditions ConditionSlice `yaml:"applyConditions"`
}

type DependsSpec struct {
	// Name of the component that we depend on
	Name string `yaml:"name"`
}

type Conditions struct {
	Message                  string                             `yaml:"message"`
	CustomResourceDefinition *CustomResourceDefinitionCondition `yaml:"customResourceDefinition"`
	Compare                  *CompareCondition                  `yaml:"compare"`
	ServiceReady             *ServiceReadyCondition             `yaml:"serviceReady"`
}

type CompareCondition struct {
	Value CompareOperant `yaml:"value"`
	With  CompareOperant `yaml:"with"`
}

type CompareOperant struct {
	ObjectValue *ObjectValueOperant `yaml:"objectValue"`
	ScalarValue interface{}         `yaml:"scalarValue"`
}

type ServiceReadyCondition struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

type GoTemplateSpec string

type ObjectValueOperant struct {
	ApiVersion string         `yaml:"apiVersion"`
	Kind       string         `yaml:"kind"`
	Namespace  string         `yaml:"namespace"`
	Name       string         `yaml:"name"`
	GoTemplate GoTemplateSpec `yaml:"goTemplate"`
}

type Condition interface {
	IsFulfilled(ctx context.Context, ka *kubeaccess.KubeAccess) (bool, error)
}

// interface implementation assertions
var _ Condition = new(CustomResourceDefinitionCondition)
var _ Condition = new(CompareCondition)
var _ Condition = new(Conditions)
var _ Condition = new(ServiceReadyCondition)

type CustomResourceDefinitionCondition struct {
	Name string `yaml:"name"`
}

type Kustomization map[string]interface{}
