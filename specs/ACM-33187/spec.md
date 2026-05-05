# Specification: ACM-33187

**Title:** CAPI Provider: ClusterImageSet uses tag-based release image, incompatible with disconnected environments

**Status:** Draft  
**Created:** 2026-05-05

---

## Overview

The cluster-api-provider-openshift-assisted (CAPOA) controlplane controller currently creates ClusterImageSet resources with tag-based release image references (e.g., `quay.io/openshift-release-dev/ocp-release:4.21.11-x86_64`). In disconnected OpenShift environments, ImageDigestMirrorSet (IDMS) is used to redirect image pulls to local mirror registries. IDMS only applies to digest-based image references (e.g., `quay.io/openshift-release-dev/ocp-release@sha256:a272...`), not tag-based references. This incompatibility completely blocks CAPI spoke cluster deployments in disconnected environments. This change will enable CAPOA to generate digest-based release image references for ClusterImageSet resources, ensuring compatibility with IDMS in disconnected environments.

---

## User Stories

- As a **platform engineer deploying OpenShift clusters in a disconnected environment**, I want CAPOA to create ClusterImageSet resources with digest-based release images, so that IDMS can redirect image pulls to my local mirror registry and allow CAPI spoke cluster deployments to succeed.

- As a **cluster administrator using CAPI with assisted-service in air-gapped environments**, I want the release image reference to be compatible with my ImageDigestMirrorSet configuration, so that I don't have to manually patch ClusterImageSet resources or find workarounds.

- As a **QE engineer testing CAPI deployments**, I want consistent behavior between connected and disconnected environments, so that clusters deploy successfully regardless of network connectivity model when proper mirror configurations are in place.

---

## Requirements

1. ClusterImageSet resources created by the CAPOA controlplane controller MUST use digest-based release image references (format: `<registry>/<repository>@sha256:<digest>`) when a digest can be resolved for the specified version.

2. The system MUST resolve the digest for a given release version and architecture combination before creating or updating a ClusterImageSet.

3. If digest resolution fails (e.g., network issues, image not found, registry unavailable), the system MUST fail the reconciliation and requeue for retry, providing clear error messaging indicating why digest resolution failed and what actions the user can take.

4. The system MUST support digest resolution for both OCP (OpenShift Container Platform) and OKD release images.

5. The system MUST respect the `cluster.x-k8s.io/release-image-repository-override` annotation when resolving digests, using the overridden repository as the source for digest lookup.

6. The digest resolution mechanism MUST work in both connected and disconnected environments where the registry is accessible from the hub cluster.

7. The system MUST NOT overwrite a valid digest-based release image with a tag-based reference during subsequent reconciliation loops.

8. The solution MUST be compatible with existing IDMS and ImageTagMirrorSet configurations in spoke clusters.

9. The hub cluster (where CAPOA runs) MUST have network access to the source registry (or its mirror) to resolve image digests at the time of ClusterImageSet creation.

10. The system SHOULD log the resolved digest and source registry used when creating or updating ClusterImageSet resources for troubleshooting purposes.

---

## Acceptance Criteria

- A ClusterImageSet created by CAPOA in a disconnected environment contains a digest-based release image reference (e.g., `quay.io/openshift-release-dev/ocp-release@sha256:abc123...`).

- An AgentClusterInstall that references the ClusterImageSet does NOT report the error "Release image uses a tag, this is usually unsupported in combination with mirror registries" when IDMS is configured.

- CAPI spoke cluster deployments complete successfully in disconnected environments when IDMS is properly configured to mirror the release image repository.

- The `ensureClusterImageSet` function resolves and uses digests instead of tags for the `ReleaseImage` field.

- The `cluster.x-k8s.io/release-image-repository-override` annotation continues to work, with digests resolved from the overridden repository.

- Existing CAPI deployments in connected environments continue to work without regression.

- Error messages clearly indicate when digest resolution fails and provide actionable guidance.

- Documentation is updated to explain digest-based release image behavior and troubleshooting steps for digest resolution failures.

---

## Non-Functional Requirements

### Backward Compatibility

- Existing ClusterImageSet resources that were created with tag-based references MUST be updated to digest-based references during the next reconciliation if digest resolution is successful.

- The change MUST NOT break existing CAPI clusters that are already deployed and running.

### Performance

