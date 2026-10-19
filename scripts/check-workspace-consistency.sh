#!/usr/bin/env bash
# Assert that every nested workspace loads: each go.work below the repository
# root must carry a go line at least as new as the go directive of every
# module it uses, or every workspace-mode go command in that directory is
# refused. The tool-module bump routes operate GOWORK=off, which never
# performs such a load, so a bump that raises a module's go directive can
# otherwise leave the sibling go.work behind. The root go.work needs no probe
# here: every workspace-mode build and test already loads it. Run by CI and
# locally, identically, from the repository root.
set -euo pipefail

echo "Workspace Consistency Check..."

status=0
while IFS= read -r workspace_file; do
  workspace_directory="$(dirname "${workspace_file}")"
  if ! go -C "${workspace_directory}" list -m > /dev/null; then
    echo "${workspace_directory} refuses to load; if its go.work go line lags a module's go directive, fix with: go -C ${workspace_directory} work use"
    status=1
  fi
done < <(find . -mindepth 2 \( -path './.*' -o -name node_modules -o -name user.transient \) -prune -o -name go.work -print)

if [ "${status}" -ne 0 ]; then
  exit "${status}"
fi

echo "✅ Workspace Consistency Check Passed."
