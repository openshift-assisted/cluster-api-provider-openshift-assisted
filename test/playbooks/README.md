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
