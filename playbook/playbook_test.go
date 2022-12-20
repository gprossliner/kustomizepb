package playbook

import (
	"testing"

	"github.com/gprossliner/kustomizepb/knownerror"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func UnmarshalKustomization(y string) Kustomization {
	k := &Kustomization{}
	err := yaml.Unmarshal([]byte(y), k)

	if err != nil {
		panic(err)
	}

	return *k
}

func TestLoadPB_Empty(t *testing.T) {
	y := `
apiVersion: kustomizeplaybook.world-direct.at/v1beta1
kind: KustomizationPlaybook
`

	pb, err := Unmarshal([]byte(y))
	assert.NoError(t, err)
	assert.NotNil(t, pb)

}

func TestLoadPB_AllFields(t *testing.T) {
	y := `
apiVersion: kustomizeplaybook.world-direct.at/v1beta1
kind: KustomizationPlaybook
prerequisites:
- customResourceDefinition:
    name: crname
components:
- name: cname
  envsubst: true
  dependsOn:
  - name: d1
  kustomization:
    resources:
    - r1
  applyConditions:
  - customResourceDefinition:
      name: crname
  readinessConditions:
  - compare:
      value:
        objectValue:
          apiVersion: apps/v1
          kind: StatefulSet
          namespace: ns
          name: n
          goTemplate: "{{.status.readyReplicas}}"
      with:
        objectValue:
          apiVersion: apps/v1
          kind: StatefulSet
          namespace: ns
          name: m
          goTemplate: "{{.status.replicas}}"
`
	pb, err := Unmarshal([]byte(y))
	assert.NoError(t, err)
	assert.NotNil(t, pb)

	assert.Len(t, pb.Prerequisites, 1)
	assert.Equal(t, "crname", pb.Prerequisites[0].CustomResourceDefinition.Name)

	assert.Len(t, pb.Components, 1)
	c := pb.Components[0]
	assert.Equal(t, c.Name, "cname")
	assert.True(t, c.Envsubst)
	assert.Len(t, c.DependsOn, 1)
	assert.Equal(t, "d1", c.DependsOn[0].Name)
	// TODO: should assert kustomization too
	assert.Len(t, c.ApplyConditions, 1)
	assert.NotNil(t, c.ApplyConditions[0].CustomResourceDefinition)
	assert.Nil(t, c.ApplyConditions[0].Compare)
	assert.Equal(t, "crname", c.ApplyConditions[0].CustomResourceDefinition.Name)

	assert.Len(t, c.ReadinessConditions, 1)
	assert.Nil(t, c.ReadinessConditions[0].CustomResourceDefinition)
	assert.NotNil(t, c.ReadinessConditions[0].Compare)
	cc := c.ReadinessConditions[0].Compare
	assert.Equal(t, "apps/v1", cc.Value.ObjectValue.ApiVersion)
	assert.Equal(t, "StatefulSet", cc.Value.ObjectValue.Kind)
	assert.Equal(t, "ns", cc.Value.ObjectValue.Namespace)
	assert.Equal(t, "n", cc.Value.ObjectValue.Name)
	assert.Equal(t, GoTemplateSpec("{{.status.readyReplicas}}"), cc.Value.ObjectValue.GoTemplate)

	assert.Equal(t, "apps/v1", cc.With.ObjectValue.ApiVersion)
	assert.Equal(t, "StatefulSet", cc.With.ObjectValue.Kind)
	assert.Equal(t, "ns", cc.With.ObjectValue.Namespace)
	assert.Equal(t, "m", cc.With.ObjectValue.Name)
	assert.Equal(t, GoTemplateSpec("{{.status.replicas}}"), cc.With.ObjectValue.GoTemplate)
}

func TestEvalGoTemplateSpec(t *testing.T) {
	obj := UnmarshalKustomization("namespace: ns1\nresources:\n- r1.yaml")

	gts := GoTemplateSpec("{{.namespace}} {{range .resources}}{{.}}{{end}}")
	res, err := gts.Evaluate(obj)
	assert.NoError(t, err)

	assert.Equal(t, "ns1 r1.yaml", res)
}

func TestEvalGoTemplateUnstructured(t *testing.T) {
	obj := unstructured.Unstructured{}
	obj.SetName("name")
	obj.SetNamespace("ns")

	gts := GoTemplateSpec("{{.metadata.namespace}} {{.metadata.name}}")
	res, err := gts.Evaluate(obj.Object)
	assert.NoError(t, err)

	assert.Equal(t, "ns name", res)
}

func assertKnownError(t *testing.T, errs []error, i int) *knownerror.KnownError {
	e := errs[i]
	assert.NotNil(t, e)

	ke, iske := e.(*knownerror.KnownError)
	assert.True(t, iske)

	return ke
}

func TestComponentValidation_EmptyName(t *testing.T) {
	y := `
apiVersion: kustomizeplaybook.world-direct.at/v1beta1
kind: KustomizationPlaybook
components:
- name: ""`
	pb, err := Unmarshal([]byte(y))
	assert.NoError(t, err)
	assert.NotNil(t, pb)

	errs := pb.Validate()
	assert.NotNil(t, errs)
	assert.Len(t, errs, 1)
	ke := assertKnownError(t, errs, 0)
	assert.Regexp(t, "\\'\\'", ke.Message) // must contain the name ''
}

func TestComponentValidation_InvalidName(t *testing.T) {
	y := `
apiVersion: kustomizeplaybook.world-direct.at/v1beta1
kind: KustomizationPlaybook
components:
- name: CAPS`
	pb, err := Unmarshal([]byte(y))
	assert.NoError(t, err)
	assert.NotNil(t, pb)

	errs := pb.Validate()
	assert.NotNil(t, errs)
	assert.Len(t, errs, 1)
	ke := assertKnownError(t, errs, 0)
	assert.Regexp(t, "\\'CAPS\\'", ke.Message) // must contain the name ''
}

func TestComponentValidation_DependencyNotFound(t *testing.T) {
	y := `
apiVersion: kustomizeplaybook.world-direct.at/v1beta1
kind: KustomizationPlaybook
components:
- name: thecomponent
  dependsOn:
  - name: notfound
`
	pb, err := Unmarshal([]byte(y))
	assert.NoError(t, err)
	assert.NotNil(t, pb)

	errs := pb.Validate()
	assert.NotNil(t, errs)
	assert.Len(t, errs, 1)
	ke := assertKnownError(t, errs, 0)
	assert.Regexp(t, "\\'notfound\\'", ke.Message)     // must contain the name 'notfound'
	assert.Regexp(t, "\\'thecomponent\\'", ke.Message) // must contain the name 'thecomponent'

}

func TestComponentValidation_DependencyBeforeComponent(t *testing.T) {
	y := `
apiVersion: kustomizeplaybook.world-direct.at/v1beta1
kind: KustomizationPlaybook
components:
- name: dependency
  dependsOn:
  - name: base
- name: base
`
	pb, err := Unmarshal([]byte(y))
	assert.NoError(t, err)
	assert.NotNil(t, pb)

	errs := pb.Validate()
	assert.NotNil(t, errs)
	assert.Len(t, errs, 1)
	ke := assertKnownError(t, errs, 0)
	assert.Regexp(t, "\\'base\\'", ke.Message)         // must contain the name 'base'
	assert.Regexp(t, "\\'dependency\\'", ke.Message)   // must contain the name 'dependency'
	assert.Regexp(t, "must not be before", ke.Message) // must contain the name 'dependency'

}
