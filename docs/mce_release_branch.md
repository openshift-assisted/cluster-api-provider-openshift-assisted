# Creating a Release Branch for a New MCE Version

## What Does It Do?

When a new MCE (Multicluster Engine) version is released, a new branch needs to be created in the CAPOA project to match that version.

The script performs the following actions:

1. **Creates a new branch** named `backplane-X.Y` (e.g., `backplane-2.11`)
2. **Updates Tekton files** - renames files and updates their content to the new version (from `mce-XY` to `mce-XY+1`)
3. **Updates `renovate.json`** - adds the previous branch to the list of branches receiving Konflux updates
4. **Updates `sync-branch.yaml`** - sets the `TARGET_BRANCH` to the new branch
5. **Creates a PR** to master with all the changes

## How to Run

> **Note:** Requires **Write** access to the repository.

1. Go to [Release Branch Generator](https://github.com/openshift-assisted/cluster-api-provider-openshift-assisted/actions/workflows/release-branch-generator.yaml)
2. Click **"Run workflow"**
3. Enter the version number (e.g., `2.11`)
4. Click **"Run workflow"**

The workflow will perform all actions automatically.
