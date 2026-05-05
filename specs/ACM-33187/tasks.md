# Tasks: ACM-33187

## Tasks

- [ ] 1. Add digest resolution helper function to ClusterDeployment controller
  - Files: `controlplane/internal/controller/clusterdeployment_controller.go`
  - Depends on: none
  - Test: Unit test that calls `getReleaseImageWithDigest()` with tag-based image, verifies digest-based image returned
  - Details: Add new function after `ensureClusterImageSet()` (~line 283). Clone pattern from `controlplane/internal/upgrade/upgrade.go:199-222`. Signature: `func getReleaseImageWithDigest(image string, pullSecret []byte, remoteImage containers.RemoteImage) (string, error)`. Use `containers.PullSecretKeyChainFromString()` for auth, `remoteImage.GetDigest()` for resolution, split image on `:` to get repo, combine as `repo@digest`.

- [ ] 2. Add RemoteImage field to ClusterDeploymentReconciler struct
  - Files: `controlplane/internal/controller/clusterdeployment_controller.go`
  - Depends on: none
  - Test: Verify struct compiles and can be instantiated with mock
  - Details: Modify lines 62-65. Add `RemoteImage containers.RemoteImage` field after `Scheme *runtime.Scheme`. Enables dependency injection for testing (same pattern as `OpenshiftAssistedControlPlaneReconciler`).

- [ ] 3. Initialize ClusterDeploymentReconciler with RemoteImage in main.go
  - Files: `controlplane/cmd/main.go`
  - Depends on: Task 2
  - Test: Verify controller starts without error
  - Details: Find ClusterDeploymentReconciler initialization (~line 80-100). Add `RemoteImage: containers.NewRemoteImageRepository()` field. Mirror pattern from OpenshiftAssistedControlPlaneReconciler initialization.

- [ ] 4. Modify Reconcile() to resolve digest before creating ClusterImageSet
  - Files: `controlplane/internal/controller/clusterdeployment_controller.go`
  - Depends on: Task 1, Task 2
  - Test: Unit test with mocked RemoteImage, verify digest-based image passed to ensureClusterImageSet
  - Details: In `Reconcile()` function (lines 97-135), after line 118 where `getReleaseImage()` is called:
    1. Call `auth.GetPullSecret(r.Client, ctx, acp)` to get pull secret
    2. Handle error if pull secret retrieval fails (return error to requeue)
    3. Call `getReleaseImageWithDigest(releaseImage, pullSecret, r.RemoteImage)`
    4. Handle error if digest resolution fails (return error to requeue)
    5. Pass digest-based image to `ensureClusterImageSet()` instead of tag-based
    6. Log resolved digest for troubleshooting (spec requirement 10)

- [ ] 5. [P] Write unit test: happy path digest resolution
  - Files: `controlplane/internal/controller/clusterdeployment_controller_test.go`
  - Depends on: none (can start in parallel with implementation)
  - Test: Test passes when implementation complete
  - Details: In existing test suite, update `BeforeEach()` to create mock RemoteImage (use `pkg/containers/mock_containers.go`). Configure mock to return `sha256:abc123...` for any image. Inject into reconciler. Update existing test at line 167-170 to expect digest-based ClusterImageSet: `quay.io/openshift-release-dev/ocp-release@sha256:abc123...` instead of tag-based.

- [ ] 6. [P] Write unit test: digest resolution failure
  - Files: `controlplane/internal/controller/clusterdeployment_controller_test.go`
  - Depends on: none
  - Test: Test verifies Reconcile returns error when digest resolution fails
  - Details: Add new test case (~line 300). Mock RemoteImage.GetDigest() to return error. Call Reconcile, assert error returned (triggers controller-runtime requeue). Verify error message contains image reference and actionable guidance. Maps to acceptance criteria: "Error messages clearly indicate when digest resolution fails".

- [ ] 7. [P] Write unit test: pull secret missing
  - Files: `controlplane/internal/controller/clusterdeployment_controller_test.go`
  - Depends on: none
  - Test: Test verifies Reconcile returns error when pull secret not found
  - Details: Add new test case. Create OpenshiftAssistedControlPlane without PullSecretRef or with invalid secret name. Call Reconcile, assert error returned. Maps to acceptance criteria: "Error messages provide actionable guidance".

- [ ] 8. [P] Write unit test: repository override annotation
  - Files: `controlplane/internal/controller/clusterdeployment_controller_test.go`
  - Depends on: none
  - Test: Test verifies digest resolution uses overridden repository
  - Details: Add new test case. Set `cluster.x-k8s.io/release-image-repository-override` annotation on OpenshiftAssistedControlPlane. Mock RemoteImage to verify GetDigest() called with overridden repo URL. Maps to acceptance criteria: "cluster.x-k8s.io/release-image-repository-override annotation continues to work, with digests resolved from the overridden repository".

