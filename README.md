# Cluster API Providers OpenShift Assisted (CAPOA)

This repository contains two Cluster API (CAPI) providers—Bootstrap and Control Plane—that work together to provision OpenShift clusters using the Assisted Installer technology.

These providers are purpose-built to enable declarative, agent-based deployment of OpenShift clusters through the Cluster API ecosystem.

## Overview

The providers leverage a special Assisted Installer's flow that support provisioning nodes via ignition userData (provided by Assisted Installer) applied to standard OpenShift/OKD-compatible OS images.
This eliminates the need for a bootstrap node or custom LiveISOs.

The flow supports both OpenShift Container Platform (OCP) and OKD clusters by using prebuilt machine images:
* OKD images: [CentOS Stream CoreOS (SCOS)](https://cloud.centos.org/centos/scos/9/prod/streams/latest/x86_64/)
* OCP images: [Red Hat CoreOS (RHCOS)](https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/)

Note: The exact RHCOS image version used should match the target OpenShift version.


### Supported Infrastructure Providers

The only officially tested infrastructure provider is Metal³ (CAPM3). However, the implementation is infrastructure-agnostic.
Other CAPI infrastructure providers should work with minimal or no modification.

* [CAPM3](https://github.com/metal3-io/cluster-api-provider-metal3)

### Components

This project contains two separate providers that operate in tandem:

* Bootstrap Provider (openshiftassisted-bootstrap)
  Orchestrates the initial provisioning of the cluster nodes using Assisted Installer technology.

* Control Plane Provider (openshiftassisted-controlplane)
  Manages the OpenShift control plane lifecycle and coordinates with the bootstrap phase to finalize cluster installation.

## Getting Started

### Prerequisites
- go version v1.21.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

# Usage

## Prerequisites

1. **Cluster Environment**:
  - A bootstrap Kubernetes cluster (e.g., a `kind` instance).
  - Ensure `kubectl` is configured to interact with the cluster.
  - optional:

2. **Pull Secret (OCP only)**:
  - For OCP, ensure you have a valid pull secret. This is not required for OKD.
  - The pull secret can be retrieved from [Red Hat OpenShift Console](https://console.redhat.com/openshift/install/pull-secret).

---

## Steps to Deploy

### 1. Prepare Bootstrap Cluster

Create a bootstrap cluster using `kind` or any other method. For example:

```bash
kind create cluster --name bootstrap
```

### 2. Install `clusterctl`

`clusterctl` simplifies the process of managing providers. To install it, follow the [official documentation](https://cluster-api.sigs.k8s.io/user/quick-start#install-clusterctl).

To configure `clusterctl` with the OpenShift Agent providers, edit `~/.cluster-api/clusterctl.yaml` and add the following:

```yaml
providers:
  - name: "openshift-assisted"
    url: "https://github.com/openshift-assisted/cluster-api-provider-openshift-assisted/releases/latest/download/bootstrap-components.yaml"
    type: "BootstrapProvider"
  - name: "openshift-assisted"
    url: "https://github.com/openshift-assisted/cluster-api-provider-openshift-assisted/releases/latest/download/controlplane-components.yaml"
    type: "ControlPlaneProvider"
```

### 3. Install CAPOA Dependencies

OpenShift Assisted CAPI providers rely on the Assisted Installer running on the bootstrap cluster. To install the Assisted Installer, run the following command:

```bash
TODO: command to deploy assisted installer.
```

### 4. Install Providers

You can install all providers in one go using `clusterctl`, assuming the `clusterctl.yaml` file is configured correctly:

```bash
clusterctl init --core cluster-api:v1.9.4 --bootstrap openshift-assisted --control-plane openshift-assisted --infrastructure metal3:v1.9.2
```

Alternatively, install only the core components and infrastructure provider, then manually install the control plane and bootstrap providers:

```bash
clusterctl init --core cluster-api:v1.9.4 --bootstrap - --control-plane - --infrastructure metal3:v1.9.2
kubectl apply -f bootstrap-components.yaml
kubectl apply -f controlplane-components.yaml
```

> **Note**: This example uses CAPM3 (Cluster API Provider Metal3) as the infrastructure provider. Refer to the [CAPM3 documentation](https://book.metal3.io/capm3/installation_guide) for more details.

#### Configuration

The following environment variables can be overridden for the bootstrap provider:

| Environment Variable | Description | Default |
|-----------------------| --------------| --------|
| `USE_INTERNAL_IMAGE_URL` | Enables the bootstrap controller to use the internal IP of assisted-image-service. The internal IP is used in place of the default URL of the live ISO provided by assisted-service to boot hosts. | `"false"`| 
| `IMAGE_SERVICE_NAME` | Name of the Service CR for assisted-image-service. This contains the internal IP of the assisted-image-service | `assisted-image-service` |
| `IMAGE_SERVICE_NAMESPACE` | Namespace that the assisted-image-service's Service CR is in | Defaults to the namespace the bootstrap provider is running in if unset|


### 5. Provision Workload Cluster

Once all providers and their dependencies are installed, you can provision a new cluster.

#### Create CAPI Resources

##### Configure Cluster

The following YAML defines a `Cluster` resource. It specifies the cluster network, control plane, and infrastructure references.

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: Cluster
metadata:
  name: test-multinode-okd
  namespace: test-capi
spec:
  clusterNetwork:
    pods:
      cidrBlocks:
        - 172.18.0.0/20
    services:
      cidrBlocks:
        - 10.96.0.0/12
  controlPlaneRef:
    apiVersion: controlplane.cluster.x-k8s.io/v1alpha2
    kind: OpenshiftAssistedControlPlane
    name: test-multinode-okd
    namespace: test-capi
  infrastructureRef:
    apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
    kind: Metal3Cluster
    name: test-multinode-okd
    namespace: test-capi
```

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: Metal3Cluster
metadata:
  name: test-multinode-okd
  namespace: test-capi
spec:
  controlPlaneEndpoint:
    host: test-multinode-okd.lab.home
    port: 6443
  noCloudProvider: true
```

##### Nodes Boot Images

The following boot images are supported:

- **OKD Clusters**: Use SCOS images from [CentOS Stream](https://cloud.centos.org/centos/scos/9/prod/streams/latest/x86_64/).
- **OCP Clusters**: Use RHCOS images from [OpenShift Mirror](https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/).

##### OKD/OCP Version

Specify the version of OKD or OCP in the `distributionVersion` field of the `OpenshiftAssistedControlPlane` resource.

##### OCP Configuration

For OCP, set the `pullSecretRef` field to the name of the secret containing the pull secret. This is required in multiple locations:
- `.spec.config.pullSecretRef` and `.spec.openshiftAssistedConfigSpec.pullSecretRef` in the `OpenshiftAssistedControlPlane` resource.
- `.spec.template.spec.pullSecretRef` in any `OpenshiftAssistedConfigTemplate` attached to MachineSets.

##### Configure Control Plane Nodes

The following YAML defines the control plane configuration, including the distribution version, API VIPs, and SSH keys.

- `.spec.config.sshAuthorizedKey` can be used to access the provisioned OpenShift Nodes
- `.spec.openshiftAssistedConfigSpec.sshAuthorizedKey` can be used to access nodes in the boot (also known as discovery) phase.

For a exhaustive list of configuration options, refer to the exposed APIs:
- For controlplane specific config [OpenshiftAssistedControlPlane](./controlplane/api/v1alpha2/openshiftassistedcontrolplane_types.go) - `.spec.config`
- For bootstrap config [OpenshiftAssistedConfig](./bootstrap/api/v1alpha1/openshiftassistedconfig_types.go) - `.spec.openshiftAssistedConfig`

```yaml
apiVersion: controlplane.cluster.x-k8s.io/v1alpha2
kind: OpenshiftAssistedControlPlane
metadata:
  name: test-multinode-okd
  namespace: test-capi
  annotations: {}
spec:
  openshiftAssistedConfigSpec:
    sshAuthorizedKey: "{{ ssh_authorized_key }}"
    nodeRegistration:
      kubeletExtraLabels:
      - 'metal3.io/uuid="${METADATA_UUID}"'
  distributionVersion: 4.19.0-okd-scos.ec.6
  config:
    apiVIPs:
    - 192.168.222.40
    ingressVIPs:
    - 192.168.222.41
    baseDomain: lab.home
    pullSecretRef:
      name: "pull-secret"
    sshAuthorizedKey: "{{ ssh_authorized_key }}"
  machineTemplate:
    infrastructureRef:
      apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
      kind: Metal3MachineTemplate
      name: test-multinode-okd-controlplane
      namespace: test-capi
  replicas: 3
```

The following YAML defines the infrastructure provider configuration for controlplane nodes. For more info check the [CAPM3 official documentation](https://book.metal3.io/capm3/introduction.html)
```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: Metal3MachineTemplate
metadata:
  name: test-multinode-okd-controlplane
  namespace: test-capi
spec:
  nodeReuse: false
  template:
    spec:
      automatedCleaningMode: disabled
      dataTemplate:
        name: test-multinode-okd-controlplane-template
      image:
        checksum: https://cloud.centos.org/centos/scos/9/prod/streams/latest/x86_64/sha256sum.txt
        checksumType: sha256
        url: https://cloud.centos.org/centos/scos/9/prod/streams/latest/x86_64/scos-9.0.20250411-0-nutanix.x86_64.qcow2
        format: qcow2
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: Metal3DataTemplate
metadata:
   name: test-multinode-okd-controlplane-template
   namespace: test-capi
spec:
   clusterName: test-multinode-okd
```

#### Configure Workers Nodes

To configure worker nodes, you can use the `MachineDeployment` resource. The following YAML defines a `MachineDeployment` for worker nodes, including the bootstrap configuration and infrastructure reference.

```yaml
apiVersion: cluster.x-k8s.io/v1beta1
kind: MachineDeployment
metadata:
  name: test-multinode-okd-worker
  namespace: test-capi
  labels:
    cluster.x-k8s.io/cluster-name: test-multinode-okd
spec:
  clusterName: test-multinode-okd
  replicas: 2
  selector:
    matchLabels:
      cluster.x-k8s.io/cluster-name: test-multinode-okd
  template:
    metadata:
      labels:
        cluster.x-k8s.io/cluster-name: test-multinode-okd
    spec:
      clusterName: test-multinode-okd
      bootstrap:
        configRef:
          name: test-multinode-okd-worker
          apiVersion: bootstrap.cluster.x-k8s.io/v1alpha1
          kind: OpenshiftAssistedConfigTemplate
      infrastructureRef:
        name: test-multinode-okd-workers-2
        apiVersion: infrastructure.cluster.x-k8s.io/v1alpha3
        kind: Metal3MachineTemplate
```


The following YAML defines the bootstrap configuration for worker nodes. This is used to register the nodes with the OpenShift Assisted Installer.
Notice how we are labeling the nodes through kubelet labels: this is [required by CAPM3](https://github.com/metal3-io/cluster-api-provider-metal3/blob/main/docs/deployment_workflow.md#requirements)
The METADATA_UUID env var is being loaded on the node, and can be leveraged by this templating.

```yaml
apiVersion: bootstrap.cluster.x-k8s.io/v1alpha1
kind: OpenshiftAssistedConfigTemplate
metadata:
  name: test-multinode-okd-worker
  namespace: test-capi
  labels:
    cluster.x-k8s.io/cluster-name: test-multinode-okd
spec:
  template:
    spec:
      nodeRegistration:
        kubeletExtraLabels:
          - 'metal3.io/uuid="${METADATA_UUID}"'
      sshAuthorizedKey: "{{ ssh_authorized_key }}"
```


The following YAML defines the infrastructure provider configuration for worker nodes. For more info check the [CAPM3 official documentation](https://book.metal3.io/capm3/introduction.html)
```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: Metal3MachineTemplate
metadata:
   name: test-multinode-okd-workers-2
   namespace: test-capi
spec:
   nodeReuse: false
   template:
      spec:
         automatedCleaningMode: metadata
         dataTemplate:
            name: test-multinode-okd-workers-template
         image:
            checksum: https://cloud.centos.org/centos/scos/9/prod/streams/latest/x86_64/sha256sum.txt
            checksumType: sha256
            url: https://cloud.centos.org/centos/scos/9/prod/streams/latest/x86_64/scos-9.0.20250411-0-nutanix.x86_64.qcow2
            format: qcow2
---
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: Metal3DataTemplate
metadata:
   name: test-multinode-okd-workers-template
   namespace: test-capi
spec:
   clusterName: test-multinode-okd
```

### 6. Apply the Configuration

Save the above YAML configurations in a file named `cluster.yaml`. Ensure that you replace the placeholders (e.g., `{{ ssh_authorized_key }}`) with actual values.
Apply the configuration using `kubectl`:

```bash
kubectl apply -f cluster.yaml
```

### 7. Monitor the Cluster

You can monitor the status of the cluster and its components using `kubectl`:

```bash
kubectl get oacp -n test-capi
```

### 8. Access the Cluster

Once the cluster is up and running, you can retrieve the kubeconfig file for the new cluster using:

```bash
clusterctl -n test-capi get kubeconfig test-sno > kubeconfig
```

You can then use this kubeconfig file to access the cluster:

```bash 
export KUBECONFIG=kubeconfig
kubectl get nodes
```

# Developper guide

## Architecture Design

TODO: Outdated, need to update

[Detailed architecture design](./docs/architecture_design.md)


## Using the Makefile

The Makefile can be used through a distrobox environment to ensure all dependencies are met.

```sh
podman build -f Dockerfile.distrobox -t capoa-build .
distrobox create --image capoa-build:latest capoa-build
distrobox enter capoa-build

make <target>
```

An exception to this is the `docker-build` target, which should be executed from a system where docker/podman is available (not within distrobox).

### Building Docker Images

The project uses a templated approach to generate Dockerfiles for both bootstrap and controlplane providers from a single `Dockerfile.j2` template.

1. Generate the Dockerfiles:
```sh
make generate-dockerfiles
```
This will create:
* Dockerfile.bootstrap-provider for the bootstrap provider
* Dockerfile.controlplane-provider for the controlplane provider

2. Build the Docker images:
```sh
make docker-build-all
```

### E2E testing

E2E tests can be executed through ansible tasks in a target remote host.

#### Prerequisites

Export the following env vars:

`SSH_KEY_FILE` path to the private SSH key file to access the remote host
`SSH_AUTHORIZED_KEY` value of your public SSH key which is used to access the hosts used for deploying the workload cluster
`REMOTE_HOST` remote host name where to execute the tests: `root@<REMOTE_HOST>`
`PULLSECRET` base64-encoded pull secret to inject into the tests
`DIST_DIR` is this repository directory `/dist` i.e. `$(pwd)/dist`
`CONTAINER_TAG` is the tag of the controller images built and deployed in the testing environment. Defaults to `local` if unset.

Run the following to generate all the manifests before starting:

```sh
make generate && make manifests && make build-installer
```

#### Run the test

Then we can run:

```sh
make e2e-test
```

### Linting the tests

Run:
```sh
make ansible-lint
```

## ADRs
* [001-distribution-version](docs/adr/001-distribution-version.md)
* [002-distribution-version-upgrades](docs/adr/002-distribution-version-upgrades.md)
* [003-sentinel-file-not-implemented](docs/adr/003-sentinel-file-not-implemented.md)

## Contributing

Please, read our [CONTRIBUTING](CONTRIBUTING.md) guidelines for more info about how to create, document, and review PRs.

## License

Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

