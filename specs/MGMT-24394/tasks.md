# MGMT-24394 Implementation Tasks

## Task Ordering Notes
- Tasks 1-4: Core changes (sequential - template drives generated files)
- Tasks 5-8: Build orchestration updates (parallelizable after Task 1 completes)
- Task 9: Verification (after all implementation tasks complete)

---

## Task 1: Update Dockerfile.j2 Template with Git Commit ARG and OCI Labels

**Files:**
- Modify: `Dockerfile.j2:31-54`

**Depends on:** none

**Test:** Verify template renders correctly with jinja2

- [ ] **Step 1: Add git_commit build argument**

Add the new ARG after the existing `version` ARG (line 33):

```dockerfile
ARG release=main
ARG version=latest
ARG git_commit=unknown
```

- [ ] **Step 2: Add OCI labels to LABEL block**

Add three new labels to the LABEL block (after line 54, before the closing quote):

```dockerfile
LABEL name="multicluster-engine/capoa-{{ provider }}-rhel9" \
      cpe="cpe:/a:redhat:multicluster_engine:5.0::el9" \
      summary="${SUMMARY}" \
      description="${DESCRIPTION}" \
      com.redhat.component="cluster-api-{{ provider }}-provider-openshift-assisted" \
      io.k8s.display-name="CAPI {{ provider | title  }} Provider OpenShift Assisted" \
      io.k8s.description="${DESCRIPTION}" \
      io.openshift.tags="capi,install,cluster,provisioning,{{ provider }}" \
      distribution-scope="public" \
      release="${release}" \
      version="${version}" \
      url="https://github.com/openshift-assisted/cluster-api-provider-openshift-assisted" \
      vendor="Red Hat, Inc." \
      org.opencontainers.image.revision="${git_commit}" \
      org.opencontainers.image.source="https://github.com/openshift-assisted/cluster-api-provider-openshift-assisted" \
      org.opencontainers.image.vcs-type="git"
```

- [ ] **Step 3: Verify template syntax**

Run: `python3 -c "import jinja2; print(jinja2.Template(open('Dockerfile.j2').read()).render(provider='bootstrap'))"`

Expected: Valid Dockerfile output with the new ARG and labels visible

- [ ] **Step 4: Commit template changes**

```bash
git add Dockerfile.j2
git commit -m "feat(build): add git commit OCI labels to Dockerfile template

Add org.opencontainers.image.revision, org.opencontainers.image.source,
and org.opencontainers.image.vcs-type labels to container images for
traceability. Introduce git_commit build arg with 'unknown' default.

MGMT-24394

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
```

---

## Task 2: Regenerate Concrete Dockerfiles from Template

**Files:**
- Generate: `Dockerfile.bootstrap-provider`
- Generate: `Dockerfile.controlplane-provider`

**Depends on:** Task 1

**Test:** Verify both Dockerfiles contain new ARG and labels

- [ ] **Step 1: Regenerate bootstrap provider Dockerfile**

Run: `make generate-dockerfile-bootstrap`

Expected: Dockerfile.bootstrap-provider updated with new ARG and labels

- [ ] **Step 2: Regenerate controlplane provider Dockerfile**

Run: `make generate-dockerfile-controlplane`

Expected: Dockerfile.controlplane-provider updated with new ARG and labels

- [ ] **Step 3: Verify bootstrap Dockerfile contains new labels**

Run: `grep -A 3 "org.opencontainers.image.revision" Dockerfile.bootstrap-provider`

Expected: Three OCI labels present

- [ ] **Step 4: Verify controlplane Dockerfile contains new labels**

Run: `grep -A 3 "org.opencontainers.image.revision" Dockerfile.controlplane-provider`

Expected: Three OCI labels present

- [ ] **Step 5: Commit generated Dockerfiles**

```bash
git add Dockerfile.bootstrap-provider Dockerfile.controlplane-provider
git commit -m "build: regenerate Dockerfiles with OCI labels

Generated from Dockerfile.j2 template.

MGMT-24394

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
```

---

## Task 3: Update Makefile to Pass Git Commit to Docker Build

**Files:**
- Modify: `Makefile:164-166`

**Depends on:** none

**Test:** Run make docker-build and verify build-arg is passed

- [ ] **Step 1: Add GIT_COMMIT variable definition**

Add before the `docker-build-internal` target (around line 164):

```make
# Git commit hash for container image labels
GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
```

- [ ] **Step 2: Update docker-build-internal target to pass git_commit build-arg**

Modify the `$(CONTAINER_TOOL) build` command (line 166):

