# Implementation Plan: ACM-33187

## Spec Summary

Convert ClusterImageSet resources created by CAPOA from tag-based release image references (e.g., `quay.io/openshift-release-dev/ocp-release:4.21.11-x86_64`) to digest-based references (e.g., `quay.io/openshift-release-dev/ocp-release@sha256:a272...`) to enable compatibility with ImageDigestMirrorSet (IDMS) in disconnected OpenShift environments. Digest resolution must occur on the hub cluster before ClusterImageSet creation, with proper error handling and support for the `cluster.x-k8s.io/release-image-repository-override` annotation.

## Approach

**Core Strategy**: Add digest resolution to the ClusterDeployment controller's reconciliation loop by reusing existing container registry infrastructure (`pkg/containers.RemoteImageRepository.GetDigest`). Follow the established pattern from `controlplane/internal/upgrade/upgrade.go:getReleaseImageWithDigest()` which already solves the same problem for cluster upgrades.

**Key Design Decisions**:

1. **Digest Resolution Location**: Inject digest resolution in `ClusterDeploymentReconciler.Reconcile()` between getting the tag-based release image from `getReleaseImage()` and calling `ensureClusterImageSet()`.

2. **Dependency Injection**: Add `containers.RemoteImage` interface to `ClusterDeploymentReconciler` struct to enable mocking in tests (mirrors pattern from `OpenshiftAssistedControlPlaneReconciler` and `OpenshiftUpgrader`).

3. **Pull Secret Access**: Use existing `auth.GetPullSecret()` to retrieve credentials. The OpenshiftAssistedControlPlane already has the `PullSecretRef` field required for authentication.

4. **Error Handling**: On digest resolution failure, return error from `Reconcile()` to trigger controller-runtime's exponential backoff retry. Clear error messages will include the image reference and underlying registry error.

5. **Idempotency**: The `controllerutil.CreateOrPatch` pattern in `ensureClusterImageSet()` already handles updates correctly. If a ClusterImageSet exists with a tag-based image, the next reconciliation will update it to digest-based. If already digest-based, no change occurs (idempotent).

6. **Repository Override**: No changes needed - `getReleaseImage()` already respects `cluster.x-k8s.io/release-image-repository-override` annotation and passes the overridden repository to `release.GetReleaseImage()`. The digest resolver will naturally use this overridden repository.

## Changes

### controlplane/internal/controller/clusterdeployment_controller.go

**Lines 62-65** - Add `RemoteImage` field to reconciler struct:
```go
type ClusterDeploymentReconciler struct {
    client.Client
    Scheme      *runtime.Scheme
    RemoteImage containers.RemoteImage  // NEW
}
```

**Lines 97-135** - Modify `Reconcile()` to add digest resolution:
- After line 118 (`getReleaseImage(*acp, arch)`), add digest resolution call
- Retrieve pull secret using `auth.GetPullSecret(r.Client, ctx, acp)`
- Call new helper `getReleaseImageWithDigest(releaseImage, pullSecret, r.RemoteImage)`
- Pass digest-based image to `ensureClusterImageSet()`
- Handle errors from digest resolution and pull secret retrieval

**New Function** - Add `getReleaseImageWithDigest()` helper:
- Location: After `ensureClusterImageSet()` (around line 283)
- Pattern: Clone logic from `controlplane/internal/upgrade/upgrade.go:199-222`
- Takes tag-based image string, pull secret bytes, RemoteImage interface
- Returns digest-based image string (`repo@sha256:...`) or error
- Uses `containers.PullSecretKeyChainFromString()` for auth
- Uses `RemoteImage.GetDigest()` for resolution
- Splits image on `:` to extract repository, combines with digest

### controlplane/cmd/main.go

**Existing pattern at lines ~80-100** - Initialize reconciler with RemoteImage:
- Find where `ClusterDeploymentReconciler` is instantiated
- Add `RemoteImage: containers.NewRemoteImageRepository()` to initialization
- Pattern already exists for `OpenshiftAssistedControlPlaneReconciler`

