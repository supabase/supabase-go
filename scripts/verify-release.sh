#!/usr/bin/env bash
# Verify the release tagged at HEAD serves consumers the intended content:
# learn the released '<module> <version>' pairs from origin's tags pointing
# at HEAD, download each one straight from GitHub into a throwaway module
# cache under /tmp, confirm a LICENSE sits at each module zip's root and
# print the go.sum-shaped content hashes. Keep the hash lines: a later
# scripts/seed-module-proxy.sh run for the same release must print them byte
# for byte, proving the public module proxy serves what GitHub serves.
#
# The downloads go direct (GOPROXY=direct) with GOPRIVATE covering the
# modules, so neither proxy.golang.org nor sum.golang.org is contacted:
# asking either would make it fetch and immutably cache the version, the
# deliberate and irreversible step that belongs to
# scripts/seed-module-proxy.sh alone. That restraint also makes this script
# work while the repository is private, where git merely needs to
# authenticate to GitHub without prompting - see
# https://go.dev/doc/faq#git_https.
set -euo pipefail

source "$(dirname "$0")/common.sh"

echo "Verify Release..."

head_sha="$(git rev-parse HEAD)"
released="$(released_versions_at_head)"
if [ -z "${released}" ]; then
  echo "No release tags on origin point at HEAD (${head_sha})." >&2
  echo "Run this from the release PR's landing commit, after the release-tags workflow has pushed its tags." >&2
  exit 1
fi

scratch="$(mktemp -d /tmp/verify-release.XXXXXX)"
cleanup() {
  GOMODCACHE="${scratch}/modcache" go clean -modcache 2> /dev/null || true
  rm -rf "${scratch}" 2> /dev/null || true
}
trap cleanup EXIT

export GOMODCACHE="${scratch}/modcache" GOWORK=off
export GOPROXY=direct GOPRIVATE="$(module_path_base)"
cd "${scratch}"

count=0
while read -r module version; do
  echo "==> download (direct from GitHub) ${module}@${version}"
  report="${scratch}/$(printf '%s' "${module}" | tr '/' '-').json"
  download_module_json "$(module_path_base)/${module}@${version}" "${report}"
  if [ ! -f "$(jq -r '.Dir' "${report}")/LICENSE" ]; then
    echo "${module}@${version}: the module zip carries no LICENSE at its root." >&2
    exit 1
  fi
  count=$((count + 1))
done <<< "${released}"

echo ""
echo "Content hashes as served by GitHub, which scripts/seed-module-proxy.sh must reproduce:"
echo ""
module_hash_lines "${scratch}"/*.json
echo ""
echo "✅ Verified ${count} module release(s) at ${head_sha}. Nothing was seeded."