- Digest resolution MUST NOT introduce significant latency to the ClusterDeployment reconciliation loop. [ASSUMPTION: "Significant" means less than 30 seconds for digest lookup under normal network conditions.]

- Digest resolution SHOULD be cached appropriately to avoid redundant registry queries for the same version+architecture combination within a reasonable time window.

### Security

- Digest resolution MUST use the same authentication mechanisms as the rest of the CAPOA controller when accessing image registries.

- The system MUST NOT expose registry credentials or authentication tokens in logs or error messages.

### Reliability

- If digest resolution fails transiently (e.g., temporary network issue), the system SHOULD retry with exponential backoff according to the standard controller reconciliation pattern.

- The system MUST handle registry API rate limiting gracefully.

---

## Out of Scope

- Automatic mirroring of release images to local registries (this remains a prerequisite setup step for disconnected environments).

- Validation of whether the digest-based image actually exists in the mirror registry (this is handled by assisted-service during cluster installation).

- Support for offline digest resolution without hub cluster connectivity to a registry (digests must be resolvable at ClusterImageSet creation time).

- Changes to the ClusterImageSet API schema or Hive upstream project.

- Conversion of tag-based to digest-based references for release images specified in other resources outside of ClusterImageSet.

- Automated cleanup or migration of existing tag-based ClusterImageSet resources created before this change (they will be updated naturally during reconciliation).

---

## Open Questions

1. **Digest Resolution Mechanism**: Should the system use the `oc` CLI tool, the OpenShift release API, container registry API calls, or a Go library (e.g., `github.com/google/go-containerregistry`) to resolve digests? What are the trade-offs in terms of dependencies, performance, and maintainability?

2. **Future: Offline Environments**: This spec currently treats hub cluster connectivity to a registry as a prerequisite (see Out of Scope, line 104). For future iterations, should the system support truly air-gapped environments where the hub cluster cannot reach any external registry? Possible mechanisms:
   - A pre-populated ConfigMap or custom resource mapping versions to digests?
   - An annotation on OpenshiftAssistedControlPlane to explicitly specify the digest?
   - Validation that ensures a local registry mirror is accessible from the hub?

3. **Digest Caching Strategy**: Should digests be cached in-memory per controller session, persisted to a ConfigMap, or re-resolved on every reconciliation? What is the appropriate cache TTL?

4. **Multi-Architecture Support**: When resolving digests, should the system handle multi-arch manifest lists and select the architecture-specific digest, or use the manifest list digest? What is the expected behavior for the assisted-service?

5. **Version Format Ambiguity**: The current code uses `DistributionVersion` from the OpenshiftAssistedControlPlane spec (e.g., "4.21.11"). Should the system validate this format before attempting digest resolution, and what error handling is appropriate for malformed versions?

6. **Repository Override Scope**: When `cluster.x-k8s.io/release-image-repository-override` is used, should the system assume:
   - The overridden repository contains the same tags and digests as the upstream repository (mirrored)?
   - The overridden repository may have different digests for the same version (requires resolution from that specific repository)?

7. **Upgrade Path**: For existing CAPI deployments with tag-based ClusterImageSets, what is the expected behavior when they are updated to digest-based references? Should this trigger any validation or reconciliation in related resources like AgentClusterInstall?

---

## Assumptions

- **[ASSUMPTION]** The hub cluster running CAPOA has network connectivity to the source registry (or its configured mirror) at the time of ClusterImageSet creation or reconciliation.

- **[ASSUMPTION]** The assisted-service and AgentClusterInstall will correctly handle digest-based release image references with IDMS configurations.

- **[ASSUMPTION]** Release images in the source registry are immutable for a given version+architecture combination, so resolving a digest once for a specific version is safe.

- **[ASSUMPTION]** The digest resolution latency is acceptable (< 30 seconds) for inclusion in the synchronous reconciliation path.

- **[ASSUMPTION]** Users deploying in disconnected environments have already mirrored the release images to their local registry and configured IDMS appropriately on the hub cluster.

---

## Related Documentation

- [CAPOA Image Registry Configuration](../docs/image_registry.md)
- [OpenShift IDMS Documentation](https://docs.openshift.com/container-platform/latest/openshift_images/image-configuration.html#images-configuration-registry-mirror_image-configuration)
- [Hive ClusterImageSet API](https://github.com/openshift/hive/blob/master/docs/clusterimageset.md)
