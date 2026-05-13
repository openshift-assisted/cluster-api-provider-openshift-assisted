# Add Git Commit ID as vcs-ref Label to Container Images - Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add OCI-compliant labels to both bootstrap and controlplane provider container images to include git commit hash for traceability.

**Architecture:** Modify the Dockerfile.j2 template to accept a git commit build arg and add OCI standard labels. Regenerate the concrete Dockerfiles (Dockerfile.bootstrap-provider and Dockerfile.controlplane-provider) from the template. Update build orchestration (Makefile, GitHub workflows, Tekton pipelines) to pass the commit hash at build time.

**Tech Stack:** Docker/Podman, Jinja2 templates, Make, GitHub Actions, Tekton Pipelines

---

## Spec Summary

Add both OCI-standard labels (`org.opencontainers.image.revision`, `org.opencontainers.image.source`) and plain backward-compatible labels (`vcs-ref`, `vcs-type`) to bootstrap-provider and controlplane-provider container images. The commit hash must be obtained from the git repository state during builds, supporting three build contexts: local developer builds (via Makefile), upstream CI/CD (GitHub workflows), and downstream CI/CD (Tekton pipelines). All existing labels must remain unchanged.

## Approach

### Build Arg Strategy

Add a new build argument `git_commit` to the Dockerfile.j2 template with a default value of `unknown`. This approach ensures builds succeed even when git information is unavailable (non-git contexts, shallow clones), satisfying the reliability non-functional requirement.

The commit hash will be injected at build time via `--build-arg git_commit=<hash>` by:
- **Makefile** (`make docker-build`): Executes `git rev-parse HEAD` and passes result
- **GitHub workflows**: Uses `${{ github.sha }}` (GitHub-provided full commit hash)
- **Tekton pipelines**: Uses `{{revision}}` parameter (already contains commit hash)

### Label Design

Add four new labels in the LABEL section of Dockerfile.j2:
- `org.opencontainers.image.revision="${git_commit}"` - OCI-standard commit hash label
- `org.opencontainers.image.source="https://github.com/openshift-assisted/cluster-api-provider-openshift-assisted"` - OCI-standard repo URL
- `vcs-ref="${git_commit}"` - plain label for backward compatibility with older tooling
- `vcs-type="git"` - plain label for backward compatibility

The OCI-prefixed labels follow the official OCI image-spec annotations standard. The plain labels ensure compatibility with tools that predate the OCI standard. All labels will appear alongside existing labels without disrupting them.

### Template-Driven Generation

Since both Dockerfile.bootstrap-provider and Dockerfile.controlplane-provider are generated from Dockerfile.j2, changing the template automatically propagates to both images. After modifying the template, run `make generate-dockerfiles` to regenerate the concrete Dockerfiles that get checked into git.

## Changes

### Dockerfile.j2 Template
**File:** `Dockerfile.j2:31-34`

**What:** Add new build argument and three OCI labels

**Why:** Single source of truth for both provider Dockerfiles

**Changes:**
1. Add `ARG git_commit=unknown` after line 33 (`ARG version=latest`)
2. Add four new labels to the LABEL block (after line 54):
   - `org.opencontainers.image.revision="${git_commit}"` (OCI standard)
   - `org.opencontainers.image.source="https://github.com/openshift-assisted/cluster-api-provider-openshift-assisted"` (OCI standard)
   - `vcs-ref="${git_commit}"` (backward compatibility)
   - `vcs-type="git"` (backward compatibility)

### Makefile
**File:** `Makefile:164-166`

**What:** Update `docker-build-internal` target to extract and pass git commit hash

**Why:** Enables local developer builds to include commit metadata

**Changes:**
1. Add variable assignment before the `$(CONTAINER_TOOL) build` command:
   ```make
   GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
   ```
2. Modify the build command to include `--build-arg git_commit=$(GIT_COMMIT)`

Pattern to follow: Similar to how PLATFORMS is defined at line 178.

### GitHub Workflow: publish_images.yaml
**File:** `.github/workflows/publish_images.yaml:50-63`

**What:** Add `build-args` parameter to both docker build-push actions

**Why:** Upstream image builds need commit traceability

