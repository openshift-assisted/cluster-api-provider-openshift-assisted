#!/bin/bash
set -o nounset
set -o errexit
set -o pipefail

if [ -z "${GITHUB_APP_ID+x}" ]; then
  echo "Error: GITHUB_APP_ID environment variable is not set"
  exit 1
fi

if [ -z "${GITHUB_APP_INSTALLATION_ID+x}" ]; then
  echo "Error: GITHUB_APP_INSTALLATION_ID environment variable is not set"
  exit 1
fi

if [ -z "${GITHUB_APP_PRIVATE_KEY_PATH+x}" ]; then
  echo "Error: GITHUB_APP_PRIVATE_KEY_PATH environment variable is not set"
  exit 1
fi

JWT=$(jwt encode --alg=RS256 --secret=@"$GITHUB_APP_PRIVATE_KEY_PATH" '{"iss":"'"$GITHUB_APP_ID"'", "exp":'"$(date -d "+600 seconds" +%s)"'}')

INSTALLATION_TOKEN=$(curl -sSf -X POST \
  -H "Authorization: Bearer $JWT" \
  -H "Accept: application/vnd.github.v3+json" \
  "https://api.github.com/app/installations/$GITHUB_APP_INSTALLATION_ID/access_tokens" \
  | jq -r ".token")

if [ -z "$INSTALLATION_TOKEN" ]; then
  echo "Failed to obtain GitHub installation token" >&2
  exit 1
fi

export GITHUB_APP_PRIVATE_KEY=$(cat ${GITHUB_APP_PRIVATE_KEY_PATH})
export GITHUB_TOKEN="$INSTALLATION_TOKEN"
repo_url="https://x-access-token:${INSTALLATION_TOKEN}@github.com/openshift-assisted/cluster-api-provider-openshift-assisted.git"
git remote add "${CI_REMOTE_NAME:-versions_management}" "$repo_url"
git remote set-url "${CI_REMOTE_NAME:-versions_management}" "$repo_url"
git config --global user.name "CAPI version automation"
git config --global user.email "capi-openshift-assisted@redhat.com"
