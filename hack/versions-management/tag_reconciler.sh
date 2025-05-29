#!/bin/bash
set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

source "$SCRIPT_DIR/github_auth.sh"

ARGS=()
if [ "${DRY_RUN:-false}" = "true" ]; then
  ARGS+=(--dry-run)
fi

python "$SCRIPT_DIR/tag_reconciler.py" "${ARGS[@]}"
