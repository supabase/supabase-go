#!/usr/bin/env bash
# Assert three properties of the module layout, the first two of the published
# module graph, which is exactly the go.work workspace - the modules that ship
# as our public API surface. Run by CI and locally, identically, from the
# repository root.
#
# Resolvability: every first-party `require` in a published module names
# another published module. The committed go.work overlay resolves every
# sibling from local source, so a `require` naming a first-party path that is
# not a published module (a typo, or a module that is not published) builds and
# tests green in-repo and would only break consumers once real tags exist.
# tools/go, telemetrytest, integration-testing/testkit and the integrationtest
# modules are never published, so they are out of scope.
#
# Layering: the require graph must stay the strict DAG the module structure
# promises - core requires no first-party module, a domain module requires only
# core and only the root supabase module composes the domains. A sibling or
# root require creeping into a domain module would force consumers of that one
# domain to pull in modules they did not ask for.
#
# Containment: the repository root carries no go.mod, and every Go source file
# lives inside a module directory. The go command treats a repository root
# without go.mod as an implicit module, so a Go file outside every module
# would be a package of that synthesized root module, and pkg.go.dev - which
# refuses a module with no packages - would then render a root page carrying
# the repository README. A root go.mod would make that module explicit and
# put every stray Go file legitimately inside it, so it is refused first.
set -euo pipefail

source "$(dirname "$0")/common.sh"

echo "Module Path Check..."

prefix="github.com/supabase/supabase-go"

# The module path a go.mod file declares.
module_path_of() {
  go mod edit -json "$1" | jq -r '.Module.Path'
}

# The first-party module paths a go.mod file requires, one per line.
first_party_requires_of() {
  go mod edit -json "$1" | jq -r --arg p "${prefix}" '
    (.Require // [])[] | select(.Path == $p or (.Path | startswith($p + "/"))) | .Path
  '
}

# The workspace lists exactly the modules we publish: it is both the set we scan
# and, resolved to module paths, the set of paths a published `require` may name.
workspace_modules="$(enumerate_workspace_modules)"

published=""
for dir in ${workspace_modules}; do
  published="${published}$(module_path_of "${dir}/go.mod")"$'\n'
done

# The resolvability leg. replace directives are irrelevant: a consumer ignores
# replace directives in its dependencies' go.mod files, so they never affect
# what resolves.
offenders=""
for dir in ${workspace_modules}; do
  while IFS= read -r target; do
    [ -n "${target}" ] || continue
    if ! printf '%s' "${published}" | grep -Fxq -- "${target}"; then
      offenders="${offenders}  ${dir}/go.mod: require ${target}"$'\n'
    fi
  done < <(first_party_requires_of "${dir}/go.mod")
done

if [ -n "${offenders}" ]; then
  echo "These published modules require a first-party path that is not a published module:" >&2
  printf '%s' "${offenders}" >&2
  echo "A consumer's build could not resolve these. Fix the require path (typo?)," >&2
  echo "or publish the module and add it to go.work." >&2
  exit 1
fi

# The layering leg: everything below the root may require only core.
core_module="${prefix}/core"
root_module="${prefix}/supabase"
violations=""
for dir in ${workspace_modules}; do
  [ "$(module_path_of "${dir}/go.mod")" = "${root_module}" ] && continue
  while IFS= read -r target; do
    [ -n "${target}" ] || continue
    if [ "${target}" != "${core_module}" ]; then
      violations="${violations}  ${dir}/go.mod: require ${target}"$'\n'
    fi
  done < <(first_party_requires_of "${dir}/go.mod")
done

if [ -n "${violations}" ]; then
  echo "These modules break the require DAG (core stands alone, domains require only core, only the root composes domains):" >&2
  printf '%s' "${violations}" >&2
  echo "Move the dependency: cross-domain needs belong in core, and composition belongs in the root supabase module." >&2
  exit 1
fi

# The containment leg. The root go.mod check comes first because with one in
# place every Go file would count as inside a module.
if [ -f go.mod ]; then
  echo "The repository root holds a go.mod, which makes the root itself a module - the one that would carry the repository README onto pkg.go.dev." >&2
  echo "Remove it: every module lives in its own subdirectory, listed in go.work." >&2
  exit 1
fi

# A directory is inside a module when it or an ancestor holds a go.mod, which
# is also the rule module zips follow: a nested module's directory is excluded
# from every enclosing module's zip.
strays=""
while IFS= read -r file; do
  dir="$(dirname "${file}")"
  until [ -f "${dir}/go.mod" ] || [ "${dir}" = "." ]; do
    dir="$(dirname "${dir}")"
  done
  [ -f "${dir}/go.mod" ] || strays="${strays}  ${file}"$'\n'
done < <(git ls-files -- '*.go')

if [ -n "${strays}" ]; then
  echo "These Go files live outside every module directory:" >&2
  printf '%s' "${strays}" >&2
  echo "They would form a package of the implicit repository-root module and give pkg.go.dev a root page to render. Move them into a module." >&2
  exit 1
fi

echo "✅ Module Path Check Passed."
