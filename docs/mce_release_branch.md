# Creating a Release Branch for a New MCE Version

## What Does It Do?

When a new MCE (Multicluster Engine) version is released, a new branch needs to be created in the CAPOA project to match that version.

The script performs the following actions:

1. **Detects the freeze version** - automatically finds the latest `backplane-X.Y` branch
2. **Creates a new branch** named `backplane-X.Y` for the next version (clean copy of master)
3. **Updates Tekton files** - renames files and updates their content to the new version
4. **Updates Dockerfiles** - updates the `cpe` label version in `Dockerfile.j2`, `Dockerfile.bootstrap-provider` and `Dockerfile.controlplane-provider`
5. **Updates `renovate.json`** - adds the freeze branch to the list of branches receiving Konflux updates
6. **Updates `sync-branch.yaml`** - sets the `TARGET_BRANCH` to the new branch
7. **Creates a PR** to master with all the changes (changes are fast-forwarded to the release branch after merge)

## How to Run

> **Note:** Requires **Write** access to the repository.

1. Go to [Release Branch Generator](https://github.com/openshift-assisted/cluster-api-provider-openshift-assisted/actions/workflows/release-branch-generator.yaml)
2. Click **"Run workflow"**
3. *(Optional)* Enter the **next version** only for non-sequential jumps (e.g., `2.17` or `5.0`)
4. Click **"Run workflow"**

The workflow will automatically detect the current version to freeze and calculate the next version.

## Examples

### Sequential Version (Default)
For standard incremental releases, just run the workflow without parameters:
- Freeze version: auto-detected (e.g., `2.11`)
- Next version: auto-calculated (e.g., `2.12`)

### Non-Sequential Version
For non-sequential jumps (e.g., MCE alignment with ACM, or OCP 5):
- **Next version:** `2.17` (or `5.0`)
- Freeze version: auto-detected from existing branches
