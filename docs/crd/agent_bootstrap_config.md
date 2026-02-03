# OpenshiftAssistedConfig

The `OpenshiftAssistedConfig` resource is used to configure the bootstrap process for nodes managed by the OpenShift Assisted Installer. It is part of the Cluster API Bootstrap Provider for OpenShift Assisted.

## Spec Fields

### spec.nodeRegistration

Node registration options control how nodes are registered with the Kubernetes cluster.

#### spec.nodeRegistration.name

**Type:** `string`

**Optional:** Yes

Specifies an environment variable reference (using shell `$` syntax) from which to read the node name at runtime. When this field is set, a dedicated systemd unit (`set-hostname.service`) is added to the ignition configuration.

The environment variable must be available in `/etc/metadata_env`, which is populated by the configdrive metadata script during boot. The value is resolved safely using `grep` (no shell expansion), and then used to set the hostname via the `hostname` command.

**Example values:**
- `$METADATA_NAME` - Uses the instance name from configdrive
- `$METADATA_UUID` - Uses the instance UUID from configdrive
- `$METADATA_LOCAL_HOSTNAME` - Uses the local hostname from configdrive

**Usage Example:**

```yaml
apiVersion: bootstrap.cluster.x-k8s.io/v1alpha2
kind: OpenshiftAssistedConfig
metadata:
  name: worker-config
spec:
  nodeRegistration:
    name: "$METADATA_NAME"
```

#### spec.nodeRegistration.kubeletExtraLabels

**Type:** `[]string`

**Optional:** Yes

Extra labels to pass to kubelet during node registration. Labels can include environment variable references using `$VAR` or `${VAR}` syntax, which will be safely resolved from `/etc/metadata_env`.

**Example with static values:**

```yaml
spec:
  nodeRegistration:
    kubeletExtraLabels:
      - topology.kubernetes.io/zone=us-east-1a
      - node-role.kubernetes.io/worker=
```

**Example with dynamic values from metadata:**

```yaml
spec:
  nodeRegistration:
    kubeletExtraLabels:
      - metal3.io/uuid=$METADATA_UUID
      - my-label=${METADATA_NAME}
```

#### spec.nodeRegistration.providerID

**Type:** `string`

**Optional:** Yes

Specifies the provider ID to pass to kubelet via the `KUBELET_PROVIDERID` environment variable in `/etc/kubernetes/kubelet-env`. This can be a static value or an environment variable reference (e.g., `$METADATA_UUID`) that will be resolved from `/etc/metadata_env`.

The provider ID is used by Kubernetes to identify nodes in cloud provider integrations and is essential for proper node lifecycle management with infrastructure providers like Metal³.

**Example with static value:**

```yaml
spec:
  nodeRegistration:
    providerID: "metal3://my-node-uuid-1234"
```

**Example with dynamic value from metadata:**

```yaml
spec:
  nodeRegistration:
    providerID: "$METADATA_UUID"
```

**Example combining providerID with other nodeRegistration fields:**

```yaml
spec:
  nodeRegistration:
    name: "$METADATA_NAME"
    providerID: "metal3://$METADATA_UUID"
    kubeletExtraLabels:
      - topology.kubernetes.io/zone=us-east-1a
```

### Other Spec Fields

| Field | Type | Description |
|-------|------|-------------|
| `proxy` | `*Proxy` | Proxy settings for agents and clusters |
| `pullSecretRef` | `*LocalObjectReference` | Reference to the pull secret for pulling images |
| `additionalNTPSources` | `[]string` | NTP sources to add to cluster hosts |
| `sshAuthorizedKey` | `string` | SSH public key for debugging access |
| `nmStateConfigLabelSelector` | `LabelSelector` | Selector for NMStateConfigs |
| `cpuArchitecture` | `string` | Target CPU architecture (default: x86_64) |
| `kernelArguments` | `[]KernelArgument` | Additional kernel arguments for boot |
| `additionalTrustBundle` | `string` | PEM-encoded X.509 certificate bundle |
| `osImageVersion` | `string` | Version of OS image to use |

## Full Example

```yaml
apiVersion: bootstrap.cluster.x-k8s.io/v1alpha2
kind: OpenshiftAssistedConfig
metadata:
  name: my-worker-config
  namespace: my-cluster
spec:
  cpuArchitecture: x86_64
  sshAuthorizedKey: "ssh-rsa AAAAB3..."
  pullSecretRef:
    name: pull-secret
  nodeRegistration:
    name: "$METADATA_NAME"
    providerID: "$METADATA_UUID"
    kubeletExtraLabels:
      - "topology.kubernetes.io/zone=us-east-1a"
  additionalNTPSources:
    - "ntp.example.com"
```
