#!/bin/bash
set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

ARGS=()
if [ "${DRY_RUN:-false}" = "true" ]; then
  ARGS+=(--dry-run)
fi

PENDING_SNAPSHOT_ID=$(yq '.snapshots[] | select(.metadata.status == "pending") | .metadata.id' release-candidates.yaml | tail -n1)

if [ -z "$PENDING_SNAPSHOT_ID" ]; then
    echo "No pending snapshot found"
    exit 0
fi

TESTED_WITH_REF=$(yq ".snapshots[] | select(.metadata.id == \"$PENDING_SNAPSHOT_ID\") | .metadata.tested_with_ref" release-candidates.yaml)
if [ -n "$TESTED_WITH_REF" ] && [ "$TESTED_WITH_REF" != "null" ]; then
    echo "Checking out to tested_with_ref: $TESTED_WITH_REF"
    git checkout "$TESTED_WITH_REF"
else
    CURRENT_COMMIT=$(git rev-parse HEAD)
    echo "Setting tested_with_ref: $CURRENT_COMMIT"
    yq -i "(.snapshots[] | select(.metadata.id == \"$PENDING_SNAPSHOT_ID\") | .metadata.tested_with_ref) |= \"$CURRENT_COMMIT\"" release-candidates.yaml
fi

python "$SCRIPT_DIR/ansible_test_runner.py" "${ARGS[@]}" --pending-snapshot-id "$PENDING_SNAPSHOT_ID"

git stash push release-candidates.yaml
git checkout master
git stash apply

if [ "${DRY_RUN:-false}" = true ]; then
    echo "Ansible test runner has finished successfully"
else
    source "$SCRIPT_DIR/github_auth.sh"
    BRANCH="release-candidates-test-$(date '+%Y-%m-%d-%H-%M')"
    git checkout -b $BRANCH
    git add release-candidates.yaml
    git commit -m "Update release candidates status after testing" || echo "No changes to commit"
    git push -u "${CI_REMOTE_NAME:-versions_management}" $BRANCH
    gh pr create --title "Update release candidates test results" --body "Automated PR to update release candidates" --base master --label "auto-merge"
fi
