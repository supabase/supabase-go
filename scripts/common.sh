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
