#!/usr/bin/env bash
# Seed a release without an operator sitting through the proxy's lag, by
# chaining the two release scripts and comparing their hash records:
#
# 1. scripts/verify-release.sh, whose failure stops everything before the
#    proxy is asked for anything.
# 2. scripts/seed-module-proxy.sh, run again a minute apart for up to two
#    hours while it fails, since proxy.golang.org answers 404 for a freshly
#    tagged version until its view of the repository catches up. A failed
#    attempt seeds nothing.
# 3. A byte-for-byte comparison of the two scripts' records, failing the run
#    if proxy.golang.org serves different content than GitHub does.
#
# Usage: scripts/seed-release.sh [<landing-commit>]
#
# The optional argument names the release PR's landing commit, HEAD when
# omitted, and is passed to both scripts. The Seed Release workflow
# (.github/workflows/seed-release.yml) runs this script when dispatched by
# hand after the release-tags workflow has pushed the tags. Run locally it
# behaves the same, each attempt reading origin's tags afresh. The outcome,
# the proxy's timing and the hash lines go to stdout and, in GitHub Actions,
# to the workflow run's summary page.
#
# Safe to run again at any time, for any release: the proxy serves an already
# seeded version from its immutable cache, each script replaces its own
# record and the comparison has no side effects, so a repeated run ends in
# the same state and reports the proxy serving on its first attempt.
set -euo pipefail

source "$(dirname "$0")/common.sh"

# proxy.golang.org has answered 404 for about twenty minutes after this
# repository's tag pushes, against the half hour its FAQ allows a stale
# answer, so the attempts span two hours.
seed_attempts=120
seed_retry_interval=60

# Print a count of seconds as minutes and seconds, such as 23m14s.
duration() {
  printf '%dm%02ds' "$(($1 / 60))" "$(($1 % 60))"
}

echo "Seed Release..."

commit_sha="$(resolve_release_commit "${1:-}")"

./scripts/verify-release.sh "$@"

started="$(date +%s)"
attempt=1
until ./scripts/seed-module-proxy.sh "$@"; do
  if [ "${attempt}" -ge "${seed_attempts}" ]; then
    note "proxy.golang.org has not served the release at ${commit_sha} in ${seed_attempts} attempts ${seed_retry_interval}s apart, $(duration $(( $(date +%s) - started ))) after the first. A failed attempt seeds nothing: run again, or see RELEASING.md."
    exit 1
  fi
  attempt=$((attempt + 1))
  echo ""
  echo "Attempt ${attempt} of ${seed_attempts} in ${seed_retry_interval}s."
  sleep "${seed_retry_interval}"
done
served="$(date +%s)"

github_record="$(hash_record_path verify-release)"
proxy_record="$(hash_record_path seed-module-proxy)"
echo ""
if ! diff "${github_record}" "${proxy_record}"; then
  note "proxy.golang.org serves different content than GitHub does for the release at ${commit_sha}: see the diff of ${github_record##*/} against ${proxy_record##*/} in the log."
  exit 1
fi

count=$(( $(wc -l < "${github_record}") / 2 ))
note "✅ Seeded ${count} module release(s) at ${commit_sha}. proxy.golang.org served the release on attempt ${attempt} of ${seed_attempts}, $(duration $((served - started))) after the first attempt and $(duration $((served - $(git show -s --format=%ct "${commit_sha}")))) after the landing commit, with the content hashes GitHub serves, byte for byte:"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  { echo '```'; cat "${github_record}"; echo '```'; } >> "${GITHUB_STEP_SUMMARY}"
fi
