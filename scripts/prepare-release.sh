#!/usr/bin/env bash
# Prepare one published module for release: pin its sibling requires to the
# given published versions, stamp its changelog, then re-tidy the non-published
# modules so their recorded versions match the new module graph. Everything it
# does is reviewable working-tree change - it never commits, tags or pushes.
# The release-tags workflow pushes the tag after the release PR merges, reading
# the changelog stamp this script writes. See RELEASING.md for the full,
# dependency-ordered procedure.
set -euo pipefail

source "$(dirname "$0")/common.sh"
workspace_modules="$(enumerate_workspace_modules)"

module_base="$(module_path_base)"

if [ "$#" -lt 2 ]; then
  echo "usage: $0 <module> <version> [dep=version ...]" >&2
  echo "  module      a published module directory listed in go.work, e.g. core" >&2
  echo "  version     the version to release, e.g. v0.1.0-alpha.1" >&2
  echo "  dep=version a sibling require to pin, e.g. core=v0.1.0-alpha.1" >&2
  exit 2
fi
module_dir="$1"; shift
version="$1"; shift

echo "Prepare Release..."

is_workspace_module() {
  local candidate="$1" module
  for module in ${workspace_modules}; do
    if [ "${module#./}" = "${candidate}" ]; then
      return 0
    fi
  done
  return 1
}

if ! is_workspace_module "${module_dir}"; then
  echo "'${module_dir}' is not a published (go.work) module. Choose one of:" >&2
  printf '%s\n' "${workspace_modules}" >&2
  exit 1
fi

if ! printf '%s' "${version}" | grep -Eq "$(semver_version_pattern)"; then
  echo "'${version}' is not a SemVer tag version (want vX.Y.Z or vX.Y.Z-pre)." >&2
  exit 1
fi

# Major version 2 or higher lives at a new module path
# (go.dev/ref/mod#major-version-suffixes), which this procedure predates.
case "${version}" in
  v[01].*) ;;
  *)
    echo "'${version}' is major version 2 or higher, which needs a /vN module path suffix and a release procedure this script does not implement." >&2
    exit 1
    ;;
esac

for pair in "$@"; do
  case "${pair}" in
    *=*) ;;
    *)
      echo "'${pair}' is not of the form dep=version." >&2
      exit 1
      ;;
  esac
  dep="${pair%%=*}"
  dep_version="${pair#*=}"
  if ! is_workspace_module "${dep}" || [ "${dep}" = "${module_dir}" ]; then
    echo "'${dep}' is not a sibling published module of '${module_dir}'." >&2
    exit 1
  fi
  if ! printf '%s' "${dep_version}" | grep -Eq "$(semver_version_pattern)"; then
    echo "'${dep_version}' (pinning ${dep}) is not a SemVer tag version." >&2
    exit 1
  fi
done

if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
  echo "The working tree has staged or unstaged changes; commit or stash them first." >&2
  exit 1
fi

changelog="${module_dir}/CHANGELOG.md"
release_date="$(date -u +%F)"
version_heading="## \`${version}\` (${release_date})"

if [ "$(grep -cx '## Unreleased' "${changelog}")" -ne 1 ]; then
  echo "${changelog} needs exactly one '## Unreleased' section, holding the entries to release." >&2
  exit 1
fi
if declared_module_versions "${module_dir}" | grep -qx "${version}"; then
  echo "${changelog} already carries a section for ${version}." >&2
  exit 1
fi

if [ "$(unreleased_entry_lines "${module_dir}")" -eq 0 ]; then
  echo "${changelog} has nothing under '## Unreleased', and a release must have something to say." >&2
  exit 1
fi

for pair in "$@"; do
  dep="${pair%%=*}"
  dep_version="${pair#*=}"
  echo "==> pin ${module_base}/${dep} @ ${dep_version}"
  go mod edit -require="${module_base}/${dep}@${dep_version}" "${module_dir}/go.mod"
done

# A sibling still at the zero pseudo-version means a missing dep=version
# argument: the tag would ship a require no consumer can resolve.
if grep -q 'v0.0.0-00010101000000-000000000000' "${module_dir}/go.mod"; then
  echo "${module_dir}/go.mod still requires a sibling at the zero pseudo-version. Pass dep=version for every sibling this module requires:" >&2
  grep 'v0.0.0-00010101000000-000000000000' "${module_dir}/go.mod" >&2
  exit 1
fi

echo "==> stamp ${changelog}: '## Unreleased' becomes '${version_heading}'"
awk -v version_heading="${version_heading}" '
  $0 == "## Unreleased" { print version_heading; next }
  { print }
' "${changelog}" > "${changelog}.tmp"
mv "${changelog}.tmp" "${changelog}"

# The pinned requires change the module graph every non-published module
# sees, and an untidy one fails every build command under -mod=readonly.
# Their replace directives resolve the SDK from the local tree, so this
# works before any tag exists.
for consumer_module in $(enumerate_consumer_shaped_modules); do
  echo "==> go mod tidy   (GOWORK=off) ${consumer_module}"
  (cd "${consumer_module}" && GOWORK=off go mod tidy)
done

candidate_paths="${module_dir}/go.mod ${changelog}"
for consumer_module in $(enumerate_consumer_shaped_modules); do
  candidate_paths="${candidate_paths} ${consumer_module}/go.mod ${consumer_module}/go.sum"
done
changed_paths="$(git status --porcelain -- ${candidate_paths} | awk '{ print $NF }' | sort)"

echo
echo "✅ Prepared ${module_dir} for ${version}. Nothing has been committed."
echo
echo "Review the changes, then run:"
echo
echo "  git add$(printf ' %s' ${changed_paths})"
echo "  git commit --message='Prepare release ${module_dir}/${version}.'"
echo
echo "Prepare any further modules the same way, then before pushing the branch"
echo "and opening the release PR, run the pre-flight check:"
echo
echo "  ./scripts/check-release-consistency.sh"
echo
echo "When the PR merges, the release-tags workflow pushes ${module_dir}/${version}."