**Changes:**
1. Add `build-args` field to bootstrap image build step (line 50-56):
   ```yaml
   build-args: |
     release=${{ github.ref_name }}
     version=${{ env.IMAGE_TAG }}
     git_commit=${{ github.sha }}
   ```
2. Add identical `build-args` field to controlplane image build step (line 57-63)

Note: `${{ github.sha }}` is always the full 40-character commit hash in GitHub Actions.

### Tekton Pipeline: bootstrap-provider push
**File:** `.tekton/cluster-api-provider-openshift-assisted-bootstrap-mce-50-push.yaml:36-39`

**What:** Add `git_commit={{revision}}` to build-args array

**Why:** Downstream bootstrap provider builds need commit traceability

**Changes:**
Modify the `build-args` parameter at lines 36-39 to:
```yaml
- name: build-args
  value:
  - release={{target_branch}}
  - version={{revision}}
  - git_commit={{revision}}
```

### Tekton Pipeline: bootstrap-provider pull-request
**File:** `.tekton/cluster-api-provider-openshift-assisted-bootstrap-mce-50-pull-request.yaml:36-39`

**What:** Add `git_commit={{revision}}` to build-args array

**Why:** PR builds of bootstrap provider need commit traceability

**Changes:** Same as push pipeline - add `- git_commit={{revision}}` to build-args array.

### Tekton Pipeline: controlplane-provider push
**File:** `.tekton/cluster-api-provider-openshift-assisted-control-plane-mce-50-push.yaml:36-39`

**What:** Add `git_commit={{revision}}` to build-args array

**Why:** Downstream controlplane provider builds need commit traceability

**Changes:** Same as bootstrap push pipeline.

### Tekton Pipeline: controlplane-provider pull-request
**File:** `.tekton/cluster-api-provider-openshift-assisted-control-plane-mce-50-pull-request.yaml:36-39`

**What:** Add `git_commit={{revision}}` to build-args array

**Why:** PR builds of controlplane provider need commit traceability

**Changes:** Same as bootstrap push pipeline.

### Generated Dockerfiles Regeneration
**Files:** `Dockerfile.bootstrap-provider`, `Dockerfile.controlplane-provider`

**What:** Regenerate from template

**Why:** These files are checked into git and used by CI/CD, must reflect template changes

**Command:** `make generate-dockerfiles` (invokes jinja2 to render Dockerfile.j2)

## Data Model Changes

None. This change only affects container image metadata.

## API Changes

None. This change only affects container image metadata.

## Testing Strategy

### Manual Verification Tests

Since this feature adds metadata to container images without changing runtime behavior, testing focuses on verifying label presence and correctness across build contexts.

#### Test 1: Local Build Verification
**Maps to acceptance criteria:** "Images built locally via `make docker-build` contain the commit hash of the current HEAD"

**Steps:**
1. Run `make bootstrap-docker-build CONTAINER_TAG=test-local`
2. Inspect image: `podman inspect quay.io/edge-infrastructure/cluster-api-bootstrap-provider-openshift-assisted:test-local`
3. Verify `org.opencontainers.image.revision` equals `$(git rev-parse HEAD)`
4. Verify `org.opencontainers.image.source` equals the repo URL
5. Verify `vcs-ref` equals `$(git rev-parse HEAD)` (same as revision)
6. Verify `vcs-type` equals "git"
7. Repeat for controlplane: `make controlplane-docker-build CONTAINER_TAG=test-local`

#### Test 2: GitHub Actions Build Verification
**Maps to acceptance criteria:** "Images built via Tekton CI/CD pipelines contain the correct commit hash"

Note: This applies to GitHub workflow builds. Tekton builds require access to the downstream CI environment.

**Steps:**
1. Trigger publish_images.yaml workflow (push to master or create a tag)
2. After successful build, pull the image
3. Inspect labels using `skopeo inspect docker://quay.io/...`
4. Verify `org.opencontainers.image.revision` matches the commit SHA from GitHub Actions logs
5. Verify both bootstrap and controlplane images

#### Test 3: Fallback Behavior (Non-Git Context)
**Maps to acceptance criteria:** "The `org.opencontainers.image.revision` label value is... `unknown` if git information is unavailable"