### controlplane/internal/controller/clusterdeployment_controller_test.go

**Lines 55-75** - Update test setup to inject mock RemoteImage:
- Create mock RemoteImage using existing `pkg/containers/mock_containers.go`
- Configure mock to return test digest (e.g., `sha256:abc123...`)
- Set mock on `controllerReconciler.RemoteImage` in `BeforeEach()`

**Lines 167-170** - Update ClusterImageSet assertion:
- Change expected value from tag-based (`quay.io/openshift-release-dev/ocp-release:4.16.0-multi`)
- To digest-based format (`quay.io/openshift-release-dev/ocp-release@sha256:abc123...`)
- Use the same test digest configured in mock setup

**New Test Cases** - Add around line 300:
- Test: Digest resolution failure returns error and requeues
- Test: Pull secret missing or malformed returns error
- Test: ClusterImageSet with tag-based image gets updated to digest on reconcile
- Test: Repository override annotation uses overridden repo for digest resolution
- Test: OKD release images resolve digest correctly

### docs/image_registry.md

**New Section** - Add after line 85 (after "ImageDigestMirrorSet" table):

```markdown
## Release Image Digest Resolution

CAPOA automatically resolves OpenShift release images to digest-based references when creating ClusterImageSet resources. This ensures compatibility with ImageDigestMirrorSet (IDMS) configurations in disconnected environments.

**Example**: `quay.io/openshift-release-dev/ocp-release:4.17.0-x86_64` is resolved to `quay.io/openshift-release-dev/ocp-release@sha256:a272...`

**Requirements**:
- Hub cluster must have network access to the release image registry (or its mirror)
- Pull secret must be configured in OpenshiftAssistedControlPlane
- For custom registries, use the `cluster.x-k8s.io/release-image-repository-override` annotation

**Troubleshooting**:
If ClusterDeployment reconciliation fails with digest resolution errors:
1. Verify hub cluster can reach the release image registry: `curl -I https://quay.io`
2. Check pull secret is valid: `oc get secret <pull-secret-name> -n <namespace> -o yaml`
3. For custom registries, ensure the override annotation points to an accessible registry
4. Review controller logs: `oc logs -n capoa-system deployment/capoa-controller-manager`
```

## Data Model Changes

None required. `hivev1.ClusterImageSet.Spec.ReleaseImage` is already a string field that accepts both tag-based and digest-based references.

## API Changes

None. This is an internal implementation change with no API surface modifications.

## Testing Strategy

### Unit Tests (`clusterdeployment_controller_test.go`)

1. **Happy Path**: Mock `RemoteImage.GetDigest()` to return test digest, verify ClusterImageSet created with digest-based image
   - **Acceptance Criteria**: "ensureClusterImageSet function resolves and uses digests"

2. **Digest Resolution Failure**: Mock returns error, verify Reconcile returns error (triggers requeue)
   - **Acceptance Criteria**: "Error messages clearly indicate when digest resolution fails"

3. **Pull Secret Missing**: OpenshiftAssistedControlPlane without PullSecretRef, verify error returned
   - **Acceptance Criteria**: "Error messages...provide actionable guidance"

4. **Repository Override**: Set annotation, verify mock receives overridden repository URL
   - **Acceptance Criteria**: "cluster.x-k8s.io/release-image-repository-override annotation continues to work"

5. **Update Existing Tag-Based ClusterImageSet**: Pre-create ClusterImageSet with tag, reconcile, verify updated to digest
   - **Acceptance Criteria**: "Existing ClusterImageSet resources...updated to digest-based references"

6. **OKD Support**: Test with OKD version string, verify digest resolution
   - **Acceptance Criteria**: "System MUST support...OKD release images"

### Integration Tests (e2e-tests)

Leverage existing e2e test infrastructure to verify end-to-end behavior in disconnected environment:

1. **Disconnected Deployment**: Deploy spoke cluster with IDMS configured, verify ClusterImageSet has digest and AgentClusterInstall succeeds
   - **Acceptance Criteria**: "ClusterImageSet created...contains digest-based release image reference"
   - **Acceptance Criteria**: "AgentClusterInstall...does NOT report error about tag usage"
   - **Acceptance Criteria**: "CAPI spoke cluster deployments complete successfully in disconnected environments"

### Manual Testing Checklist

1. Connected environment (quay.io accessible) - verify digest resolution succeeds
2. Disconnected with IDMS - verify spoke deployment works
3. Custom registry with override annotation - verify digest resolved from custom registry
4. Registry unreachable - verify clear error message and requeue behavior

## Risks

### Backward Compatibility

**Risk**: Existing ClusterImageSet resources created before this change have tag-based images.

**Mitigation**: The `controllerutil.CreateOrPatch` pattern in `ensureClusterImageSet()` will automatically update them to digest-based on the next reconciliation. No manual migration needed. Spec requirement 7 explicitly allows this behavior.

**Risk**: Digest-based images might break workflows that expect tags.

**Mitigation**: OpenShift's assisted-service and Hive's ClusterImageSet API both support digest-based references (verified via vendor code inspection). Spec assumption confirms AgentClusterInstall handles digests correctly.

### Digest Resolution Failures

**Risk**: Transient network issues cause digest resolution to fail, blocking cluster deployment.

**Mitigation**: Controller-runtime's built-in exponential backoff retry will automatically retry. Clear error messages (logged and in controller events) will indicate the failure reason and resolution steps.

**Risk**: Hub cluster in truly air-gapped environment cannot reach any registry.

**Mitigation**: This is explicitly out of scope per spec line 104. Users must ensure hub has registry access (possibly via mirror on hub cluster's network). Future enhancement could support pre-populated digest ConfigMap (spec open question #2).

### Performance

**Risk**: Digest resolution adds latency to reconciliation loop.

**Mitigation**: Spec assumption (line 144) accepts <30s latency. `go-containerregistry`'s `remote.Get()` typically completes in <5s for quay.io. Controller continues other work (AgentClusterInstall, etc.) in parallel. Future optimization could cache digests (spec open question #3).

### Multi-Architecture

**Risk**: Multi-arch manifest lists have different digest than architecture-specific images.

**Mitigation**: `remote.Get()` returns the manifest list digest, which is what IDMS expects. Assisted-service handles architecture selection during cluster installation. Spec open question #4 deferred to future work if needed.

### Security

**Risk**: Pull secret credentials logged or exposed in errors.

**Mitigation**: Reusing existing `containers.PullSecretKeyChain` which never logs credentials. Error messages only include registry URLs, not auth tokens. Spec requirement 88 enforced by existing infrastructure.

## Open Questions Resolved

**Q1**: Which digest resolution mechanism to use?
**A**: `github.com/google/go-containerregistry` - already a dependency, proven pattern in `upgrade.go`, no new dependencies.

**Q3**: Digest caching strategy?
**A**: No caching in initial implementation. Re-resolve on every reconcile. Future optimization if performance becomes issue (not expected per spec assumption).

**Q4**: Multi-arch digest handling?
**A**: Use manifest list digest (what `remote.Get()` returns). Assisted-service handles arch selection. Revisit if issues arise.

**Q5**: Version format validation?
**A**: No additional validation. If `release.GetReleaseImage()` produces valid URL, digest resolution proceeds. Invalid formats fail at registry API level with clear error.

**Q6**: Repository override behavior?
**A**: Assume overridden repository has same content (mirrored). Digest resolution queries the overridden repository. No special handling needed.

**Q7**: Upgrade path for existing tag-based ClusterImageSets?
**A**: Automatic update on next reconcile via `CreateOrPatch`. No validation or user action needed.
