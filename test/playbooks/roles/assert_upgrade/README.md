# Cluster Upgrade Test Role

## Purpose

This role validates the cluster upgrade functionality by upgrading an existing OpenShift cluster to a newer version using the Cluster API OpenshiftAssistedControlPlane resource. It ensures the upgrade process completes successfully and the cluster remains functional.

## What It Does

### Pre-Upgrade Phase
- Displays upgrade information (cluster name, target version, namespace)
- Retrieves and displays the current cluster version from the OpenshiftAssistedControlPlane resource

### Upgrade Phase
- Updates the `distributionVersion` field in the OpenshiftAssistedControlPlane spec
- This triggers the Assisted Service to begin the upgrade process on the target cluster

### Verification Phase
- Waits for the upgrade to complete by monitoring the `UpgradeCompleted` condition
- Waits for the controlplane to return to `Ready` state after upgrade
- Retrieves and displays the final cluster version
- Shows complete upgrade status information

## How OpenShift Upgrades Work

The upgrade process follows this workflow:

1. **Trigger**: The playbook updates `spec.distributionVersion` in the OpenshiftAssistedControlPlane CR
2. **Operator Action**: The cluster-api-provider-openshift-assisted operator detects the change
3. **Assisted Service**: The operator instructs Assisted Service to orchestrate the upgrade
4. **Cluster Upgrade**: The target OpenShift cluster performs a rolling upgrade of control plane and worker nodes
5. **Completion**: The `UpgradeCompleted` condition becomes `True` when all nodes are upgraded

## Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `cluster_name` | Name of the cluster to upgrade | - | Yes |
| `test_namespace` | Namespace containing the cluster resources | `test-capi` | Yes |
| `upgrade_to_version` | Target OpenShift version (e.g., "4.20.8") | - | Yes |
| `high_retries` | Number of retries for upgrade completion wait | `300` | No |
| `high_delay` | Delay between retries for upgrade wait (seconds) | `60` | No |
| `low_retries` | Number of retries for ready state wait | `1` | No |
| `medium_delay` | Delay between retries for ready wait (seconds) | `10` | No |

## Requirements

- Cluster must be fully deployed and operational
- kubectl must have access to the kind management cluster
- Kubeconfig must be available at `~/.kube/config` for the ansible_user
- The `upgrade_to_version` must be a valid OpenShift version
- Current cluster version must be compatible for upgrade to target version

## Usage

This role is conditionally executed based on the `upgrade_to_version` variable:

```yaml
- role: assert_upgrade
  when: upgrade_to_version is defined and upgrade_to_version | length > 0
```

To enable upgrade testing, set the environment variable before running the playbook:

```bash
export UPGRADE_TO_VERSION="4.20.8"
source setup_env_vars.sh && ansible-playbook -i test/playbooks/inventories/remote_host.yaml ./test/playbooks/run_test.yaml
```

## Execution Flow

1. **Display upgrade information** - Shows upgrade parameters
2. **Get current cluster version** - Retrieves current distributionVersion from OpenshiftAssistedControlPlane
3. **Display current version** - Shows the version before upgrade
4. **Upgrade cluster** - Updates the distributionVersion to trigger upgrade
5. **Wait for upgrade to complete** - Monitors UpgradeCompleted condition (up to 5 hours by default)
6. **Wait for controlplane to be ready** - Ensures cluster stabilizes after upgrade
7. **Get final cluster version** - Retrieves updated version information
8. **Display upgrade completion** - Shows final status and version

## Success Criteria

The role passes when:
1. Current version is successfully retrieved before upgrade
2. OpenshiftAssistedControlPlane resource is updated with new distributionVersion
3. `UpgradeCompleted` condition transitions to `True`
4. `Ready` condition remains `True` after upgrade
5. Final distributionVersion matches the target upgrade version

## Failure Scenarios

The role will fail if:
- Cannot connect to the kind cluster (kubeconfig issue)
- OpenshiftAssistedControlPlane resource not found
- Upgrade doesn't complete within timeout (300 retries × 60 seconds = 5 hours)
- Controlplane doesn't return to ready state after upgrade
- Upgrade process encounters errors (visible in status conditions)

## Timeout Considerations

The default timeout for upgrade completion is **5 hours** (300 retries × 60 second delay):
- Control plane upgrades: ~30-60 minutes
- Worker node upgrades: ~30-60 minutes
- Additional stabilization time: ~15-30 minutes
- Buffer for large clusters or slow networks

For faster environments, you can override the timeout:

```yaml
- role: assert_upgrade
  vars:
    high_retries: 120  # 2 hours instead of 5
```

## Integration

This role runs after successful cluster deployment and application validation:

```
cluster_install → assert_install → app_deploy → assert_upgrade
```

## Monitoring Upgrade Progress

To manually check upgrade status during execution:

```bash
# Check OpenshiftAssistedControlPlane status
kubectl get openshiftassistedcontrolplane -n test-capi test-multinode -o yaml

# Check for UpgradeCompleted condition
kubectl get openshiftassistedcontrolplane -n test-capi test-multinode \
  -o jsonpath='{.status.conditions[?(@.type=="UpgradeCompleted")]}'

# Check current vs target version
kubectl get openshiftassistedcontrolplane -n test-capi test-multinode \
  -o jsonpath='{.spec.distributionVersion}{" -> "}{.status.distributionVersion}{"\n"}'
```

## Testing Locally

To test upgrade functionality independently:

```bash
# Set the upgrade version
export UPGRADE_TO_VERSION="4.20.8"

# Run only the upgrade role
ansible-playbook test/playbooks/run_test.yaml \
  --tags assert_upgrade \
  -e upgrade_to_version=4.20.8
```

## Available OpenShift Versions

To find available upgrade versions, check the OpenShift release page:
- https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/

Common upgrade paths:
- 4.20.4 → 4.20.8 (patch upgrade)
- 4.20.x → 4.21.x (minor upgrade, when available)

## Troubleshooting

### Issue: "Invalid kube-config file. No configuration found"
**Cause**: KUBECONFIG path is incorrect or file doesn't exist
**Solution**: Verify `~/.kube/config` exists for the ansible_user and contains valid kind cluster config

### Issue: Upgrade timeout
**Cause**: Upgrade is taking longer than expected
**Solution**: Check cluster status manually, verify network connectivity, review Assisted Service logs

### Issue: UpgradeCompleted condition never becomes True
**Cause**: Upgrade encountered an error
**Solution**: Check the status.conditions in OpenshiftAssistedControlPlane for error messages

### Issue: Upgrade appears stuck
**Cause**: Nodes may be waiting for image downloads or experiencing network issues
**Solution**: Check individual node status, verify network connectivity, check image registry access

## Maintenance

When updating this role:
- Ensure timeout values accommodate larger cluster topologies
- Test with both SNO and multinode clusters
- Verify compatibility with different OpenShift version combinations
- Keep upgrade version examples current with latest releases
- Update documentation when Assisted Service upgrade behavior changes