**Steps:**
1. Create a temporary directory and copy only the Dockerfile and necessary source files (no .git directory)
2. Run `podman build -f Dockerfile.bootstrap-provider -t test-nogit .`
3. Inspect the image
4. Verify `org.opencontainers.image.revision` equals "unknown"

#### Test 4: Existing Labels Unchanged
**Maps to acceptance criteria:** "All existing container labels remain present and unchanged"

**Steps:**
1. Before changes: inspect existing image and save labels to file
2. After changes: build new image and inspect labels
3. Compare label sets - all original labels must still exist with same values (except name/version/release which vary by build context)
4. Verify labels like `vendor`, `url`, `io.k8s.display-name` remain unchanged

#### Test 5: Multi-Architecture Consistency
**Maps to acceptance criteria:** "The implementation must work across all supported build platforms"

This is automatically verified by the Tekton pipelines which build for linux/x86_64, linux/ppc64le, linux/s390x, linux/arm64. Manual testing of local builds on different architectures (if available) provides additional confidence.

### Automated Tests

No new Go unit tests are required since this change is purely build-time metadata injection. The `check-generated-files` make target (line 246-248 in Makefile) will catch cases where Dockerfile.j2 changes but generated Dockerfiles were not updated.

### Verification Command Reference

```bash
# Get labels from a local image
podman inspect <image> | jq '.[0].Config.Labels'

# Get labels from a remote image (without pulling)
skopeo inspect docker://<image> | jq '.Labels'

# Verify specific OCI labels
podman inspect <image> | jq '.[0].Config.Labels."org.opencontainers.image.revision"'
podman inspect <image> | jq '.[0].Config.Labels."org.opencontainers.image.source"'

# Verify plain backward-compatible labels
podman inspect <image> | jq '.[0].Config.Labels."vcs-ref"'
podman inspect <image> | jq '.[0].Config.Labels."vcs-type"'

# Get current git commit (for comparison)
git rev-parse HEAD
```

## Risks

### Risk 1: Shallow Clones
**Issue:** Some CI systems use shallow clones or detached HEADs which might not provide full git history.

**Mitigation:** The `git rev-parse HEAD` command works correctly even in shallow clones and detached HEAD states - it returns the current commit hash. The fallback to "unknown" handles edge cases like missing .git directories.

**Evidence:** GitHub Actions uses `actions/checkout@v4` which defaults to shallow clone with depth=1, but `${{ github.sha }}` is always available. Tekton pipelines explicitly pass `{{revision}}` parameter.

### Risk 2: Generated Dockerfiles Out of Sync
**Issue:** Developers might modify Dockerfile.j2 but forget to run `make generate-dockerfiles`, causing CI to use stale Dockerfiles.

**Mitigation:** The existing `check-generated-files` make target (run in CI) fails if generated files are out of sync with their sources. This already covers manifests and mocks; Dockerfiles are included.

**Action:** Ensure `check-generated-files` continues to run in CI (already present in workflows).

### Risk 3: Build-Time Performance
**Issue:** Spec requires < 1 second additional build time.

**Mitigation:** The `git rev-parse HEAD` command typically executes in < 50ms. GitHub Actions and Tekton pipelines use variables already available, adding zero overhead. Dockerfile ARG and LABEL directives are processed during image layer creation with negligible cost.

**Verification:** Time the `make docker-build` target before and after changes to confirm overhead is unmeasurable.

### Risk 4: Backward Compatibility
**Issue:** Existing automation might break if unexpected labels appear.

**Mitigation:** Adding labels to container images is inherently backward-compatible - consumers that don't look for these labels are unaffected. OCI image-spec explicitly supports arbitrary labels. Existing labels remain unchanged (name, version, release, vendor, etc.).

**Verification:** Run existing deployment workflows against new images to confirm no regressions.

### Risk 5: Hardcoded Repository URL
**Issue:** If the project is forked or moved, the `org.opencontainers.image.source` label will be incorrect.

**Mitigation:** The spec explicitly targets the openshift-assisted/cluster-api-provider-openshift-assisted repository. If the project moves, updating a single hardcoded string in Dockerfile.j2 is straightforward. For upstream GitHub builds, `${{ github.repository }}` could be used, but downstream Tekton builds would need adjustment, so hardcoding provides consistency.

**Decision:** Accept hardcoded URL as low-risk given the stable nature of the repository location.
