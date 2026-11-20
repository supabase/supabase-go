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

# Succeed when $1 names one of the workspace module directories, written
# without the leading ./ that enumerate_workspace_modules prints, such as core.
is_workspace_module() {
  local candidate="$1" module
  for module in $(enumerate_workspace_modules); do
    if [ "${module#./}" = "${candidate}" ]; then
      return 0
    fi
  done
  return 1
}

# Enumerate the non-published module directories carrying integration-test
# code: each workspace module's adjacent integrationtest module plus their
# shared integration-testing/testkit fixtures module. These sit outside go.work
# (the workspace is exactly the published set), so workspace enumeration never
# reaches them; scripts visiting them must run the go tool with GOWORK=off so
# their replace directives resolve the SDK modules from the local tree.
enumerate_adjacent_test_modules() {
  printf '%s\n' \
    integrationtest \
    auth/integrationtest \
    postgrest/integrationtest \
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
    examples/tracing-otel \
    examples/admin-provisioning \
    examples/auth-session-service \
    examples/premium-access-api
}

# Enumerate every non-published consumer-shaped module: the adjacent test
# modules, the example programs and the telemetrytest consumer probe. All but
# integration-testing/testkit require SDK modules and resolve them from the
# local tree through replace directives, so scripts/prepare-release.sh
# raises each one's require of the module being released and re-tidies it,
# keeping their recorded versions consistent with the new module graph, and
# scripts/check-release-consistency.sh fails when one records a released
# sibling behind its newest release (testkit rides along from the adjacent
# set; it requires no SDK module, so both are no-ops for it).
enumerate_consumer_shaped_modules() {
  enumerate_adjacent_test_modules
  enumerate_example_modules
  printf '%s\n' telemetrytest
}

# Print the module path the published modules share as a base: the root
# module's path itself, and the parent of every subdirectory module's path.
module_path_base() {
  printf '%s\n' 'github.com/supabase/supabase-go'
}

# Print a workspace module's module path. $1 is the module directory as
# go.work lists it, with or without the leading ./: '.' for the root module,
# which is the base itself, or a subdirectory such as core.
module_path_for() {
  local directory="${1#./}"
  if [ "${directory}" = "." ]; then
    module_path_base
  else
    printf '%s/%s\n' "$(module_path_base)" "${directory}"
  fi
}

# Print what a workspace module's release tags carry before the version:
# nothing for the root module, since its path is the repository, otherwise
# the module directory and a slash (https://go.dev/ref/mod#vcs-version). $1
# is the module directory.
release_tag_prefix_for() {
  local directory="${1#./}"
  if [ "${directory}" != "." ]; then
    printf '%s/' "${directory}"
  fi
  echo
}

# Print the Git tag that releases a workspace module version. $1 is the
# module directory, $2 the version.
release_tag_for() {
  printf '%s%s\n' "$(release_tag_prefix_for "$1")" "$2"
}

