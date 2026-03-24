---
name: e2e-tests
description: Run end-to-end tests for CAPOA using Ansible playbooks against a remote bare-metal test host. Use when running e2e tests, setting up test environments, or troubleshooting e2e failures.
---

# E2E Tests

End-to-end tests run as Ansible playbooks against a remote bare-metal host. They provision a Kind management cluster, deploy CAPI + CAPOA + CAPM3, emulate bare-metal nodes via libvirt, and install an OpenShift cluster.

## Prerequisites

- **distrobox** container `capoa-dev` (see the `dev-env` skill)
- `.venv` activated inside distrobox with `ansible` installed
- SSH access (key-based) to the remote test host
- A base64-encoded pull secret for container registries

## Setup

Inside the distrobox container:

```bash
distrobox enter capoa-dev
cd <project-root>
source .venv/bin/activate
```

If `.venv` does not exist or ansible is missing, create it:

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install ansible ansible-lint jinja2-cli kubernetes
```

Install Ansible Galaxy collections (done automatically by `make e2e-test`, or manually):

```bash
ansible-galaxy collection install -r test/ansible-requirements.yaml
```

## Required Environment Variables

Set these before running:

| Variable | Description |
|---|---|
| `REMOTE_HOST` | Hostname or IP of the remote test machine |
| `SSH_KEY_FILE` | Path to SSH private key for the remote host |
| `SSH_AUTHORIZED_KEY` | SSH public key content (e.g. `$(cat ~/.ssh/id_rsa.pub)`) |
| `PULLSECRET` | Base64-encoded container registry pull secret |
| `DIST_DIR` | Directory for build artifacts (e.g. `/tmp/dist`) |
| `CONTAINER_TAG` | Image tag for built provider images (default: `local`) |

Check `~/.cursor/skills/` for a personal `capoa-local-env` skill that may contain preferred defaults for these variables.

## Running

```bash
make e2e-test
```

This installs Ansible Galaxy collections and runs:

```bash
ansible-playbook test/playbooks/run_test.yaml -i test/playbooks/inventories/remote_host.yaml
```

The full run takes **30+ minutes**.

On immutable distributions (Fedora Silverblue/CoreOS), override Ansible cache paths:

```bash
ANSIBLE_HOME=/tmp/.ansible ANSIBLE_LOCAL_TEMP=/tmp/.ansible.tmp XDG_CACHE_HOME=/tmp/.cache ANSIBLE_CACHE_PLUGIN_CONNECTION=/tmp/.ansible-cache make e2e-test
```

## Optional Environment Variables

| Variable | Default | Description |
|---|---|---|
| `CLUSTER_TOPOLOGY` | `multinode` | `multinode`, `sno`, `multinode-okd`, or `sno-okd` |
| `NUMBER_OF_NODES` | `7` | Number of emulated BMH nodes |
| `CAPI_VERSION` | `v1.11.3` | Cluster API version |
| `CAPM3_VERSION` | `v1.11.2` | CAPM3 version |
| `SKIP_BUILD` | `false` | Skip building provider images |
| `SKIP_CAPI_INSTALL` | `false` | Skip CAPI/MCE component installation |
| `SKIP_CLUSTER` | `false` | Skip cluster provisioning and assertions |
| `APPLY_EXAMPLE_ONLY` | `false` | Only render and apply the example template |
| `USE_MCE_CHART` | `false` | Install via MCE Helm chart instead of clusterctl |
| `INSTALLATION_MODE` | `downstream` | `downstream` or `upstream` (MCE mode) |
| `GITHUB_TOKEN` | -- | Required for upstream MCE mode |

## Common Patterns

### SNO (Single Node OpenShift)

```bash
export CLUSTER_TOPOLOGY="sno"
export NUMBER_OF_NODES="1"
make e2e-test
```

### Re-run without rebuilding images

```bash
SKIP_BUILD=true make e2e-test
```

### Re-apply cluster manifest only

Against an already-provisioned environment:

```bash
APPLY_EXAMPLE_ONLY=true CLUSTER_TOPOLOGY=sno make e2e-test
```

### Demo mode (infra only, no CAPI, no cluster)

```bash
SKIP_BUILD=true SKIP_CAPI_INSTALL=true SKIP_CLUSTER=true make e2e-test
```

## Playbook Structure

The playbook runs these roles in order: `system_dependencies` -> `sourcecode_setup` -> `network_setup` -> `baremetal_emulation` -> `kind_setup` -> `build_images` -> `components_install` -> `bmh_setup` -> `cluster_install` -> `assert_install` -> `assert_upgrade` (conditional). See [test/playbooks/README.md](test/playbooks/README.md) for details on each role.
