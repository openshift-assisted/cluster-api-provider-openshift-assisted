# Cluster API Provider OpenShift Assisted (CAPOA)

This repository contains two [Cluster API](https://cluster-api.sigs.k8s.io/) (CAPI) providers—Bootstrap and Control Plane—that work together to provision OpenShift clusters using the [Assisted Installer](https://github.com/openshift/assisted-service) technology.

These providers enable declarative, agent-based deployment of OpenShift clusters (both OCP and OKD) on bare metal through the Cluster API ecosystem, eliminating the need for a bootstrap node or custom LiveISOs.

## Components

This project contains two separate providers that operate in tandem:

- **Bootstrap Provider** (`openshiftassisted-bootstrap`): orchestrates the initial provisioning of cluster nodes using Assisted Installer technology. It generates ignition-based userData for each machine and coordinates with the infrastructure provider to boot hosts.

- **Control Plane Provider** (`openshiftassisted-controlplane`): manages the OpenShift control plane lifecycle, creates the necessary Assisted Installer resources (ClusterDeployment, AgentClusterInstall), and coordinates with the bootstrap phase to finalize cluster installation.

### Supported Infrastructure Providers

- [Cluster API Provider Metal3 (CAPM3)](https://github.com/metal3-io/cluster-api-provider-metal3)

## Getting Started

For a complete guide on setting up a management cluster, installing providers, and provisioning workload clusters, see the [Getting Started](./docs/getting_started.md) guide.

### Quick Overview

1. Set up a Kubernetes management cluster (e.g., using [kind](https://kind.sigs.k8s.io/))
2. Install the [Infrastructure Operator](https://github.com/openshift/assisted-service) (Assisted Installer)
3. Install CAPI core, infrastructure provider (CAPM3), and CAPOA providers via `clusterctl`
4. Apply cluster manifests to provision a workload cluster

## Architecture

The [architecture design document](./docs/architecture_design.md) describes the provider flows and interactions. Key design decisions are captured in the following ADRs:

- [ADR 001 — Distribution Version](docs/adr/001-distribution-version.md): custom `distributionVersion` field to handle OpenShift versioning within CAPI
- [ADR 002 — Distribution Version Upgrades](docs/adr/002-distribution-version-upgrades.md): centralized upgrade handling via the ControlPlane resource
- [ADR 003 — Sentinel File Not Implemented](docs/adr/003-sentinel-file-not-implemented.md): rationale for not implementing the CAPI sentinel file mechanism
- [ADR 004 — Node Name from Env Var](docs/adr/004-node-name-from-env-var.md): dynamic node naming using infrastructure metadata

## API Versioning

### Bootstrap Provider

| Version | Role |
|---------|------|
| `bootstrap/api/v1alpha1/` | Spoke (converts to/from hub) |
| `bootstrap/api/v1alpha2/` | **Hub** (storage version) |

### Control Plane Provider

| Version | Role |
|---------|------|
| `controlplane/api/v1alpha2/` | Spoke (converts to/from hub) |
| `controlplane/api/v1alpha3/` | **Hub** (storage version) |

Controllers use the hub version types. Conversion logic is in `conversion.go` files under spoke API version directories.

---

## Development

### Prerequisites

All Go commands, tests, and make targets **must** be run inside a [distrobox](https://distrobox.it/) environment to ensure the correct toolchain and dependencies are available.

#### Setting Up Distrobox

```sh
podman build -f Dockerfile.distrobox -t capoa-build .
distrobox create --image capoa-build:latest capoa-build
distrobox enter capoa-build
```

Once inside distrobox, all `make` targets and `go` commands can be run directly.

> **Exception**: the `docker-build` / `docker-build-all` targets should be executed from the host where Docker/Podman is available, not from within distrobox.

### Project Structure

```
├── bootstrap/             # Bootstrap provider
│   ├── api/               # API types (v1alpha1 spoke, v1alpha2 hub)
│   ├── config/            # Kustomize manifests (CRDs, RBAC, webhooks)
│   └── internal/          # Controllers and internal packages
├── controlplane/          # Control Plane provider
│   ├── api/               # API types (v1alpha2 spoke, v1alpha3 hub)
│   ├── config/            # Kustomize manifests
│   └── internal/          # Controllers and internal packages
├── pkg/                   # Shared packages
├── assistedinstaller/     # Assisted Installer client library
├── util/                  # Utilities (logging, test helpers)
├── cluster-api-installer/ # Helm chart packaging for MCE
├── test/                  # E2E test playbooks and assets
├── examples/              # Example cluster manifests
├── docs/                  # Documentation and ADRs
└── hack/                  # Scripts and version management tooling
```

### Building

```sh
# Build the manager binary (inside distrobox)
make build

# Generate CRDs, RBAC, and webhook manifests
make manifests

# Generate DeepCopy and other code
make generate

# Run code generation and manifest generation together
make generate manifests
```

### Building Docker Images

The project uses a Jinja2-templated `Dockerfile.j2` to generate provider-specific Dockerfiles.

```sh
# Generate Dockerfiles from the template
make generate-dockerfiles

# Build both provider images (run from host, not distrobox)
make docker-build-all

# Build individually
make bootstrap-docker-build
make controlplane-docker-build

# Push images
make docker-push-all
```

Images are published to:
- `quay.io/edge-infrastructure/cluster-api-bootstrap-provider-openshift-assisted`
- `quay.io/edge-infrastructure/cluster-api-controlplane-provider-openshift-assisted`

### Running Unit Tests

```sh
# Run the full test suite (inside distrobox)
make test

# Run tests for a specific package
go test ./bootstrap/api/v1alpha2/... -v
go test ./controlplane/internal/... -v

# Run a specific test
go test ./bootstrap/internal/controller/... -v -run TestControllerName

# Enable verbose Ginkgo output
TEST_VERBOSE=true make test
```

### Linting

```sh
# Run Go linter (inside distrobox)
make lint

# Run linter with auto-fix
make lint-fix

# Lint Ansible playbooks
make ansible-lint
```

### Code Generation

After modifying API types, always regenerate:

```sh
make generate manifests
```

To verify that all generated files are up-to-date (used in CI):

```sh
make check-generated-files
```

### Generating Published Manifests

To generate the component YAML files used in releases and e2e tests:

```sh
make build-installer
make generate-published-manifests
```

This produces:
- `bootstrap-components.yaml`
- `controlplane-components.yaml`
- `dist/bootstrap_install.yaml`
- `dist/controlplane_install.yaml`

---

## E2E Testing

E2E tests are implemented as Ansible playbooks and roles that set up a complete test environment (including bare metal emulation via libvirt), deploy all dependencies, install CAPI components, provision a workload cluster, and validate the installation.

Full documentation is available at [test/playbooks/README.md](./test/playbooks/README.md).

### Prerequisites

Export the required environment variables:

```sh
export SSH_KEY_FILE=~/.ssh/id_rsa
export SSH_AUTHORIZED_KEY="$(cat ~/.ssh/id_rsa.pub)"
export REMOTE_HOST="root@<HOST>"
export PULLSECRET="$(base64 -w0 < pull-secret.json)"
export DIST_DIR="$(pwd)/dist"
export CONTAINER_TAG="local"  # optional, defaults to "local"
```

Generate the installer manifests before running:

```sh
make generate && make manifests && make build-installer
```

### Running E2E Tests

#### Standard Mode (clusterctl)

```sh
make e2e-test
```

This uses Ansible to:

1. Install system dependencies on the remote host
2. Sync source code and build provider images
3. Set up a kind cluster with networking and bare metal emulation
4. Install cert-manager, Ironic, BMO, MetalLB, nginx-ingress
5. Install CAPI, CAPM3, and CAPOA via `clusterctl`
6. Provision BMH resources and apply cluster manifests
7. Assert the cluster installs and comes up successfully

#### Cluster Topologies

```sh
# Multi-node cluster (default, 3 control plane + workers)
export CLUSTER_TOPOLOGY="multinode"

# Single Node OpenShift
export CLUSTER_TOPOLOGY="sno"
export NUMBER_OF_NODES="1"

# OKD variants
export CLUSTER_TOPOLOGY="multinode-okd"
export CLUSTER_TOPOLOGY="sno-okd"
```

#### Skipping Phases

```sh
# Skip building images (use pre-built)
SKIP_BUILD=true make e2e-test

# Skip cluster provisioning (deploy components only)
SKIP_CLUSTER=true make e2e-test

# Demo mode: infrastructure + dependencies only (manual MCE deploy)
SKIP_BUILD=true SKIP_CAPI_INSTALL=true SKIP_CLUSTER=true make e2e-test
```

#### Local Execution

E2E tests can also run locally using the `local_host` inventory:

```sh
ansible-playbook test/playbooks/run_test.yaml -i test/playbooks/inventories/local_host.yaml
```

> On immutable distributions, set cache paths accordingly:
> ```sh
> ANSIBLE_HOME=/tmp/.ansible ANSIBLE_LOCAL_TEMP=/tmp/.ansible.tmp XDG_CACHE_HOME=/tmp/.cache make e2e-test
> ```

---

## Deploying with MCE Packaging

The CAPOA providers are packaged as components of the [Multicluster Engine (MCE)](https://github.com/stolostron/mce-operator-helm-xks) Helm chart, which is used by the MultiClusterEngine operator to deploy CAPI and its providers on OpenShift.

The `cluster-api-installer/` directory contains the Helm chart definitions and packaging logic. See the [cluster-api-installer README](./cluster-api-installer/README.md) for details on chart synchronization.

### MCE E2E Testing

The E2E test framework supports deploying CAPI components via the MCE Helm chart as an alternative to the standard `clusterctl` installation path.

#### Quick Start

```sh
# Downstream mode (uses quay.io/acm-d images)
USE_MCE_CHART=true make e2e-test

# Upstream mode (uses quay.io/stolostron images, requires GitHub auth)
USE_MCE_CHART=true INSTALLATION_MODE=upstream make e2e-test
```

#### What MCE Mode Does

When `USE_MCE_CHART=true`, the following components are deployed via the MCE Helm chart instead of `clusterctl`:

| Component | Standard Mode | MCE Mode |
|-----------|--------------|----------|
| Infrastructure Operator | kustomize | MCE chart |
| Cluster API (CAPI) | clusterctl | MCE chart |
| CAPOA (bootstrap + controlplane) | kustomize | MCE chart |
| CAPM3 | clusterctl | MCE chart |

These components are still installed separately in both modes:
- cert-manager, Ironic, BMO, MetalLB, nginx-ingress
- kube-prometheus (MCE mode only, as it is an MCE dependency)

#### Installation Modes

**Downstream** (default): uses image manifests from `stolostron/mce-operator-bundle` with registry override to `quay.io/acm-d`.

```sh
USE_MCE_CHART=true make e2e-test
```

**Upstream**: uses image manifests from `stolostron/release` (private repo). Images are from `quay.io/stolostron`. Requires GitHub authentication.

```sh
# Using gh CLI (recommended)
gh auth login
USE_MCE_CHART=true INSTALLATION_MODE=upstream make e2e-test

# Using a Personal Access Token
export GITHUB_TOKEN=ghp_...
USE_MCE_CHART=true INSTALLATION_MODE=upstream make e2e-test
```

#### Standalone Components with MCE

Individual components can be installed via `clusterctl` while the rest use MCE:

```sh
# CAPI via clusterctl, everything else via MCE
USE_MCE_CHART=true CAPI_STANDALONE=true make e2e-test

# CAPM3 via clusterctl, everything else via MCE
USE_MCE_CHART=true CAPM3_STANDALONE=true make e2e-test

# Both CAPI and CAPM3 standalone
USE_MCE_CHART=true CAPI_STANDALONE=true CAPM3_STANDALONE=true make e2e-test
```

#### Image Overrides

Override specific component images for testing custom builds:

```sh
# Override the CAPOA bootstrap image
USE_MCE_CHART=true \
  MCE_CAPOA_BOOTSTRAP_IMAGE=quay.io/myrepo/capoa-bootstrap:dev \
  make e2e-test

# Override multiple images (upstream)
USE_MCE_CHART=true INSTALLATION_MODE=upstream \
  MCE_OSE_CLUSTER_API_IMAGE=quay.io/myrepo/capi:v1 \
  MCE_CAPOA_CONTROLPLANE_IMAGE=quay.io/myrepo/capoa-cp:dev \
  make e2e-test
```

See the full list of override variables in [test/playbooks/README.md](./test/playbooks/README.md#image-overrides).

#### Registry Override

For downstream mode, the image registry can be overridden:

```sh
# Use a custom registry
USE_MCE_CHART=true MCE_REGISTRY_OVERRIDE=my-registry.io/path make e2e-test
```

### Manual MCE Deployment

For manual testing or demos, use demo mode to set up infrastructure without installing CAPI components, then deploy MCE manually:

```sh
# Set up infrastructure only
SKIP_BUILD=true SKIP_CAPI_INSTALL=true SKIP_CLUSTER=true \
  ansible-playbook -i test/playbooks/inventories/local_host.yaml test/playbooks/run_test.yaml
```

Then follow the [MCE deployment guide](./cluster-api-installer/mce/README.md) to deploy MCE with Helm and provision a cluster manually.

### MCE Release Branches

For creating release branches aligned with new MCE versions, see the [MCE release branch documentation](./docs/mce_release_branch.md). The process is automated via a GitHub Actions workflow that detects the current version, creates the new `backplane-X.Y` branch, and updates all version references.

---

## Contributing

Please read our [CONTRIBUTING](CONTRIBUTING.md) guidelines for information about how to create, document, and review PRs.

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
