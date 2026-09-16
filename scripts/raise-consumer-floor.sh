#!/usr/bin/env bash
# Raise the consumer floor to the next Go minor version: bump every artefact
# that carries the floor - go.work, the floor-carrying go.mod files and CI's
# floor matrix legs - in lockstep, then commit the result. Run locally from
# the repository root. Fails before changing anything if the working tree is
# not clean or the floor statements do not already agree.
set -euo pipefail

source "$(dirname "$0")/common.sh"
workspace_modules="$(enumerate_workspace_modules)"

ci_workflow=".github/workflows/ci.yml"

# Every go.mod stating the floor: the published (workspace) modules plus the
# non-published consumer-shaped modules, whose go lines must be at least the
# published modules' because they require them (go.dev/ref/mod#go-mod-file-go).
floor_gomods="$(
  for module in ${workspace_modules}; do
    printf '%s\n' "${module#./}/go.mod"
  done
  printf '%s\n' \
    */integrationtest/go.mod \
    examples/*/go.mod \
    telemetrytest/go.mod \
    integration-testing/testkit/go.mod
)"

echo "Raise Consumer Floor..."

if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
  echo "The working tree has staged or unstaged changes; commit or stash them first." >&2
  exit 1
fi

# One "<location> <floor>" line per floor statement, reduced to major.minor so
# toolchain-written point granularity does not read as drift.
floor_statements() {
  printf '%s %s\n' "go.work" "$(go work edit -json | jq -r '.Go' | cut -d. -f1,2)"
  local gomod
  for gomod in ${floor_gomods}; do
    printf '%s %s\n' "${gomod}" "$(go mod edit -json "${gomod}" | jq -r '.Go' | cut -d. -f1,2)"
  done
  sed -n "s|^.*go-version: \[\"\([0-9.]*\)\", \"stable\"\].*$|${ci_workflow} \1|p" "${ci_workflow}"
}

statements="$(floor_statements)"

if [ "$(printf '%s\n' "${statements}" | grep -c "^${ci_workflow} ")" -ne 2 ]; then
  echo "Expected exactly two floor matrix legs in ${ci_workflow}. Align it with this script by hand, then re-run:" >&2
  printf '%s\n' "${statements}" >&2
  exit 1
fi

current_floor="$(printf '%s\n' "${statements}" | awk '{print $2}' | sort -u)"
if [ "$(printf '%s\n' "${current_floor}" | wc -l)" -ne 1 ]; then
  echo "The consumer floor is not aligned across its statements. Fix these by hand, then re-run:" >&2
  printf '%s\n' "${statements}" >&2
  exit 1
fi

new_floor="${current_floor%%.*}.$(( ${current_floor#*.} + 1 ))"

go work edit -go="${new_floor}"
for gomod in ${floor_gomods}; do
  go mod edit -go="${new_floor}" "${gomod}"
done

# BSD and GNU sed disagree on -i, so rewrite via a temporary file.
sed "s|go-version: \[\"${current_floor//./\\.}\", \"stable\"\]|go-version: [\"${new_floor}\", \"stable\"]|" \
  "${ci_workflow}" > "${ci_workflow}.tmp"
mv "${ci_workflow}.tmp" "${ci_workflow}"

git commit --all --message="Bump consumer floor from Go version ${current_floor} to ${new_floor}."

echo "✅ Consumer floor raised from Go ${current_floor} to Go ${new_floor}."
