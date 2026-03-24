---
name: dev-env
description: Set up and use the distrobox-based development environment for running linter, tests, and generated file checks via Makefile targets. Use when setting up the dev environment, running make targets, building the project, or troubleshooting build issues.
---

# Development Environment

This project uses a distrobox container built from `Dockerfile.distrobox` as the development environment. All linting, testing, and code generation should be run inside it.

## Prerequisites

- `distrobox` installed on the host
- `podman` (preferred) or `docker` installed on the host

## Setup

Check whether the distrobox container already exists before building anything:

```bash
distrobox list   # look for "capoa-dev"
```

If the container already exists, just enter it:

```bash
distrobox enter capoa-dev
```

If it does not exist, build the image and create the container (run from the project root on the host):

```bash
podman build -t capoa-dev -f Dockerfile.distrobox .
distrobox create --name capoa-dev --image localhost/capoa-dev
distrobox enter capoa-dev
```

Once inside the container, the project directory is available at the same path as on the host.

## Rebuilding

Only rebuild when necessary -- e.g. after a change to `Dockerfile.distrobox`, a Go version bump in `go.mod`, or when a make target fails due to missing dependencies:

```bash
podman build -t capoa-dev -f Dockerfile.distrobox .
distrobox stop capoa-dev && distrobox rm capoa-dev
distrobox create --name capoa-dev --image localhost/capoa-dev
```

## Key Makefile Targets

Run these **inside the distrobox container**.

### Linter

```bash
make lint        # golangci-lint
make lint-fix    # golangci-lint with auto-fix
```

### Tests

```bash
make test        # unit tests (runs manifests, generate, fmt, vet, envtest)
```

### Generated Files

```bash
make check-generated-files   # verify all generated files are up-to-date (CI gate)
```

Individual generation targets:

```bash
make generate              # DeepCopy methods (controller-gen)
make manifests             # CRD, RBAC, webhook manifests (controller-gen)
make generate-mocks        # mock implementations (mockgen)
make generate-dockerfiles  # provider Dockerfiles from Jinja2 template
```

### Build

```bash
make build       # build the manager binary (runs manifests, generate, fmt, vet)
```

## How Tool Installation Works

Go tools (golangci-lint, controller-gen, setup-envtest, kustomize, mockgen) are **not** baked into the image. The Makefile installs them on first use into `./bin/` via `go install`. The first run of a target that needs a tool will be slower; subsequent runs use the cached binary.

Python tool `jinja2-cli` (used by `make generate-dockerfiles`) **is** pre-installed in a venv at `/opt/venv`, which is on PATH inside the container.

## Python .venv

A project-local `.venv` provides Python tools needed for Ansible-based workflows (e2e tests, ansible-lint). Create it inside the distrobox container if it does not exist:

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install ansible ansible-lint jinja2-cli kubernetes
```

If `.venv` already exists, just activate it:

```bash
source .venv/bin/activate
```
