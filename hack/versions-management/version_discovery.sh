#!/bin/bash
set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

source "$SCRIPT_DIR/github_auth.sh"

ARGS=()
if [ "${DRY_RUN:-false}" = "true" ]; then
  ARGS+=(--dry-run)
fi
python "$SCRIPT_DIR/version_discovery.py" "${ARGS[@]}"

if [ "${DRY_RUN:-false}" != true ]; then
    BRANCH="version-discovery-$(date '+%Y-%m-%d-%H-%M')"
    git checkout -b $BRANCH
    git add release-candidates.yaml
    git commit -m "Update release candidates"
    git push -u "${CI_REMOTE_NAME:-versions_management}" $BRANCH
    gh pr create --title "Update release candidates" --body "Automated PR to update release candidates" --base master --label "auto-merge"
fi