```make
.PHONY: docker-build-internal
docker-build-internal: ## Does not ensure Dockerfiles are generated.
	$(CONTAINER_TOOL) build -f Dockerfile.$(PROVIDER)-provider --build-arg git_commit=$(GIT_COMMIT) -t ${IMG} .
```

- [ ] **Step 3: Test the Makefile change with dry-run**

Run: `make docker-build-internal PROVIDER=bootstrap CONTAINER_TAG=test-makefile --dry-run 2>&1 | grep "build-arg"`

Expected: Output shows `--build-arg git_commit=<commit-hash>`

- [ ] **Step 4: Commit Makefile changes**

```bash
git add Makefile
git commit -m "feat(build): pass git commit hash to docker build

Extract current git commit using 'git rev-parse HEAD' and pass as
git_commit build-arg to container builds. Fallback to 'unknown' if
git command fails.

MGMT-24394

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
```

---

## Task 4: Update GitHub Workflow to Pass Git Commit

**Files:**
- Modify: `.github/workflows/publish_images.yaml:50-63`

**Depends on:** none

**Test:** Verify workflow YAML syntax is valid

- [ ] **Step 1: Add build-args to bootstrap image build step**

Modify the "Build and publish bootstrap image to Quay" step (lines 50-56):

```yaml
      - name: Build and publish bootstrap image to Quay
        uses: docker/build-push-action@v5
        with:
          push: true
          context: ${{ env.context }}
          tags: "${{ secrets.REGISTRY_SERVER }}/${{ secrets.REGISTRY_NAMESPACE }}/${{ env.bootstrap_image_name }}:${{env.IMAGE_TAG}}"
          file: "${{ env.bootstrap_dockerfile }}"
          build-args: |
            release=${{ github.ref_name }}
            version=${{ env.IMAGE_TAG }}
            git_commit=${{ github.sha }}
```

- [ ] **Step 2: Add build-args to controlplane image build step**

Modify the "Build and publish controplane image to Quay" step (lines 57-63):

```yaml
      - name: Build and publish controplane image to Quay
        uses: docker/build-push-action@v5
        with:
          push: true
          context: ${{ env.context }}
          tags: "${{ secrets.REGISTRY_SERVER }}/${{ secrets.REGISTRY_NAMESPACE }}/${{ env.controlplane_image_name }}:${{env.IMAGE_TAG}}"
          file: "${{ env.controlplane_dockerfile }}"
          build-args: |
            release=${{ github.ref_name }}
            version=${{ env.IMAGE_TAG }}
            git_commit=${{ github.sha }}
```

- [ ] **Step 3: Validate YAML syntax**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/publish_images.yaml'))"`

Expected: No errors (valid YAML)

- [ ] **Step 4: Commit workflow changes**

```bash
git add .github/workflows/publish_images.yaml
git commit -m "feat(ci): add git commit to GitHub workflow image builds

Pass github.sha as git_commit build-arg to both bootstrap and
controlplane image builds for traceability.

MGMT-24394

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
```

---

## Task 5 [P]: Update Tekton Pipeline - Bootstrap Provider Push

**Files:**
- Modify: `.tekton/cluster-api-provider-openshift-assisted-bootstrap-mce-50-push.yaml:36-40`

**Depends on:** none (parallelizable)

**Test:** Verify YAML syntax and build-args parameter

- [ ] **Step 1: Add git_commit to build-args array**

Modify the `build-args` parameter (lines 36-39):

```yaml
  - name: build-args
    value:
    - release={{target_branch}}
    - version={{revision}}
    - git_commit={{revision}}
```

- [ ] **Step 2: Validate YAML syntax**

Run: `python3 -c "import yaml; yaml.safe_load(open('.tekton/cluster-api-provider-openshift-assisted-bootstrap-mce-50-push.yaml'))"`

Expected: No errors

- [ ] **Step 3: Commit changes**

```bash
git add .tekton/cluster-api-provider-openshift-assisted-bootstrap-mce-50-push.yaml
git commit -m "feat(tekton): add git commit to bootstrap push pipeline

Pass revision as git_commit build-arg for downstream image builds.

MGMT-24394

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
```

---

## Task 6 [P]: Update Tekton Pipeline - Bootstrap Provider Pull Request

**Files:**
- Modify: `.tekton/cluster-api-provider-openshift-assisted-bootstrap-mce-50-pull-request.yaml:36-40`

**Depends on:** none (parallelizable)

**Test:** Verify YAML syntax and build-args parameter

- [ ] **Step 1: Add git_commit to build-args array**

