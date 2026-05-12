# Specification: MGMT-24394 - Add Git Commit ID as vcs-ref Label to Container Images

## Overview

The CAPI Provider OpenShift Assisted container images must include a `vcs-ref` label containing the full git commit hash ID from which the image was built. This enables operators and automation tools to trace any deployed container image back to its exact source code state in the git repository, improving debugging, auditing, and compliance workflows.

## User Stories

- As a **platform operator**, I want to inspect a running container and determine the exact git commit it was built from, so that I can investigate issues, verify deployments, and ensure I'm running the expected code version.

- As a **security auditor**, I want to trace a container image back to its source commit, so that I can validate the provenance of the software and ensure compliance with security policies.

- As a **developer**, I want to quickly identify which commit corresponds to a deployed image, so that I can debug production issues by examining the exact source code that was built.

- As an **automation system**, I want to programmatically read the git commit from a container image's metadata, so that I can track deployments, generate reports, and enforce version control policies.

## Requirements

1. All CAPI Provider container images (bootstrap-provider and controlplane-provider) **MUST** include a label with the git commit hash ID.

2. The label key **MUST** be named `vcs-ref` [ASSUMPTION: using literal "vcs-ref" as the label name, though OCI standard `org.opencontainers.image.revision` may also be appropriate].

3. The git commit hash **MUST** be the full-length SHA-1 hash (40 characters), not an abbreviated version.

4. The git commit hash **MUST** represent the exact commit from which the image was built.

5. The label **MUST** be populated during the container image build process, not manually or in a separate step.

6. For CI/CD builds (Tekton pipelines), the commit hash **MUST** be obtained from the pipeline's revision parameter. Note: Tekton pipelines already provide this information; the actual implementation work is needed in the GitHub workflows that build upstream images.

7. For local developer builds, the commit hash **MUST** be obtained from the current git repository state.

8. The implementation **MUST** work across all supported build platforms (linux/x86_64, linux/ppc64le, linux/s390x, linux/arm64).

9. The existing release and version labels **MUST** remain unchanged and continue to function as they currently do.

## Acceptance Criteria

- [ ] Bootstrap provider container images include a `vcs-ref` label with the full git commit hash.

- [ ] Controlplane provider container images include a `vcs-ref` label with the full git commit hash.

- [ ] The vcs-ref label value is exactly 40 characters long (full SHA-1 hash format).

- [ ] Images built via Tekton CI/CD pipelines contain the correct commit hash matching the `{{revision}}` parameter.

- [ ] Images built locally via `make docker-build` contain the commit hash of the current HEAD.

- [ ] The vcs-ref label can be inspected using standard container tools (e.g., `podman inspect`, `docker inspect`, `skopeo inspect`).

- [ ] All existing container labels (name, version, release, vendor, etc.) remain present and unchanged.

- [ ] Documentation or comments explain how the vcs-ref label is populated.

## Non-Functional Requirements

### Backward Compatibility
- Adding the new label must not break any existing functionality or automation that depends on current image metadata.
- Existing label names and values must remain unchanged.

### Build Performance
- The addition of git commit metadata must not introduce significant build time overhead (acceptable: < 1 second additional build time).

### Reliability
- If git commit information cannot be obtained (e.g., building from a non-git context), the build should either fail with a clear error message or use a well-documented fallback value (e.g., "unknown").

## Out of Scope

- Changing the naming or format of existing labels (version, release, etc.).
- Adding additional VCS metadata beyond the commit hash (e.g., branch name, tag, dirty status).
- Implementing label standardization across other container label schemas (e.g., migrating all labels to OCI annotation standard).
- Modifying image build processes beyond what's necessary to add the vcs-ref label.
- Adding similar metadata to other artifacts (binaries, helm charts, etc.) not related to container images.
- Validation or verification of the commit hash at runtime.
- Storing commit metadata in locations other than container image labels.

## Open Questions

1. **Label naming convention**: Should we use the literal label name `vcs-ref`, or should we follow a standard like `org.label-schema.vcs-ref` (Label Schema standard) or `org.opencontainers.image.revision` (OCI standard)? [NEEDS CLARIFICATION]

2. **Fallback behavior**: What should happen if the git commit cannot be determined (e.g., shallow clone, .git directory missing)? Should the build fail, use "unknown", or use an empty string? [NEEDS CLARIFICATION]

3. **Dirty workspace handling**: Should images built from a workspace with uncommitted changes include any indication in the label (e.g., appending "-dirty"), or should the commit hash always be clean? [NEEDS CLARIFICATION]

4. **Multi-platform builds**: Does each platform-specific image need to verify it has the same vcs-ref, or is this handled automatically by the build process? [ASSUMPTION: handled automatically by build process]
