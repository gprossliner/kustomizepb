# kustomize playbook

kustomizepb is a small tool that orchestrates the execution of different
kustomizations, based on dependencies and readieness.

Furthermore it allows to perform envsubst on kustomizations.

It is designed in the same way that kustomize works.

# Installation

You can install the latest version by `go install github.com/gprossliner/kustomizepb@latest`

# Components

Like kustomize, kustomizepb is executed against a directory, which is required 
to contain a file named `kustomizationplaybook.yaml`.

kustomizepb has the notation of a "component", which is a named logical unit, 
that consists of:

* A name (must be a valid DNS subdomain name, aka kubernetes name)
* A optional list of dependencies, refering to other components
* A kustomization file, inlined into the playbook
* A optional flag `envsubst` that can be enabled to perform substitution
* A list of `readinessConditions` that are tested after the component has been applied.

kustomizepb will apply only one component at any specific time. Dependent components 
can only be applied when all dependencies have been applied, and there `readinessConditions` 
are fulfilled.

# Conditions

Currentlyy there are three different condition types implemented.

## CustomResourceDefinition

You only need to specify the name of the CRD. When it is present, the condition 
is fulfilled.

```yaml
# tests the existance of the innodbclusters.mysql.oracle.com CRD
- customResourceDefinition:
    name: innodbclusters.mysql.oracle.com
```

## Compare

Compares one value to another for equality. The operant can eighter be a `objectValue` 
from a kubernetes object, or a scalar value.

```yaml
# tests the innodb-default cluster to get ONLINE
- compare:
    value:
        objectValue:
            apiVersion: mysql.oracle.com/v2
            kind: InnoDBCluster
            namespace: innodb-default
            name: innodbclu1
            goTemplate: "{{.status.cluster.status}}"
    with:
        scalarValue: ONLINE
```

```yaml
# compares if all replicas of a statefulset are ready
- compare:
    value:
        objectValue:
            apiVersion: apps/v1
            kind: StatefulSet
            namespace: innodb-default
            name: innodbclu1
            goTemplate: "{{.status.readyReplicas}}"
    with:
        objectValue:
            apiVersion: apps/v1
            kind: StatefulSet
            namespace: innodb-default
            name: innodbclu1
            goTemplate: "{{.status.replicas}}"
```

##  ServiceReady

Tests a specific service to have endpoints. This can be used to test if a webhook
is available, and resources can be created.

This example shows how to install cert-manager, and wait for the webhook to get 
ready, so that other components can create cert-manager objects.

```yaml
- name: cert-manager-operator
  kustomization:
    resources:
    - ../../cert-manager/1-operator
    namespace: cert-manager
  readinessConditions:
  - customResourceDefinition:
      name: clusterissuers.cert-manager.io
  - serviceReady:
      name: cert-manager-webhook
      namespace: cert-manager
```

# Kustomize execution

When applying a component, these steps are performed:

1. If requested, envsub is performed on the `kustomization` contents
2. A tempoary `kustomization.yaml` file is created in the same directory as the
playbook, containing the `kustomization` of the component
3. `kustomize build` is executed against the directory to render the manifest
4. The tempoary `kustomization.yaml` is deleted
5. `kubectl apply` is executed against the given context
6. All `readinessConditions` are evaluated and once fulfilled, the component is
considered ready.

Kustomize always need to be executed against a directory, so we need to create 
real file to apply a component. Because the user expects the paths to be relative 
to the `kustomizationplaybook.yaml` file