Modify the `build-args` parameter (same as Task 5):

```yaml
  - name: build-args
    value:
    - release={{target_branch}}
    - version={{revision}}
    - git_commit={{revision}}
```

- [ ] **Step 2: Validate YAML syntax**

Run: `python3 -c "import yaml; yaml.safe_load(open('.tekton/cluster-api-provider-openshift-assisted-bootstrap-mce-50-pull-request.yaml'))"`

Expected: No errors

- [ ] **Step 3: Commit changes**

```bash
git add .tekton/cluster-api-provider-openshift-assisted-bootstrap-mce-50-pull-request.yaml
git commit -m "feat(tekton): add git commit to bootstrap PR pipeline

Pass revision as git_commit build-arg for downstream PR builds.

MGMT-24394

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
```

---

## Task 7 [P]: Update Tekton Pipeline - Controlplane Provider Push

**Files:**
- Modify: `.tekton/cluster-api-provider-openshift-assisted-control-plane-mce-50-push.yaml:36-40`

**Depends on:** none (parallelizable)

**Test:** Verify YAML syntax and build-args parameter

- [ ] **Step 1: Add git_commit to build-args array**

Modify the `build-args` parameter (same as Task 5):

```yaml
  - name: build-args
    value:
    - release={{target_branch}}
    - version={{revision}}
    - git_commit={{revision}}
```

- [ ] **Step 2: Validate YAML syntax**

Run: `python3 -c "import yaml; yaml.safe_load(open('.tekton/cluster-api-provider-openshift-assisted-control-plane-mce-50-push.yaml'))"`

Expected: No errors

- [ ] **Step 3: Commit changes**

```bash
git add .tekton/cluster-api-provider-openshift-assisted-control-plane-mce-50-push.yaml
git commit -m "feat(tekton): add git commit to controlplane push pipeline

Pass revision as git_commit build-arg for downstream image builds.

MGMT-24394

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
```

---

## Task 8 [P]: Update Tekton Pipeline - Controlplane Provider Pull Request

**Files:**
- Modify: `.tekton/cluster-api-provider-openshift-assisted-control-plane-mce-50-pull-request.yaml:36-40`

**Depends on:** none (parallelizable)

**Test:** Verify YAML syntax and build-args parameter

- [ ] **Step 1: Add git_commit to build-args array**

Modify the `build-args` parameter (same as Task 5):

```yaml
  - name: build-args
    value:
    - release={{target_branch}}
    - version={{revision}}
    - git_commit={{revision}}
```

- [ ] **Step 2: Validate YAML syntax**

Run: `python3 -c "import yaml; yaml.safe_load(open('.tekton/cluster-api-provider-openshift-assisted-control-plane-mce-50-pull-request.yaml'))"`

Expected: No errors

- [ ] **Step 3: Commit changes**

```bash
git add .tekton/cluster-api-provider-openshift-assisted-control-plane-mce-50-pull-request.yaml
git commit -m "feat(tekton): add git commit to controlplane PR pipeline

Pass revision as git_commit build-arg for downstream PR builds.

MGMT-24394

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>"
```

---

## Task 9: Verification - Build and Inspect Images Locally

**Files:**
- Test: All modified files

**Depends on:** Tasks 1-8 (all implementation complete)

**Test:** Build both images locally and verify labels are present with correct values

- [ ] **Step 1: Build bootstrap provider image locally**

Run: `make bootstrap-docker-build CONTAINER_TAG=test-verify`

Expected: Build succeeds, outputs image with tag `quay.io/edge-infrastructure/cluster-api-bootstrap-provider-openshift-assisted:test-verify`

- [ ] **Step 2: Inspect bootstrap image labels**

Run: `podman inspect quay.io/edge-infrastructure/cluster-api-bootstrap-provider-openshift-assisted:test-verify | jq '.[0].Config.Labels'`

Expected: JSON output showing all labels including:
- `org.opencontainers.image.revision`: equals output of `git rev-parse HEAD`
- `org.opencontainers.image.source`: equals "https://github.com/openshift-assisted/cluster-api-provider-openshift-assisted"
- `org.opencontainers.image.vcs-type`: equals "git"

- [ ] **Step 3: Verify bootstrap revision label matches current commit**

Run: 
```bash
EXPECTED_COMMIT=$(git rev-parse HEAD)
ACTUAL_COMMIT=$(podman inspect quay.io/edge-infrastructure/cluster-api-bootstrap-provider-openshift-assisted:test-verify | jq -r '.[0].Config.Labels."org.opencontainers.image.revision"')
echo "Expected: $EXPECTED_COMMIT"
echo "Actual: $ACTUAL_COMMIT"
test "$EXPECTED_COMMIT" = "$ACTUAL_COMMIT" && echo "✓ PASS" || echo "✗ FAIL"
```

