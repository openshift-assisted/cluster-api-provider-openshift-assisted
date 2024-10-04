# Cluster API OpenShift Agent providers

OpenShift Agent providers are a pair of CAPI providers, bootstrap and controlplane, which objective
is to install OpenShift on BareMetal.

## Description
OpenShift Agent providers install OpenShift on BareMetal without the need of a bootstrap node.
To achieve this feature, the providers are using Assisted Installer ZTP flow behind the scenes.

## Getting Started

### Supported Infrastructure Providers
* [CAPM3](https://github.com/metal3-io/cluster-api-provider-metal3)

### Prerequisites
- go version v1.21.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### Installation
To configure clusterctl with the OpenShift Agent providers, edit `~/.cluster-api/clusterctl.yaml` and add the following:

```yaml
  - name: "openshift-agent"
    url: "https://github.com/openshift-assisted/cluster-api-agent/releases/latest/download/bootstrap-components.yaml"
    type: "BootstrapProvider"
  - name: "openshift-agent"
    url: "https://github.com/openshift-assisted/cluster-api-agent/releases/latest/download/controlplane-components.yaml"
    type: "ControlPlaneProvider"
```

After this we will be able to initialize clusterctl:
```bash
clusterctl init --bootstrap openshift-agent --control-plane openshift-agent -i  metal3:v1.7.0
```

## Architecture Design

[Detailed architecture design](./docs/architecture_design.md)
### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/cluster-api-agent:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install-all
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/cluster-api-agent:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

## Deploy on vanilla Kubernetes
This provider is configured to deploy on [Red Hat OpenShift](https://www.redhat.com/en/technologies/cloud-computing/openshift) (OCP) by default.

To deploy this provider on vanilla Kubernetes, the following must be done:

### Prerequisites

A vanilla Kubernetes such as [Kind](https://kind.sigs.k8s.io/) must be deployed and you must be authenticated to the cluster.

In addition to the prerequisites section above, the following services are required on your cluster before installing this provider.

1. Install [Assisted-Service operator](https://github.com/openshift/assisted-service/blob/master/docs/dev/operator-on-kind.md)
2. Install [CAPI](https://cluster-api.sigs.k8s.io/user/quick-start.html)
3. Install [CAPM3](https://book.metal3.io/capm3/installation_guide)

The following CLI tools are required:

1. [kustomize](https://kubectl.docs.kubernetes.io/installation/kustomize/)


### Deploy Providers

Create kustomize files for the providers

**Bootstrap Provider**
```bash
mkdir -p bootstrap
cat <<EOF > bootstrap/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- https://github.com/openshift-assisted/cluster-api-agent/bootstrap/config/default?ref=master

patches:
- patch: |-
    - op: add
      path: "/spec/template/spec/containers/1/env/-"
      value:
        name: USE_INTERNAL_IMAGE_URL
        value: "true"
  target:
    group: apps
    version: v1
    kind: Deployment
    namespace: system
    name: controller-manager
EOF
```

_NOTE_: The `path` for the patch is fragile, ensure that the container index is correct.

The overrides listed in the [Configuration](#configuration) section below can also be patched here.

**Control Plane Provider**

```bash
mkdir -p controlplane
cat <<EOF > controlplane/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- https://github.com/openshift-assisted/cluster-api-agent/controlplane/config/default?ref=master
EOF
```

Run the following to define and create the CRs for the controllers:
```bash
kustomize build bootstrap > bootstrap_config.yaml
kustomize build controlplane > controlplane_config.yaml

kubectl apply -f bootstrap_config.yaml
kubectl apply -f controlplane_config.yaml
```

### Configuration

The following environment variables can be overridden for the bootstrap provider:

| Environment Variable | Description | Default |
|-----------------------| --------------| --------|
| `USE_INTERNAL_IMAGE_URL` | Enables the bootstrap controller to use the internal IP of assisted-image-service. The internal IP is used in place of the default URL of the live ISO provided by assisted-service to boot hosts. | `"false"`| 
| `IMAGE_SERVICE_NAME` | Name of the Service CR for assisted-image-service. This contains the internal IP of the assisted-image-service | `assisted-image-service` |
| `IMAGE_SERVICE_NAMESPACE` | Namespace that the assisted-image-service's Service CR is in | Defaults to the namespace the bootstrap provider is running in if unset|


### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following are the steps to build the installer and distribute this project to users.

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/cluster-api-agent:tag
```

NOTE: The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without
its dependencies.

2. Using the installer

Users can just run kubectl apply -f <URL for YAML BUNDLE> to install the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/cluster-api-agent/<tag or branch>/dist/install.yaml
```
## Development

### E2E testing

To run e2e tests we need to fulfill the following requirements:
* host with libvirt exposing qemu+ssh protocol. hostname and username are required (`qemu+ssh://<username>@<hostname>/system`)
  * sshd running
  * libvirtd running (ssh+qemu protocol should also be enabled)
  * podman, networkmanager required
* kubernetes controlplane running in an environment with connectivity with libvirt network. NOTE: for now controlplane is required to run on the same host as the one used for VMs. See future improvements
  * connection is required, so make sure in `kind.yaml` `networking.apiServerAddress` is set to a reachable IP address and `networking.apiServerPort` is accessible

Run `REMOTE_HOSTNAME=<hostname> REMOTE_USERNAME=<username> SSH_AUTHORIZED_KEY=<ssh public key> PULLSECRET=<base64 encoded pullsecret> KUBECONFIG=<path to kubeconfig> make test-e2e`

`REMOTE_HOSTNAME` is the host where VMs will be spun.
`REMOTE_USERNAME` is the username to connect to `REMOTE_HOSTNAME`.
`SSH_AUTHORIZED_KEY` is the key that would be configured to access the workload cluster's nodes.
`PULLSECRET` will be used to pull images that require authentication.

Note that `SSH_AUTHORIZED_KEY` expects the content of the key, for example `SSH_AUTHORIZED_KEY="$(cat $HOME/.ssh/id_rsa.pub)"`
The tests will use qemu connection to create necessary VMs, and will setup the nameserver on the same host.

#### Future improvement
* decouple where k8s controlplane is running from the host running the VMs
  * Connectivity with VM host
  * specific DNS settings will be required (maybe DNS settings can be patched to coredns?)

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

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

