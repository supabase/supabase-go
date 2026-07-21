#!/usr/bin/env bash
# Assert that the modules we publish will resolve for a consumer: every
# first-party `require` in a published module names another published module. The
# published set is exactly the go.work workspace - the modules that ship as our
# public API surface. Run by CI and locally, identically, from the repository root.
#
# Why this needs a guard: the committed go.work overlay resolves every sibling
# from local source, so a `require` naming a first-party path that is not a
# published module (a typo, or a module that is not published) builds and tests
# green in-repo and would only break consumers once real tags exist. tools/go,
# tools/node and telemetrytest are never published, so they are out of scope.
set -euo pipefail

source "$(dirname "$0")/common.sh"

echo "Module Path Check..."

prefix="github.com/supabase/supabase-go"

# The workspace lists exactly the modules we publish: it is both the set we scan
# and, resolved to module paths, the set of paths a published `require` may name.
workspace_modules="$(enumerate_workspace_modules)"

published=""
for dir in ${workspace_modules}; do
  module_path="$(go mod edit -json "${dir}/go.mod" | jq -r '.Module.Path')"
  published="${published}${module_path}"$'\n'
done

# Every first-party require in a published module must name a published module.
# replace directives are irrelevant: a consumer ignores replace directives in its
# dependencies' go.mod files, so they never affect what resolves.
offenders=""
for dir in ${workspace_modules}; do
  while IFS= read -r target; do
    [ -n "${target}" ] || continue
    if ! printf '%s' "${published}" | grep -Fxq -- "${target}"; then
      offenders="${offenders}  ${dir}/go.mod: require ${target}"$'\n'
    fi
  done < <(
    go mod edit -json "${dir}/go.mod" | jq -r --arg p "${prefix}" '
      (.Require // [])[] | select(.Path == $p or (.Path | startswith($p + "/"))) | .Path
    '
  )
done

if [ -n "${offenders}" ]; then
  echo "These published modules require a first-party path that is not a published module:" >&2
  printf '%s' "${offenders}" >&2
  echo "A consumer's build could not resolve these. Fix the require path (typo?)," >&2
  echo "or publish the module and add it to go.work." >&2
  exit 1
fi

echo "✅ Module Path Check Passed."
