# Standalone CAPI/CAPM3 Manifests

These directories contain kustomize overlays for deploying CAPI and CAPM3 as standalone
components when MCE is enabled. This is necessary because MCE manages its own CAPI/CAPM3
installation and will remove resources it thinks it should control.

## How it works

1. The playbook generates base manifests from `clusterctl generate provider`
2. Kustomize applies transformations:
   - **namePrefix**: Adds `standalone-` to ALL resource names
   - **namespace**: Changes to `capi-standalone` or `capm3-standalone`
   - **commonLabels**: Adds identifying labels
   - **name-reference-config**: Updates all references (ClusterRoleBinding → ClusterRole, etc.)

## Result

All resources are renamed to avoid conflicts with MCE:

| Original | Renamed |
|----------|---------|
| `capi-controller-manager` | `standalone-capi-controller-manager` |
| `capi-manager-role` | `standalone-capi-manager-role` |
| `capi-system` namespace | `capi-standalone` namespace |

This ensures MCE won't recognize or remove these resources.

## Usage

Set these environment variables when running the playbook:

```bash
USE_MCE_CHART=true CAPI_STANDALONE=true make test-e2e
```

