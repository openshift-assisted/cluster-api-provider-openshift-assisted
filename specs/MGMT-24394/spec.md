# Specification: MGMT-24394 - Add Git Commit ID as vcs-ref Label to Container Images

## Overview

The CAPI Provider OpenShift Assisted container images must include a `vcs-ref` label containing the full git commit hash ID from which the image was built. This enables operators and automation tools to trace any deployed container image back to its exact source code state in the git repository, improving debugging, auditing, and compliance workflows.

## User Stories

- As a **platform operator**, I want to inspect a running container and determine the exact git commit it was built from, so that I can investigate issues, verify deployments, and ensure I'm running the expected code version.

- As a **security auditor**, I want to trace a container image back to its source commit, so that I can validate the provenance of the software and ensure compliance with security policies.

- As a **developer**, I want to quickly identify which commit corresponds to a deployed image, so that I can debug production issues by examining the exact source code that was built.

- As an **automation system**, I want to programmatically read the git commit from a container image's metadata, so that I can track deployments, generate reports, and enforce version control policies.

## Requirements

1. All CAPI Provider container images (bootstrap-provider and controlplane-provider) **MUST** include OCI standard labels for git commit information.

2. The following labels **MUST** be included in the container images:
   - `org.opencontainers.image.revision` - the full git commit hash (OCI standard)
   - `org.opencontainers.image.source` - the git repository URL (if not already present)
   - `org.opencontainers.image.vcs-type: git` - hardcoded to indicate git as the VCS (OCI extension)

3. The git commit hash **MUST** be the full-length SHA-1 hash (40 characters), not an abbreviated version.

4. The git commit hash **MUST** represent the exact commit from which the image was built.

5. The label **MUST** be populated during the container image build process, not manually or in a separate step.

6. For CI/CD builds (Tekton pipelines), the commit hash **MUST** be obtained from the pipeline's revision parameter. Note: Tekton pipelines already provide this information; the actual implementation work is needed in the GitHub workflows that build upstream images.

7. For local developer builds, the commit hash **MUST** be obtained from the current git repository state.

8. The implementation **MUST** work across all supported build platforms (linux/x86_64, linux/ppc64le, linux/s390x, linux/arm64).

9. The existing release and version labels **MUST** remain unchanged and continue to function as they currently do.

## Acceptance Criteria

- [ ] Bootstrap provider container images include `org.opencontainers.image.revision` with the full git commit hash.

- [ ] Controlplane provider container images include `org.opencontainers.image.revision` with the full git commit hash.

- [ ] Container images include `org.opencontainers.image.vcs-type: git` label.

- [ ] The `org.opencontainers.image.revision` label value is exactly 40 characters long (full SHA-1 hash format), or `unknown` if git information is unavailable.

- [ ] Images built via Tekton CI/CD pipelines contain the correct commit hash matching the `{{revision}}` parameter.

- [ ] Images built locally via `make docker-build` contain the commit hash of the current HEAD.

- [ ] The labels can be inspected using standard container tools (e.g., `podman inspect`, `docker inspect`, `skopeo inspect`).

- [ ] All existing container labels (name, version, release, vendor, etc.) remain present and unchanged.

- [ ] Documentation or comments explain how the labels are populated.

## Non-Functional Requirements

### Backward Compatibility
- Adding the new label must not break any existing functionality or automation that depends on current image metadata.
- Existing label names and values must remain unchanged.

### Build Performance
- The addition of git commit metadata must not introduce significant build time overhead (acceptable: < 1 second additional build time).

### Reliability
- If git commit information cannot be obtained (e.g., building from a non-git context, shallow clone, missing .git directory), the label value **MUST** be set to `unknown`.

## Out of Scope

- Changing the naming or format of existing labels (version, release, etc.).
- Adding additional VCS metadata beyond the commit hash (e.g., branch name, tag, dirty status).
- Implementing label standardization across other container label schemas (e.g., migrating all labels to OCI annotation standard).
- Modifying image build processes beyond what's necessary to add the vcs-ref label.
- Adding similar metadata to other artifacts (binaries, helm charts, etc.) not related to container images.
- Validation or verification of the commit hash at runtime.
- Storing commit metadata in locations other than container image labels.

## Open Questions

1. **Dirty workspace handling**: Should images built from a workspace with uncommitted changes include any indication in the label (e.g., appending "-dirty"), or should the commit hash always be clean? [NEEDS CLARIFICATION]
