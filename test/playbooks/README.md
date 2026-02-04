# Test Playbooks Documentation

This directory contains Ansible playbooks for running end-to-end tests of the Cluster API Provider OpenShift Assisted project.

## Main Playbook: run_test.yaml

The `run_test.yaml` playbook is the primary test automation script that sets up a complete testing environment and runs comprehensive end-to-end tests for the Cluster API Provider OpenShift Assisted.

### Quick Start

To run the e2e tests using the Makefile:

```bash
make e2e-test
```

Note: on immutable distributions, you might need to change the default locations of caches:
```bash
ANSIBLE_HOME=/tmp/.ansible ANSIBLE_LOCAL_TEMP=/tmp/.ansible.tmp  XDG_CACHE_HOME=/tmp/.cache ANSIBLE_CACHE_PLUGIN_CONNECTION=/tmp/.ansible-cache make e2e-test
```

This command will:
1. Install required Ansible collections (`e2e-test-dependencies`)
2. Execute the playbook using the remote host inventory

### Manual Execution

You can also run the playbook manually:

```bash
# Install dependencies first
ansible-galaxy collection install -r test/ansible-requirements.yaml

# Run the playbook
ansible-playbook test/playbooks/run_test.yaml -i test/playbooks/inventories/remote_host.yaml
```

## Required Environment Variables

The following environment variables must be set before running the playbook:

### Essential Variables
- `REMOTE_HOST`: The hostname or IP address of the test runner machine
- `SSH_KEY_FILE`: Path to the SSH private key file for accessing the remote host
- `SSH_AUTHORIZED_KEY`: SSH public key content for authentication
- `PULLSECRET`: Container registry pull secret for accessing private images, base64 encoded
- `DIST_DIR`: Directory path where build artifacts are stored

### Optional Variables (with defaults)
- `NUMBER_OF_NODES`: Number of available nodes (BMHs) in the test cluster (default: "7")
- `CLUSTER_TOPOLOGY`: Cluster topology type - "multinode", "sno", "multinode-okd" or "sno-okd" (default: "multinode")
- `CAPI_VERSION`: Cluster API version to use (default: "v1.9.6")
- `CAPM3_VERSION`: Cluster API Provider Metal3 version (default: "v1.9.3")  
- `CONTAINER_TAG`: Container image tag for built images (default: "local")

### MCE Installation Variables
- `USE_MCE_CHART`: When set to `true`, installs CAPI components via MCE Helm chart instead of clusterctl (default: "false")
- `INSTALLATION_MODE`: `downstream` or `upstream` - selects which image manifest to use (default: "downstream")
- `GITHUB_TOKEN`: GitHub Personal Access Token for accessing private upstream manifest (required for upstream mode)
- `MCE_REGISTRY_OVERRIDE`: Override the image registry for downstream mode (default: "quay.io/acm-d")

### Example Environment Setup

```bash
export REMOTE_HOST="10.0.0.100"
export SSH_KEY_FILE="~/.ssh/id_rsa"
export SSH_AUTHORIZED_KEY="$(cat ~/.ssh/id_rsa.pub)"
export PULLSECRET='{"auths":{"registry.redhat.io":{"auth":"..."}}}'
export DIST_DIR="./dist"
export NUMBER_OF_NODES="3"
export CLUSTER_TOPOLOGY="multinode"
```

## Playbook Structure and Roles

The playbook executes a series of roles in sequence, each handling a specific aspect of the test environment setup and execution:

### 1. system_dependencies
**Purpose**: Install system-level dependencies and packages
- Installs EPEL repository (non-RHEL systems)
- Installs required system packages via DNF/YUM
- Enables and starts libvirtd service
- Installs Kubernetes Python package via pip

### 2. sourcecode_setup  
**Purpose**: Synchronize source code to the test environment
- Copies the entire project source code to `/tmp/capbcoa` on the test machine
- Uses rsync for efficient file transfer with archive and delete options

### 3. network_setup
**Purpose**: Configure networking for the test environment
- Sets up network interfaces and routing
- Configures networking components required for cluster communication

### 4. baremetal_emulation
**Purpose**: Set up bare metal emulation infrastructure  
- Creates virtual machines that simulate bare metal servers
- Configures libvirt domains and networks for testing

### 5. kind_setup
**Purpose**: Create and configure a Kind (Kubernetes in Docker) cluster
- Creates a Kind cluster named `capi-baremetal-provider`
- Sets up the base Kubernetes environment for testing

