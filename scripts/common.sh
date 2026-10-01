# Shared helpers for the repository's build and check scripts. This file is
# sourced, not executed, so it defines functions only and inherits the caller's
# `set` options. Source it from a script that runs from the repository root:
#
#   source "$(dirname "$0")/common.sh"

# Enumerate the workspace module directories listed in go.work - the modules we
# publish - one per line. go.work is the single source of truth for which modules
# exist, so scripts consume this instead of hard-coding `(. core postgrest)`.
# Returns non-zero and prints nothing when the workspace lists no modules, so a
# caller capturing the output under `set -e` stops rather than iterating nothing.
enumerate_workspace_modules() {
  local dirs
  dirs="$(go work edit -json | jq -r '.Use[].DiskPath')"
  [ -n "${dirs}" ] || return 1
  printf '%s\n' "${dirs}"
}

# Enumerate the non-published module directories carrying integration-test
# code: each workspace module's adjacent integrationtest module plus their
# shared integration-testing/testkit fixtures module. These sit outside go.work
# (the workspace is exactly the published set), so workspace enumeration never
# reaches them; scripts visiting them must run the go tool with GOWORK=off so
# their replace directives resolve the SDK modules from the local tree.
enumerate_adjacent_test_modules() {
  printf '%s\n' \
    auth/integrationtest \
    postgrest/integrationtest \
    supabase/integrationtest \
    integration-testing/testkit
}

# Enumerate the non-published example program modules under examples/. Like
# the adjacent test modules they sit outside go.work, so the go tool visits
# them with GOWORK=off and their replace directives resolve the SDK modules
# from the local tree. scripts/integration-test.sh additionally runs each one
# against the local stack.
enumerate_example_modules() {
  printf '%s\n' \
    examples/rls-backend \
    examples/database-standalone \
    examples/tracing-otel
}

# Enumerate every non-published consumer-shaped module: the adjacent test
# modules, the example programs and the telemetrytest consumer probe. All but
# integration-testing/testkit require SDK modules and resolve them from the
# local tree through replace directives, so scripts/prepare-release.sh
# re-tidies each one after pinning a published module's sibling requires,
# keeping their recorded versions consistent with the new module graph
# (testkit rides along from the adjacent set; its tidy is a no-op).
enumerate_consumer_shaped_modules() {
  enumerate_adjacent_test_modules
  enumerate_example_modules
  printf '%s\n' telemetrytest
}

# Print the versions a published module's changelog declares, one per
# well-formed '## vX.Y.Z - YYYY-MM-DD' heading, newest first (headings are
# prepended by scripts/prepare-release.sh, whose stamp is the declaration of
# a release). $1 is the module directory. Malformed headings print nothing
# here and are caught by scripts/check-release-consistency.sh.
declared_module_versions() {
  sed -En 's/^## (v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?) - [0-9]{4}-[0-9]{2}-[0-9]{2}$/\1/p' "$1/CHANGELOG.md"
}

# Print how many entry lines sit under a published module's '## Unreleased'
# heading, 0 when the heading is absent. Every line before the next section
# counts unless it is empty or holds only whitespace. Initial-release entries
# are plain paragraphs, later ones grouped bullets. $1 is the module directory.
unreleased_entry_lines() {
  awk '
    $0 == "## Unreleased" { in_unreleased = 1; next }
    /^## / { in_unreleased = 0 }
    in_unreleased && /[^[:space:]]/ { entry_lines++ }
    END { print entry_lines + 0 }
  ' "$1/CHANGELOG.md"
}
