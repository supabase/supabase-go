#!/usr/bin/env bash
# Push the release tags a merged release PR declared: for every published
# module whose changelog's newest version heading has no corresponding
# '<module>/<version>' tag on the remote, create that tag at HEAD and push
# it, in dependency order so a consumer never resolves a tagged module whose
# sibling requirement is still untagged. Existing tags are left alone, so
# re-running at the same commit pushes exactly the tags still missing and an
# ordinary (non-release) commit is a no-op. The release-tags workflow runs
# this on every push to main; run it locally from a clean checkout of the
# release commit as the fallback (see RELEASING.md).
set -euo pipefail

source "$(dirname "$0")/common.sh"
workspace_modules="$(enumerate_workspace_modules)"

module_base="$(module_path_base)"

echo "Push Release Tags..."

if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
  echo "The working tree has staged or unstaged changes; run this from a clean checkout of the release commit." >&2
  exit 1
fi

# Reports progress on stdout and, when running in GitHub Actions, on the
# workflow run's summary page, where an obvious no-release statement matters
# as much as a list of what was tagged.
note() {
  echo "$*"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    echo "$*" >> "${GITHUB_STEP_SUMMARY}"
  fi
}

sibling_requires() {
  go mod edit -json "$1/go.mod" \
    | jq -r --arg base "${module_base}/" \
      '.Require[]? | select(.Path | startswith($base)) | .Path | ltrimstr($base)'
}

# Order the modules so every module follows the siblings it requires, in
# rounds of "everything whose requirements are already ordered". go.work
# order breaks ties, and check-module-paths.sh holds the require graph to a
# DAG, so a round that orders nothing means a require outside the workspace.
remaining="$(printf '%s\n' ${workspace_modules} | sed 's|^\./||' | tr '\n' ' ')"
ordered=""
while [ -n "${remaining// /}" ]; do
  advanced=""
  deferred=""
  for module in ${remaining}; do
    ready="yes"
    for sibling in $(sibling_requires "${module}"); do
      case " ${ordered} " in
        *" ${sibling} "*) ;;
        *) ready="no" ;;
      esac
    done
    if [ "${ready}" = "yes" ]; then
      ordered="${ordered} ${module}"
      advanced="yes"
    else
      deferred="${deferred} ${module}"
    fi
  done
  if [ -z "${advanced}" ]; then
    echo "Cannot order these modules by their sibling requires:${deferred}" >&2
    exit 1
  fi
  remaining="${deferred}"
done

head_sha="$(git rev-parse HEAD)"
tagged_any=""
for module in ${ordered}; do
  version="$(declared_module_versions "${module}" | head -n 1)"
  if [ -z "${version}" ]; then
    note "- \`${module}\`: no released version declared, nothing to tag."
    continue
  fi
  tag="${module}/${version}"
  if git ls-remote --exit-code origin "refs/tags/${tag}" > /dev/null 2>&1; then
    note "- \`${module}\`: ${version} is already tagged."
    continue
  fi
  # A leftover local tag pointing elsewhere would win over the tag this run
  # means to create, so it is an error rather than something to overwrite.
  if git rev-parse -q --verify "refs/tags/${tag}" > /dev/null; then
    if [ "$(git rev-parse "refs/tags/${tag}^{commit}")" != "${head_sha}" ]; then
      echo "Local tag ${tag} exists but does not point at HEAD; delete it and re-run." >&2
      exit 1
    fi
  else
    git tag "${tag}"
  fi
  git push origin "refs/tags/${tag}"
  note "- \`${module}\`: tagged \`${tag}\` at ${head_sha}."
  tagged_any="yes"
done

note ""
if [ -z "${tagged_any}" ]; then
  note "**No release detected**: every declared version is already tagged, so this run pushed nothing."
else
  note "**Release tags pushed.** Run scripts/verify-release.sh next, per RELEASING.md."
fi