### 6. build_images
**Purpose**: Build container images for the providers
- Builds bootstrap and controlplane provider images
- Tags images with the specified `CONTAINER_TAG`

### 7. components_install
**Purpose**: Install Cluster API and related components
- Installs Cluster API core components
- Installs Metal3 provider components  
- Deploys cert-manager and other dependencies
- Installs the OpenShift Assisted providers

This role supports two installation modes controlled by the `USE_MCE_CHART` environment variable:

#### Standard Mode (default)
Uses `clusterctl` and kustomize to install components individually.

#### MCE Chart Mode
When `USE_MCE_CHART=true`, installs InfrastructureOperator, CAPI, CAPOA, and CAPM3 via the MCE (Multicluster Engine) Helm chart from [stolostron/mce-operator-helm-xks](https://github.com/stolostron/mce-operator-helm-xks).

See [MCE Installation](#mce-installation) section for details.

### 8. bmh_setup
**Purpose**: Set up BareMetalHost resources
- Creates BareMetalHost (BMH) custom resources
- Configures bare metal inventory for cluster nodes

### 9. cluster_install
**Purpose**: Install the OpenShift cluster
- Applies the cluster manifest (multinode or SNO based on topology)
- Initiates the cluster installation process
- Creates test namespace and applies configurations

### 10. assert_install
**Purpose**: Validate successful cluster installation
- Checks cluster status and node readiness
- Validates that all components are running correctly
- Performs post-installation verification tests

### 11. assert_upgrade (conditional)
**Purpose**: Validate cluster upgrade functionality
- Only runs when `upgrade_to_version` variable is defined
- Tests cluster upgrade scenarios
- Validates upgrade success and cluster stability

## Default Image Configuration

The playbook uses several default container images from Quay.io registries:

- **Assisted Service**: `quay.io/edge-infrastructure/assisted-service`
- **Infrastructure Operator**: `quay.io/edge-infrastructure/assisted-service`  
- **Image Service**: `quay.io/edge-infrastructure/assisted-image-service`
- **Installer Agent**: `quay.io/edge-infrastructure/assisted-installer-agent`
- **Installer Controller**: `quay.io/edge-infrastructure/assisted-installer-controller`
- **Installer**: `quay.io/edge-infrastructure/assisted-installer`

## Cluster Topologies

### Multinode (default)
- Creates a multi-node OpenShift cluster
- Uses the `multinode-example.yaml.j2` template
- Suitable for testing distributed cluster scenarios

### Single Node OpenShift (SNO)  
- Creates a single-node OpenShift cluster
- Uses the `sno-example.yaml.j2` template
- Suitable for edge computing and resource-constrained testing

To use SNO topology:
```bash
export CLUSTER_TOPOLOGY="sno"
export NUMBER_OF_NODES="1"
```

## Timing and Retry Configuration

The playbook includes configurable timing parameters for different operations:

- **Low operations**: 1 second delay, 1 retry
- **Medium operations**: 10 second delay, 60 retries  
- **High operations**: 60 second delay, 300 retries

These can be adjusted by modifying the respective variables in the playbook.

## MCE Installation

The `components_install` role supports installing CAPI components via the MCE (Multicluster Engine) Helm chart instead of the standard clusterctl approach.

### When to Use MCE Mode

- Testing with MCE-bundled components
- Validating MCE integration
- Using specific MCE image versions

### Quick Start

```bash
# Downstream mode (uses quay.io/acm-d images)
USE_MCE_CHART=true make e2e-test

# Upstream mode (requires GitHub authentication)
USE_MCE_CHART=true INSTALLATION_MODE=upstream make e2e-test
```

### Installation Modes

#### Downstream Mode (default)
- Uses manifest from `stolostron/mce-operator-bundle`
- Images are from `registry.redhat.io/multicluster-engine`
- Automatically overrides registry to `quay.io/acm-d` for accessibility

```bash
USE_MCE_CHART=true make e2e-test
```

#### Upstream Mode
- Uses manifest from `stolostron/release` (private repository)
- Images are from `quay.io/stolostron` (public)
- Requires GitHub authentication

```bash
# Option 1: Using gh CLI (recommended)
gh auth login
USE_MCE_CHART=true INSTALLATION_MODE=upstream make e2e-test

# Option 2: Using Personal Access Token
export GITHUB_TOKEN=ghp_yourPersonalAccessToken
USE_MCE_CHART=true INSTALLATION_MODE=upstream make e2e-test
```

### GitHub Authentication for Upstream Mode

The upstream image manifest is in a private GitHub repository. You need to authenticate using one of these methods:

1. **GitHub CLI (recommended)**:
   ```bash
   # Install gh CLI: https://cli.github.com/
   gh auth login
   ```
   The playbook will automatically use `gh auth token` to get your credentials.

2. **Personal Access Token**:
   - Go to https://github.com/settings/tokens
   - Generate a new token (classic) with `repo` scope
   - Set `GITHUB_TOKEN` environment variable

### Components Installed via MCE

When using MCE mode, the following components are installed from the MCE chart:
- Infrastructure Operator (assisted-service)
- Cluster API (CAPI)
- Cluster API Provider OpenShift Assisted (CAPOA)
- Cluster API Provider Metal3 (CAPM3)

The following components are still installed separately (same as standard mode):
- cert-manager
- Ironic
- Baremetal Operator (BMO)
- MetalLB
- nginx-ingress
- kube-prometheus (MCE mode only)

### Registry Override

For downstream mode, you can override the image registry:

```bash
# Use a custom registry
USE_MCE_CHART=true MCE_REGISTRY_OVERRIDE=my-registry.io/path make e2e-test

# Use original registry from manifest (no override)
USE_MCE_CHART=true MCE_REGISTRY_OVERRIDE= make e2e-test
```

### Image Overrides

You can override specific images using environment variables. This is useful for testing with custom-built images.

> **Important**: The upstream and downstream manifests use different image keys for some components. Use the correct environment variable based on your `INSTALLATION_MODE`.

#### Cluster API Image Override

| Mode | Environment Variable | Image Key in Manifest |
|------|---------------------|----------------------|
| **Upstream** | `MCE_OSE_CLUSTER_API_IMAGE` | `ose_cluster_api_rhel9` |
| **Downstream** | `MCE_CLUSTER_API_IMAGE` | `cluster_api` |

#### All Available Image Override Variables

| Environment Variable | Image Key | Description |
|---------------------|-----------|-------------|
| `MCE_CLUSTER_API_IMAGE` | cluster_api | Cluster API (downstream only) |
| `MCE_OSE_CLUSTER_API_IMAGE` | ose_cluster_api_rhel9 | OpenShift Cluster API (upstream) |
| `MCE_CAPM3_IMAGE` | ose_baremetal_cluster_api_controllers_rhel9 | CAPM3 controller |
| `MCE_CAPOA_BOOTSTRAP_IMAGE` | cluster_api_provider_openshift_assisted_bootstrap | CAPOA bootstrap provider |
| `MCE_CAPOA_CONTROLPLANE_IMAGE` | cluster_api_provider_openshift_assisted_control_plane | CAPOA control plane provider |
| `MCE_ASSISTED_SERVICE_IMAGE` | assisted_service_9 | Assisted Service |
| `MCE_ASSISTED_INSTALLER_IMAGE` | assisted_installer | Assisted Installer |
| `MCE_ASSISTED_INSTALLER_AGENT_IMAGE` | assisted_installer_agent | Assisted Installer Agent |
| `MCE_ASSISTED_INSTALLER_CONTROLLER_IMAGE` | assisted_installer_controller | Assisted Installer Controller |
| `MCE_BACKPLANE_OPERATOR_IMAGE` | backplane_operator | MCE Backplane Operator |

#### Examples

```bash
# Upstream mode - override cluster-api image
USE_MCE_CHART=true INSTALLATION_MODE=upstream \
  MCE_OSE_CLUSTER_API_IMAGE=quay.io/myrepo/cluster-api:latest \
  make e2e-test

# Downstream mode - override cluster-api image
USE_MCE_CHART=true INSTALLATION_MODE=downstream \
  MCE_CLUSTER_API_IMAGE=quay.io/myrepo/cluster-api:latest \
  make e2e-test

# Override multiple images (upstream)
USE_MCE_CHART=true INSTALLATION_MODE=upstream \
  MCE_OSE_CLUSTER_API_IMAGE=quay.io/myrepo/capi:v1 \
  MCE_CAPOA_BOOTSTRAP_IMAGE=quay.io/myrepo/capoa-bootstrap:dev \
  make e2e-test
```