Expected: Output shows "✓ PASS"

- [ ] **Step 4: Build controlplane provider image locally**

Run: `make controlplane-docker-build CONTAINER_TAG=test-verify`

Expected: Build succeeds

- [ ] **Step 5: Inspect controlplane image labels**

Run: `podman inspect quay.io/edge-infrastructure/cluster-api-controlplane-provider-openshift-assisted:test-verify | jq '.[0].Config.Labels'`

Expected: Same three OCI labels present with correct values

- [ ] **Step 6: Verify controlplane revision label matches current commit**

Run:
```bash
EXPECTED_COMMIT=$(git rev-parse HEAD)
ACTUAL_COMMIT=$(podman inspect quay.io/edge-infrastructure/cluster-api-controlplane-provider-openshift-assisted:test-verify | jq -r '.[0].Config.Labels."org.opencontainers.image.revision"')
echo "Expected: $EXPECTED_COMMIT"
echo "Actual: $ACTUAL_COMMIT"
test "$EXPECTED_COMMIT" = "$ACTUAL_COMMIT" && echo "✓ PASS" || echo "✗ FAIL"
```

Expected: Output shows "✓ PASS"

- [ ] **Step 7: Verify existing labels are unchanged**

Run:
```bash
# Check that critical existing labels are still present
podman inspect quay.io/edge-infrastructure/cluster-api-bootstrap-provider-openshift-assisted:test-verify | \
  jq -r '.[0].Config.Labels | {
    vendor,
    url,
    "io.k8s.display-name",
    "com.redhat.component"
  }'
```

Expected: All four labels present with expected values:
- `vendor`: "Red Hat, Inc."
- `url`: "https://github.com/openshift-assisted/cluster-api-provider-openshift-assisted"
- `io.k8s.display-name`: "CAPI Bootstrap Provider OpenShift Assisted"
- `com.redhat.component`: "cluster-api-bootstrap-provider-openshift-assisted"

- [ ] **Step 8: Verify label count increased by exactly 3**

Compare label count before/after. Since we're testing post-implementation:

Run:
```bash
# Count OCI image labels (should be exactly 3)
podman inspect quay.io/edge-infrastructure/cluster-api-bootstrap-provider-openshift-assisted:test-verify | \
  jq -r '.[0].Config.Labels | to_entries | map(select(.key | startswith("org.opencontainers.image"))) | length'
```

Expected: Output is `3`

- [ ] **Step 9: Test fallback behavior (optional - for completeness)**

Create a test without git context:

```bash
# Build from a non-git directory
mkdir -p /tmp/test-nogit
cp Dockerfile.bootstrap-provider /tmp/test-nogit/
cp -r bootstrap controlplane util pkg assistedinstaller internal go.mod go.sum vendor /tmp/test-nogit/
cd /tmp/test-nogit
podman build -f Dockerfile.bootstrap-provider -t test-nogit:latest .
podman inspect test-nogit:latest | jq -r '.[0].Config.Labels."org.opencontainers.image.revision"'
cd -
rm -rf /tmp/test-nogit
```

Expected: Output is `unknown`

- [ ] **Step 10: Document verification results**

Create a summary comment in the commit or PR description confirming:
- ✓ Bootstrap image has all 3 OCI labels
- ✓ Controlplane image has all 3 OCI labels
- ✓ Revision label contains correct 40-char commit hash
- ✓ Existing labels unchanged
- ✓ Fallback to "unknown" works (if Step 9 executed)

---

## Post-Implementation Notes

After all tasks are complete:

1. **Create Pull Request** with title: `feat: add git commit OCI labels to container images (MGMT-24394)`

2. **PR Description** should include:
   - Link to spec: `specs/MGMT-24394/spec.md`
   - Link to plan: `specs/MGMT-24394/plan.md`
   - Verification results from Task 9
   - Note that changes affect all three build contexts (local, GitHub, Tekton)

3. **CI Checks**: Ensure `check-generated-files` passes (verifies Dockerfiles are in sync with template)

4. **Downstream Testing**: Once merged, Tekton pipeline builds will automatically include the labels. Verify in downstream by pulling a built image and inspecting labels.

5. **Documentation**: No user-facing docs needed - this is an infrastructure change. The commit messages and spec serve as documentation.
