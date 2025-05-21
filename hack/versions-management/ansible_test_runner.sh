#!/bin/bash
set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

ARGS=()
if [ "${DRY_RUN:-false}" = "true" ]; then
  ARGS+=(--dry-run)
fi

python "$SCRIPT_DIR/ansible_test_runner.py" "${ARGS[@]}"

source "$SCRIPT_DIR/github_auth.sh"

if [ "${DRY_RUN:-false}" = true ]; then
    echo "Ansible test runner has finished successfully"
else
    BRANCH="release-candidates-test-$(date '+%Y-%m-%d-%H-%M')"
    git checkout -b $BRANCH
    git add release-candidates.yaml
    git commit -m "Update release candidates status after testing" || echo "No changes to commit"
    git push -u "${CI_REMOTE_NAME:-versions_management}" $BRANCH
    gh pr create --title "Update release candidates test results" --body "Automated PR to update release candidates" --base master
fi