- [ ] 9. [P] Write unit test: update existing tag-based ClusterImageSet
  - Files: `controlplane/internal/controller/clusterdeployment_controller_test.go`
  - Depends on: none
  - Test: Test verifies tag-based ClusterImageSet updated to digest
  - Details: Add new test case. Pre-create ClusterImageSet with tag-based ReleaseImage. Call Reconcile. Assert ClusterImageSet updated to digest-based ReleaseImage. Maps to acceptance criteria: "Existing ClusterImageSet resources...updated to digest-based references during next reconciliation".

- [ ] 10. [P] Write unit test: OKD release image support
  - Files: `controlplane/internal/controller/clusterdeployment_controller_test.go`
  - Depends on: none
  - Test: Test verifies OKD version strings resolve correctly
  - Details: Add new test case. Use OKD version string (e.g., "4.18.0-okd-scos.ec.1"). Mock digest resolution. Verify ClusterImageSet created with `quay.io/okd/scos-release@sha256:...`. Maps to spec requirement 4: "System MUST support...OKD release images".

- [ ] 11. Update documentation with digest resolution behavior
  - Files: `docs/image_registry.md`
  - Depends on: Task 4 (so we know final behavior)
  - Test: Documentation accurately describes implementation
  - Details: Add new section after line 85 (after ImageDigestMirrorSet table). Title: "Release Image Digest Resolution". Content: Explain automatic digest resolution, requirements (hub cluster access, pull secret), troubleshooting steps (verify registry access, check pull secret, review logs). Include example of tag→digest conversion. Maps to acceptance criteria: "Documentation is updated to explain digest-based release image behavior and troubleshooting steps".

- [ ] 12. Run e2e tests to verify disconnected environment support
  - Files: N/A (uses existing e2e test infrastructure)
  - Depends on: Tasks 1-4 (implementation complete)
  - Test: E2e tests pass with IDMS configured
  - Details: Use `/e2e-tests` skill to run Ansible playbooks against bare-metal test host. Configure IDMS on spoke cluster. Verify: ClusterImageSet has digest-based image, AgentClusterInstall succeeds without "tag unsupported" error, spoke cluster deploys successfully. Maps to acceptance criteria: "CAPI spoke cluster deployments complete successfully in disconnected environments when IDMS is properly configured".

- [ ] 13. Manual testing: connected environment
  - Files: N/A
  - Depends on: Tasks 1-4
  - Test: Deploy spoke cluster in connected environment, verify no regression
  - Details: Create OpenshiftAssistedControlPlane with standard quay.io registry. Verify digest resolution succeeds, ClusterImageSet created with digest, cluster deploys. Maps to acceptance criteria: "Existing CAPI deployments in connected environments continue to work without regression".

- [ ] 14. Manual testing: custom registry with override annotation
  - Files: N/A
  - Depends on: Tasks 1-4
  - Test: Verify digest resolved from custom registry
  - Details: Set `cluster.x-k8s.io/release-image-repository-override` annotation to point to test registry. Verify digest resolution queries custom registry (check controller logs). Cluster deploys successfully. Maps to spec requirement 5.

- [ ] 15. Manual testing: registry unreachable error handling
  - Files: N/A
  - Depends on: Tasks 1-4
  - Test: Verify clear error message and retry behavior
  - Details: Configure OpenshiftAssistedControlPlane to use unreachable registry or block network access. Trigger reconciliation. Verify error message in controller logs includes registry URL and actionable guidance. Verify reconciliation requeues automatically (exponential backoff). Maps to spec requirement 3 and acceptance criteria.

## Task Ordering Notes

- **Test-First Tasks (5-10)**: Can be written in parallel before implementation tasks (1-4) are complete. Marked with `[P]` for parallelizable.
- **Implementation Dependencies**: Tasks 2-4 depend on task 1 (helper function) being defined, but can be implemented in any order after that.
- **Documentation (Task 11)**: Should wait until implementation is stable to ensure accuracy.
- **E2E Testing (Task 12)**: Requires implementation complete but can run in parallel with manual testing (13-15).
- **Manual Testing (13-15)**: Can run in parallel once implementation is complete.

## Verification Strategy

Each task includes a "Test" field describing how to verify completion in isolation. Final verification comes from:
1. All unit tests passing (`make test`)
2. E2e tests passing (`/e2e-tests` skill)
3. Manual testing checklist complete
4. Documentation review confirms accuracy
