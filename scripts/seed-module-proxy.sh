#!/usr/bin/env bash
# Seed the public Go module ecosystem with a release: learn the released
# '<module> <version>' pairs from origin's tags pointing at the landing
# commit - named by the optional argument, HEAD when omitted - and pull each
# one through proxy.golang.org into a throwaway module cache under /tmp.
#
# Usage: scripts/seed-module-proxy.sh [<landing-commit>] The first request makes the proxy fetch the version from GitHub
# and cache it immutably - the point of no return for a release - records its
# hashes in the sum.golang.org checksum database and leads pkg.go.dev to
# build the documentation pages minutes later. Run scripts/verify-release.sh
# first: its hash lines and this script's must match byte for byte, or the
# proxy is serving different content than GitHub does.
#
# GOPROXY is pinned to proxy.golang.org with no direct fallback, so success
# proves the proxy itself serves every version, and the checksum database
# stays on, so every download is verified against sum.golang.org exactly as
# a consumer's would be. GOPRIVATE and its siblings are cleared so an
# operator shell still configured for the repository's private phase cannot
# sidestep the proxy. Requires the repository to be public - a release cut
# while it was private is seeded the day it goes public, by passing that
# release's landing commit. Safe to re-run: the proxy answers from its
# immutable cache.
set -euo pipefail

source "$(dirname "$0")/common.sh"

echo "Seed Module Proxy..."

commit_sha="$(resolve_release_commit "${1:-}")"
released="$(released_versions_at "${commit_sha}")"
if [ -z "${released}" ]; then
  echo "No release tags on origin point at ${commit_sha}." >&2
  echo "Name a release PR's landing commit as the argument (HEAD when omitted), once the release-tags workflow has pushed its tags." >&2
  exit 1
fi

scratch="$(mktemp -d /tmp/seed-module-proxy.XXXXXX)"
cleanup() {
  GOMODCACHE="${scratch}/modcache" go clean -modcache 2> /dev/null || true
  rm -rf "${scratch}" 2> /dev/null || true
}
trap cleanup EXIT

export GOMODCACHE="${scratch}/modcache" GOWORK=off
export GOPROXY='https://proxy.golang.org' GOPRIVATE='' GONOPROXY='' GONOSUMDB=''
cd "${scratch}"

count=0
while read -r module version; do
  echo "==> download (through proxy.golang.org) ${module}@${version}"
  report="${scratch}/$(printf '%s' "${module}" | tr '/' '-').json"
  download_module_json "$(module_path_base)/${module}@${version}" "${report}"
  count=$((count + 1))
done <<< "${released}"

echo ""
echo "Content hashes as served by the proxy, which must match scripts/verify-release.sh's:"
echo ""
module_hash_lines "${scratch}"/*.json
echo ""
echo "✅ Seeded ${count} module release(s) at ${commit_sha} into the public module ecosystem."
