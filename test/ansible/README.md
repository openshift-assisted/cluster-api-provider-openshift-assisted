# Additional Ansible Plays

## Deploy controllers

`deploy_controllers.yaml` allows for redeploying the OpenShift Agent Control Plane and OpenShift Agent Bootstrap controllers in an already setup test environment. Useful for testing code changes live.

### Usage

```sh
# Set a container tag that's different from the current deployed tag
export CONTAINER_TAG=

# Recreate the manifests
make generate && make manifests && make build-installer

# Run the play to deploy the updated controllers
ansible-playbook test/ansible/deploy_controllers.yaml -i test/ansible/inventory.yaml
```

## Environment Cleanup

`cleanup.yaml` will destroy the e2e environment by removing all of the hosts (VMs and network), the sushy tools podman container, and the kind cluster.

### Usage

```sh
ansible-playbook test/ansible/cleanup.yaml -i test/ansible/inventory.yaml
```

## Create more hosts
WIP
