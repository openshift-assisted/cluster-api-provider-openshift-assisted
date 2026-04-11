# Application Deployment Test Role

## Purpose

This role validates end-to-end application deployment functionality on the deployed OpenShift cluster. It serves as a comprehensive integration test that:

1. Deploys a simple HTTP server (nginx) application
2. Verifies the application is accessible and functional
3. Tests networking and service discovery
4. Cleans up all deployed resources

## What It Does

### Deployment Phase
- Creates a dedicated test namespace (`test-app-validation`)
- Deploys a 2-replica nginx deployment with resource limits
- Creates a ClusterIP service to expose the application
- Configures liveness and readiness probes

### Verification Phase
- Waits for deployment to become ready
- Tests HTTP connectivity using a curl pod
- Verifies HTTP responses are valid
- Tests pod-to-pod communication
- Validates service discovery works correctly

### Cleanup Phase
- Deletes the deployment and service
- Waits for pods to terminate
- Removes the test namespace
- Verifies complete cleanup

## Application Manifest

The role deploys:
- **Deployment**: 2 replicas of nginx-unprivileged
  - Image: `docker.io/nginxinc/nginx-unprivileged:alpine`
  - Resource requests: 100m CPU, 64Mi memory
  - Resource limits: 200m CPU, 128Mi memory
  - Liveness probe on port 8080
  - Readiness probe on port 8080
  - OpenShift-compatible security context (non-root, no privilege escalation)

- **Service**: ClusterIP on port 8080
  - Routes to container port 8080
  - Selector: `app=test-http-server`

**Note**: The role uses `nginx-unprivileged` instead of standard nginx to comply with OpenShift's security constraints. Standard nginx requires root privileges to bind to port 80, which is not allowed in OpenShift's restricted security context.

## Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `test_app_namespace` | Namespace for test application | `test-app-validation` |
| `cluster_name` | Name of the cluster being tested | Required |
| `medium_retries` | Number of retries for deployment wait | `60` |
| `medium_delay` | Delay between retries (seconds) | `10` |

## Requirements

- Cluster must be fully deployed and operational
- kubectl must be available on the test runner
- Cluster kubeconfig must be available at `/tmp/{{ cluster_name }}-kubeconfig`

## Usage

This role is automatically executed after `assert_install` in the test playbook:

```yaml
- role: assert_install
  become: false
- role: app_deploy
  become: false
```

## Success Criteria

The role passes when:
1. Deployment reaches ready state (2/2 replicas)
2. HTTP requests to the service succeed
3. Response contains expected content
4. All resources are successfully cleaned up

## Failure Scenarios

The role will fail if:
- Deployment doesn't become ready within timeout
- Service is unreachable
- HTTP responses are invalid
- Cleanup fails to remove resources

## Integration

This role runs as part of the complete test suite:

```
cluster_install → assert_install → app_deploy → assert_upgrade
```

## Testing Locally

To test this role independently:

```bash
ansible-playbook test/playbooks/run_test.yaml \
  --start-at-task="Create test application namespace"
```

## Maintenance

When updating this role:
- Keep the test simple and focused on basic functionality
- Ensure cleanup always runs (use `failed_when: false` where appropriate)
- Update timeout values if cluster startup times change
- Test with both SNO and multinode topologies
