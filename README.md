# Cluster API Providers OpenShift Assisted (CAPOA)

This repository contains two Cluster API (CAPI) providers—Bootstrap and Control Plane—that work together to provision OpenShift clusters using the Assisted Installer technology.

These providers are purpose-built to enable declarative, agent-based deployment of OpenShift clusters through the Cluster API ecosystem.

### Components

This project contains two separate providers that operate in tandem:

* Bootstrap Provider (openshiftassisted-bootstrap)
  Orchestrates the initial provisioning of the cluster nodes using Assisted Installer technology.

* Control Plane Provider (openshiftassisted-controlplane)
  Manages the OpenShift control plane lifecycle and coordinates with the bootstrap phase to finalize cluster installation.


## Getting Started

For more details check the [Getting Started](./docs/getting_started.md) guide.

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