# Print the first-party modules a module requires, one 'directory version'
# line per require, the directory as go.work would list it: '.' for the root
# module, otherwise the path below the base, such as core. $1 is the
# requiring module's directory.
sibling_requires_of() {
  go mod edit -json "$1/go.mod" | jq -r --arg base "$(module_path_base)" '
    (.Require // [])[]
    | select(.Path == $base or (.Path | startswith($base + "/")))
    | (if .Path == $base then "." else (.Path | ltrimstr($base + "/")) end) + " " + .Version
  '
}

# Print the package directories of the module in the current directory, one
# per line and relative to it ('.' for the module root): the directories the
# go command attributes this module's packages to, so nested modules and
# directories the go.mod ignore directive names are absent. For tools that
# walk the filesystem rather than load packages, which would otherwise cross
# those boundaries.
module_package_dirs() {
  go list -f '{{.Module.Path}} {{.ImportPath}}' ./... | awk '
    $2 == $1 { print "."; next }
    { print "./" substr($2, length($1) + 2) }
  '
}

# Print the Go files in the module's package directories (module_package_dirs),
# one per line, test files and build-constrained files included.
module_go_files() {
  local dir
  module_package_dirs | while read -r dir; do
    find "${dir}" -maxdepth 1 -type f -name '*.go'
  done
}

# Print the pattern matching a SemVer version with the leading 'v' Go module
# versions require and an optional pre-release suffix (vX.Y.Z or vX.Y.Z-pre):
# the form versions take in changelog headings, dep=version pin arguments and
# the version half of a release tag. Extended regular expression syntax,
# anchored at both ends.
semver_version_pattern() {
  printf '%s\n' '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'
}

# Print the pattern matching a well-formed changelog version heading,
# '## `vX.Y.Z` (YYYY-MM-DD)' as scripts/prepare-release.sh stamps it, in the
# extended regular expression syntax of grep -E and sed -E, capturing the
# version as its first group.
version_heading_pattern() {
  printf '%s\n' '^## `(v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?)` \([0-9]{4}-[0-9]{2}-[0-9]{2}\)$'
}

# Print the pattern matching a version heading in tag form,
# '## `<prefix>/vX.Y.Z` (YYYY-MM-DD)': a release a module made before it
# moved, under an earlier tag prefix, kept in its changelog as history.
# Such a heading declares no version of the module's current path, so
# declared_module_versions never prints it. Same syntax as
# version_heading_pattern.
earlier_prefix_version_heading_pattern() {
  printf '%s\n' '^## `([A-Za-z0-9._-]+/)v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?` \([0-9]{4}-[0-9]{2}-[0-9]{2}\)$'
}

# Print the versions a published module's changelog declares, one per
# well-formed version heading, newest first (headings are prepended by
# scripts/prepare-release.sh, whose stamp is the declaration of a release).
# $1 is the module directory. Malformed headings print nothing here and are
# caught by scripts/check-release-consistency.sh.
declared_module_versions() {
  sed -En "s/$(version_heading_pattern)/\\1/p" "$1/CHANGELOG.md"
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

# Resolve a commit reference to its full SHA, HEAD when the reference is
# empty, failing with guidance when it names nothing in this clone. $1 is
# the reference, typically a script's optional argument naming a release
# PR's landing commit.
resolve_release_commit() {
  local reference="${1:-HEAD}"
  if ! git rev-parse --verify --quiet "${reference}^{commit}"; then
    echo "'${reference}' does not name a commit in this clone. Fetch first, or check the reference." >&2
    return 1
  fi
}

# Print the releases the remote's tags declare at a commit, one
# 'module version' line per workspace module (per the current checkout's
# go.work) whose release tag on origin points at that commit, in go.work
# order. Reads origin rather than local tags, so the result
# reflects what the release-tags workflow actually pushed, and prints
# nothing when no release tag points at the commit - callers decide whether
# that is an error. Returns non-zero when a module carries several tags at
# the commit or a tag's version half is not SemVer. $1 is the full commit
# SHA.
released_versions_at() {
  local commit_sha="$1" remote_tags module prefix version
  remote_tags="$(git ls-remote --tags origin \
    | awk -v sha="${commit_sha}" '
        $1 == sha { tag = $2; sub(/^refs\/tags\//, "", tag); sub(/\^\{\}$/, "", tag); print tag }
      ' \
    | sort -u)"
  for module in $(enumerate_workspace_modules); do
    module="${module#./}"
    prefix="$(release_tag_prefix_for "${module}")"
    version="$(printf '%s\n' "${remote_tags}" | sed -n "s|^${prefix}\(v[^/]*\)$|\1|p")"
    [ -n "${version}" ] || continue
    if [ "$(printf '%s\n' "${version}" | wc -l)" -ne 1 ]; then
      echo "Several release tags of ${module} on origin point at ${commit_sha}: $(printf '%s\n' "${version}" | tr '\n' ' ')" >&2
      return 1
    fi
    if ! printf '%s' "${version}" | grep -Eq "$(semver_version_pattern)"; then
      echo "Tag $(release_tag_for "${module}" "${version}") on origin points at ${commit_sha} but does not carry a SemVer version." >&2
      return 1
    fi
    printf '%s %s\n' "${module}" "${version}"
  done
}

# Download one module version into the caller's GOMODCACHE with
# 'go mod download -json', writing the JSON report to $2 and surfacing the
# report's Error field as a failure. The caller's environment picks the
# route: GOPROXY=direct fetches from the VCS origin, a proxy URL fetches
# through that proxy. $1 is a 'modulepath@version' reference.
download_module_json() {
  go mod download -json "$1" > "$2" || true
  local error
  error="$(jq -r '.Error // empty' "$2")"
  if [ -n "${error}" ]; then
    echo "${error}" >&2
    return 1
  fi
}

# Print go.sum-shaped hash lines (the zip hash line, then the go.mod hash
# line, per module) from the given 'go mod download -json' reports, in
# argument order. Two runs that downloaded the same versions by different
# routes must print identical lines, or the routes served different content.
module_hash_lines() {
  jq -r '"\(.Path) \(.Version) \(.Sum)", "\(.Path) \(.Version)/go.mod \(.GoModSum)"' "$@"
}

# Print the path of the record a release script leaves of its hash lines:
# at the repository root, git-ignored, named for the script so the two
# routes' records sit side by side for scripts/seed-release.sh or a diff by
# hand to compare. $1 is the script's name without its extension.
hash_record_path() {
  printf '%s/.%s-hashes.txt\n' "$(git rev-parse --show-toplevel)" "$1"
}

# Report progress on stdout and, when running in GitHub Actions, on the
# workflow run's summary page, where a release script's outcome matters as
# much as its log.
note() {
  echo "$*"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    echo "$*" >> "${GITHUB_STEP_SUMMARY}"
  fi
}

# Strip git ls-remote decoration from stdin, printing bare tag names: the
# object SHA and tab go, the refs/tags/ prefix goes and an annotated tag's
# peeled ^{} suffix goes, then duplicates collapse.
tag_names() {
  cut -f2 | sed -E 's|^refs/tags/||; s|\^\{\}$||' | sort -u
}

# Print the versions, prefix removed, of the stdin tag names formed of the
# given prefix followed by a three-part version (prefix 'v' matches v7.0.0).
# Other shapes - moving major tags like v7, pre-releases - are dropped.
# $1 is the prefix.
versions_with_prefix() {
  awk -v prefix="$1" '
    index($0, prefix) == 1 {
      version = substr($0, length(prefix) + 1)
      if (version ~ /^[0-9]+\.[0-9]+\.[0-9]+$/) print version
    }'
}

# Print the newest of the three-part versions fed on stdin, one per line.
# Numeric field sort rather than sort -V, which macOS's BSD sort lacks.
newest_version() {
  sort -t. -k1,1n -k2,2n -k3,3n | tail -n 1
}

# Print the newest tag on a remote repository formed of the given prefix
# followed by a three-part version, and the commit it points at, as
# 'tag commit'. For an annotated tag, that is the commit the tag peels to.
# Probes that live origin, so network access is required. $1 is the
# repository URL, $2 the prefix.
newest_tag_with_commit() {
  local repository_url="$1" tag_prefix="$2" tag_listing newest_tag
  tag_listing="$(git ls-remote --tags "${repository_url}" "${tag_prefix}*")"
  newest_tag="${tag_prefix}$(printf '%s\n' "${tag_listing}" | tag_names | versions_with_prefix "${tag_prefix}" | newest_version)"
  printf '%s\n' "${tag_listing}" | awk -v tag="${newest_tag}" -v peeled="refs/tags/${newest_tag}^{}" -v plain="refs/tags/${newest_tag}" '
    $2 == peeled { peeled_commit = $1 }
    $2 == plain { plain_commit = $1 }
    END { print tag, (peeled_commit != "" ? peeled_commit : plain_commit) }'
}

# Print the path of the script pinning the Supabase CLI for the integration
# tests - the single source of truth for that pin: SUPABASE_CLI_VERSION plus
# one cli_sha256 value per supported platform.
supabase_cli_pin_file() {
  printf '%s\n' 'scripts/integration-test.sh'
}

# Print the pinned Supabase CLI version declared in the pin file.
pinned_supabase_cli_version() {
  sed -nE 's/^SUPABASE_CLI_VERSION="([0-9.]+)"$/\1/p' "$(supabase_cli_pin_file)"
}

# Print the newest Supabase CLI release version, discovered from the version
# tags on github.com/supabase/cli. Probes that live origin, so network access
# is required.
latest_supabase_cli_version() {
  git ls-remote --tags https://github.com/supabase/cli 'v*' | tag_names | versions_with_prefix 'v' | newest_version
}

# Succeed when tools/node/node_modules holds the package tree that
# tools/node/package-lock.json pins. npm records each tree it installs in
# node_modules/.package-lock.json, and the two must list the same packages at
# the same paths, each at the same version and integrity. The lockfile's root
# entry describes tools/node itself and takes no part. Otherwise, including
# when nothing is installed, print the command that installs the pinned tree
# and return non-zero, so a caller under `set -e` stops.
require_pinned_node_tools() {
  if ! jq -e -n \
    --slurpfile locked tools/node/package-lock.json \
    --slurpfile installed tools/node/node_modules/.package-lock.json \
    'def tree: .packages | del(."") | map_values({version, integrity});
     ($locked[0] | tree) == ($installed[0] | tree)' > /dev/null 2>&1; then
    echo "tools/node dependencies are missing or differ from tools/node/package-lock.json. Run this, then retry:" >&2
    echo "  (cd tools/node && npm ci)" >&2
    return 1
  fi
}
