# Getting Started

Cluster API OpenShift Assisted providers leverage a special Assisted Installer's flow that support provisioning nodes via ignition userData (provided by Assisted Installer) applied to standard OpenShift/OKD-compatible OS images.
This eliminates the need for a bootstrap node or custom LiveISOs.

The flow supports both OpenShift Container Platform (OCP) and OKD clusters by using prebuilt machine images:
* OKD images: [CentOS Stream CoreOS (SCOS)](https://cloud.centos.org/centos/scos/9/prod/streams/latest/x86_64/)
* OCP images: [Red Hat CoreOS (RHCOS)](https://mirror.openshift.com/pub/openshift-v4/x86_64/dependencies/rhcos/)

Note: The exact RHCOS image version used should match the target OpenShift version.

## Prerequisites

* [kind](https://kind.sigs.k8s.io/) or any other Kubernetes cluster to be used as bootstrap management cluster
* [kubectl](https://kubernetes.io/docs/tasks/tools/) to apply manifests to provision a workload cluster
* (optional) [clusterctl](https://cluster-api.sigs.k8s.io/user/quick-start#install-clusterctl) to simplify thhe installation of CAPI components and management of the workload clusters lifecycle


## Management Cluster

A management cluster is necessary to provision workload clusters with CAPI.
To provision one, we just need to have an available Kubernetes cluster, and install the following components:

* [Cluster API](https://cluster-api.sigs.k8s.io/)
* CAPOA providers and their dependencies
* An infrastructure provider and its dependencies. In this guide we will use [CAPM3](https://metal3.io/)

### Install components with `clusterctl`

#### Install dependencies

##### CAPOA
Ensure that the [Infrastructure Operator](https://github.com/openshift/assisted-service/blob/master/docs/dev/operator-on-kind.md) is installed. This operator will be in charge of running Assisted Installer.

##### CAPM3
Please follow [CAPM3 documentation](https://book.metal3.io/capm3/installation_guide) for installation.

#### Install CAPI providers
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

You can now install all providers in one go using `clusterctl`, assuming the `clusterctl.yaml` file is configured correctly:

```bash
clusterctl init --core cluster-api:v1.9.4 --bootstrap openshift-assisted --control-plane openshift-assisted --infrastructure metal3:v1.9.2
```

Alternatively, install only the core components and infrastructure provider, then manually install the control plane and bootstrap providers:

```bash
clusterctl init --core cluster-api:v1.9.4 --bootstrap - --control-plane - --infrastructure metal3:v1.9.2
kubectl apply -f bootstrap-components.yaml
kubectl apply -f controlplane-components.yaml
```

#### CAPOA Configuration

The following environment variables can be overridden for the bootstrap provider:

| Environment Variable | Description | Default             |
|-----------------------| --------------|---------------------|
| `USE_INTERNAL_IMAGE_URL` | Enables the bootstrap controller to use the internal IP of assisted-image-service. The internal IP is used in place of the default URL of the live ISO provided by assisted-service to boot hosts. | `"false"`           |
| `ASSISTED_SERVICE_NAME` | Name of the Assisted Installer service. It is used to generate the ignition URL when set to be retrieved internally (`USE_INTERNAL_IMAGE_URL=true`) | `assisted-service`  |
| `ASSISTED_INSTALLER_NAMESPACE` | Namespace where Assisted Installer is running .It is used to generate the ignition URL when set to be retrieved internally (`USE_INTERNAL_IMAGE_URL=true`). | *none*              |
| `ASSISTED_CA_BUNDLE_NAMESPACE` | Namespace for Assisted Installer CA bundle. | `assisted-installer` |
| `ASSISTED_CA_BUNDLE_RESOURCE` | Type of the resource holding the CA bundle (i.e. configmap/secret).| `secret`            |
| `ASSISTED_CA_BUNDLE_NAME` | Name of the resource holding the CA bundle. | `assisted-installer-ca` |
| `ASSISTED_CA_BUNDLE_KEY` | Key in the resource holding the CA bundle. | `ca.crt`|

If you are planning to use non-default values, make sure you set these values in the manifest.

## Provision a Workload Cluster

Once all providers and their dependencies are installed, you can provision a new cluster.

### Prerequisites

* DNS entries for the expected endpoints:
  - `api.<baseDomain>`: The API endpoint for the cluster.
  - `api-int.<baseDomain>`: The internal API endpoint for the cluster.
  - `*.apps.<baseDomain>`: The wildcard DNS entry for the cluster's applications.
  - '<assisted-service-dns>': The DNS entry for the Assisted Installer service (see configuration).
  - '<assisted-image-service-dns>': The DNS entry for the Assisted Installer image service (see configuration).
The DNS entries should be resolvable from both the bootstrap cluster and the hosts that will form the workload cluster.

### Configure Cluster

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

#### Configure Cluster Infrastructure

This is an example on how to configure the infrastructure provider for the cluster.

*Note that the API endpoint is composed by <clusterName>.<baseDomain>*

`clusterName` is defined in the `Cluster` resource, while `baseDomain` is defined in the `OpenshiftAssistedControlPlane` resource in the section below.

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

### Configure Control Plane Nodes

The following YAML defines the control plane configuration, including the distribution version, API VIPs, and SSH keys.

- `.spec.config.sshAuthorizedKey` can be used to access the provisioned OpenShift Nodes
- `.spec.openshiftAssistedConfigSpec.sshAuthorizedKey` can be used to access nodes in the boot (also known as discovery) phase.

For a exhaustive list of configuration options, refer to the exposed APIs:
- For controlplane specific config [OpenshiftAssistedControlPlane](./controlplane/api/v1alpha2/openshiftassistedcontrolplane_types.go) - `.spec.config`
- For bootstrap config [OpenshiftAssistedConfig](./bootstrap/api/v1alpha1/openshiftassistedconfig_types.go) - `.spec.openshiftAssistedConfig`

#### OCP Configuration

For OCP, set the `pullSecretRef` field to the name of the secret containing the pull secret. This is required in multiple locations:
- `.spec.config.pullSecretRef` and `.spec.openshiftAssistedConfigSpec.pullSecretRef` in the `OpenshiftAssistedControlPlane` resource.
- `.spec.template.spec.pullSecretRef` in any `OpenshiftAssistedConfigTemplate` attached to MachineSets.

#### OKD/OCP Version

Specify the version of OKD or OCP in the `distributionVersion` field of the `OpenshiftAssistedControlPlane` resource.
* [OKD Releases](https://origin-release.ci.openshift.org/)
* [OCP Releases](https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/latest/)

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

#### Configure Control Plane Infrastructure


The following YAML defines the infrastructure provider configuration for controlplane nodes. For more info check the [CAPM3 official documentation](https://book.metal3.io/capm3/introduction.html)

*Note: make sure clusterName is set to the same value as in the `Cluster` resource*

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

### Configure Worker Nodes

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


### Provision the Cluster

Save the above YAML configurations in a file named `cluster.yaml`. Ensure that you replace the placeholders (e.g., `{{ ssh_authorized_key }}`) with actual values.
Apply the configuration using `kubectl`:

```bash
kubectl apply -f cluster.yaml
```

#### Monitor the Cluster

You can monitor the status of the cluster and its components using `clusterctl`

```bash
clusterctl describe cluster --namespace test-capi test-multinode-okd --show-conditions=all
````

#### Access the cluster

Once the cluster is up and running, you can retrieve the kubeconfig file for the new cluster using:

```bash
clusterctl -n test-capi get kubeconfig test-multinode-okd > kubeconfig
```

You can then use this kubeconfig file to access the cluster:

```bash
export KUBECONFIG=kubeconfig
kubectl get nodes
```