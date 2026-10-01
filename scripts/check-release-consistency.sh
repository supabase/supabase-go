#!/usr/bin/env bash
# Join-the-dots pre-flight over the release bookkeeping, from committed files
# alone: no network and no Go module resolution, since pinned sibling versions
# are not resolvable until their tags exist. The invariants hold on every
# commit, release or not, so this runs in CI on every push as well as locally
# before a release PR and in the release-tags workflow before it pushes tags.
#
# It catches the anticipated release failure modes: a malformed or duplicate
# changelog version heading, a misplaced or empty Unreleased section, a
# sibling require left at the zero pseudo-version once the sibling has
# releases (a forgotten pin), a require naming a version the sibling never
# declared (a typo'd pin, or a release prepared out of dependency order) and
# telemetrytest fabricated versions that a real declared version outranks.
set -euo pipefail

source "$(dirname "$0")/common.sh"
workspace_modules="$(enumerate_workspace_modules)"

module_base="github.com/supabase/supabase-go"
zero_pseudo_version="v0.0.0-00010101000000-000000000000"

echo "Check Release Consistency..."

failures=0
fail() {
  echo "FAIL: $*" >&2
  failures=$((failures + 1))
}

for module in ${workspace_modules}; do
  module="${module#./}"
  changelog="${module}/CHANGELOG.md"

  if [ ! -f "${changelog}" ]; then
    fail "${changelog} is missing."
    continue
  fi

  # The Unreleased section is optional, but when present it opens the
  # changelog and holds entries (see the root CHANGELOG.md).
  unreleased_headings="$(grep -cx '## Unreleased' "${changelog}" || true)"
  if [ "${unreleased_headings}" -gt 1 ]; then
    fail "${changelog} carries more than one '## Unreleased' heading."
  elif [ "${unreleased_headings}" -eq 1 ]; then
    if [ "$(grep -E '^## ' "${changelog}" | head -n 1)" != "## Unreleased" ]; then
      fail "${changelog} must open its sections with '## Unreleased'."
    fi
    if [ "$(unreleased_entry_lines "${module}")" -eq 0 ]; then
      fail "${changelog} carries an empty '## Unreleased' section - add the heading only alongside an entry."
    fi
  elif [ -z "$(declared_module_versions "${module}")" ]; then
    fail "${changelog} has neither an '## Unreleased' section nor a release."
  fi

  # A level-two heading that is neither Unreleased nor a well-formed version
  # stamp would be skipped by the release automation, silently.
  malformed="$(grep -E '^## ' "${changelog}" | grep -vx '## Unreleased' | grep -Ev "$(version_heading_pattern)" || true)"
  if [ -n "${malformed}" ]; then
    fail "${changelog} carries malformed version headings: ${malformed}"
  fi

  duplicates="$(declared_module_versions "${module}" | sort | uniq -d)"
  if [ -n "${duplicates}" ]; then
    fail "${changelog} declares a version more than once: ${duplicates}"
  fi
done

# A published module may only require a sibling at a version the sibling's
# changelog declares. The zero pseudo-version is the pre-first-release
# placeholder and stops being legal the moment the sibling declares any
# version.
for module in ${workspace_modules}; do
  module="${module#./}"
  sibling_requires="$(go mod edit -json "${module}/go.mod" \
    | jq -r --arg base "${module_base}/" \
      '.Require[]? | select(.Path | startswith($base)) | (.Path | ltrimstr($base)) + " " + .Version')"
  while read -r sibling required_version; do
    [ -n "${sibling}" ] || continue
    if [ "${required_version}" = "${zero_pseudo_version}" ]; then
      if [ -n "$(declared_module_versions "${sibling}")" ]; then
        fail "${module}/go.mod requires ${sibling} at the zero pseudo-version, but ${sibling} has releases - a forgotten dep=version pin."
      fi
    elif ! declared_module_versions "${sibling}" | grep -qx "${required_version}"; then
      fail "${module}/go.mod requires ${sibling}@${required_version}, which ${sibling}/CHANGELOG.md never declares - a typo'd pin, or a release prepared out of dependency order."
    fi
  done <<< "${sibling_requires}"
done

# telemetrytest's fabricated versions must outrank every declared version of
# the modules they stand in for, or minimal version selection would record a
# real version and the consumer-view telemetry check would assert the wrong
# header. The fabricated scheme keeps a major.minor of 1.999, so comparing
# major then minor is sufficient.
fabricated_requires="$(go mod edit -json telemetrytest/go.mod \
  | jq -r --arg base "${module_base}/" \
    '.Require[]? | select(.Version | endswith("-fabricated")) | (.Path | ltrimstr($base)) + " " + .Version')"
while read -r sibling fabricated_version; do
  [ -n "${sibling}" ] || continue
  newest_declared="$(declared_module_versions "${sibling}" | head -n 1)"
  [ -n "${newest_declared}" ] || continue
  outranks="$(awk -v fabricated="${fabricated_version#v}" -v declared="${newest_declared#v}" 'BEGIN {
    split(fabricated, f, /[.-]/)
    split(declared, d, /[.-]/)
    if (f[1] + 0 > d[1] + 0 || (f[1] + 0 == d[1] + 0 && f[2] + 0 > d[2] + 0)) {
      print "yes"
    } else {
      print "no"
    }
  }')"
  if [ "${outranks}" != "yes" ]; then
    fail "telemetrytest requires ${sibling}@${fabricated_version}, which no longer outranks the declared ${newest_declared} - raise the fabricated scheme (see telemetrytest/go.mod)."
  fi
done <<< "${fabricated_requires}"

if [ "${failures}" -gt 0 ]; then
  echo "${failures} release-consistency check(s) failed." >&2
  exit 1
fi
echo "✅ Release bookkeeping is consistent."
