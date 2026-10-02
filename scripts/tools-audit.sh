#!/usr/bin/env bash
# Report where the repository's externally sourced, version-pinned tooling
# sits against each tool's canonical origin: the GitHub Actions pins in
# .github/workflows/, the Go tool module in tools/go, the npm tool module in
# tools/node, the Supabase CLI pin in scripts/integration-test.sh and the Go
# consumer floor in go.work. Read-only and report-only: nothing is installed
# or modified, an available update is information rather than a failure and
# the exit status is non-zero only when the audit itself cannot complete.
# Probes live origins (GitHub tags, the Go module proxy, the npm registry,
# go.dev), so it needs network access and sits outside check-fast.sh and CI.
# Requires git, curl, jq, go and npm. Run from the repository root.
set -euo pipefail

echo "Tools Audit..."

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

# Upgrade-available conclusions render bold red when stdout is a terminal,
# and plain when piped or when NO_COLOR is set (https://no-color.org).
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  highlight=$'\033[1;31m'
  reset=$'\033[0m'
else
  highlight=""
  reset=""
fi

echo
echo "==> GitHub Actions pins (.github/workflows/)"
# One audit per distinct pin: 'uses: <owner>/<repo>[/<path>]@<sha> # <tag>'.
# The comment's tag decides the scheme: everything before the trailing
# three-part version is the prefix, so 'v7.0.0' audits v* tags and
# 'capability-matrix/v1.13.0' audits capability-matrix/v* tags. The newest
# matching tag's commit (the peeled ^{} entry when the tag is annotated) is
# compared against the pinned SHA - the SHA is the pin, so a stale version
# comment cannot fake currency.
grep -hoE 'uses: [A-Za-z0-9._/-]+@[0-9a-f]{40} # [^ ]+' .github/workflows/*.yml | sort -u \
  | while read -r _ pinned_reference _ pinned_tag; do
      action_path="${pinned_reference%@*}"
      pinned_sha="${pinned_reference#*@}"
      repository_url="https://github.com/$(printf '%s' "${action_path}" | cut -d/ -f1-2)"
      tag_prefix="$(printf '%s' "${pinned_tag}" | sed -E 's/[0-9]+\.[0-9]+\.[0-9]+$//')"
      tag_listing="$(git ls-remote --tags "${repository_url}" "${tag_prefix}*")"
      latest_version="$(printf '%s\n' "${tag_listing}" | tag_names | versions_with_prefix "${tag_prefix}" | newest_version)"
      latest_tag="${tag_prefix}${latest_version}"
      latest_sha="$(printf '%s\n' "${tag_listing}" | awk -v peeled="refs/tags/${latest_tag}^{}" -v plain="refs/tags/${latest_tag}" '
        $2 == peeled { peeled_sha = $1 }
        $2 == plain { plain_sha = $1 }
        END { print (peeled_sha != "" ? peeled_sha : plain_sha) }')"
      if [ "${latest_sha}" = "${pinned_sha}" ]; then
        echo "  ${action_path} ${pinned_tag}: ✅ current"
      else
        echo "  ${action_path} ${pinned_tag}: ${highlight}${latest_tag} available${reset}"
        echo "    pin: uses: ${action_path}@${latest_sha} # ${latest_tag}"
        echo "    release notes: ${repository_url}/releases"
      fi
    done

echo
echo "==> Go tooling (tools/go, pinned by go.mod + go.sum)"
# GOWORK=off go -C tools/go is the established form for this module. go list
# prints a line per module even when the template yields nothing, so blank
# lines are dropped. The report groups what go.mod declares - tool directives
# and direct requires - and reduces transitive dependencies to a count, since
# the go get forms below carry those along.
module_updates="$(GOWORK=off go -C tools/go list -m -u -f '{{if .Update}}{{.Path}} {{.Version}} {{.Update.Version}}{{end}}' all | sed '/^$/d')"
if [ -z "${module_updates}" ]; then
  echo "  ✅ all current"
else
  # A tool directive names a package path; the toolchain resolves the module
  # providing it (gopls, say, is its own module beneath golang.org/x/tools).
  tool_packages="$(GOWORK=off go -C tools/go mod edit -json | jq -r '.Tool[].Path')"
  tool_modules="$(GOWORK=off go -C tools/go list -f '{{with .Module}}{{.Path}}{{end}}' ${tool_packages} | sort -u)"
  direct_modules="$(GOWORK=off go -C tools/go mod edit -json | jq -r '.Require[] | select(.Indirect != true) | .Path')"
  declared_updates=0
  transitive_updates=0
  while read -r module_path current_version latest_version; do
    if printf '%s\n' "${tool_modules}" | grep -qxF "${module_path}"; then
      label="tool"
    elif printf '%s\n' "${direct_modules}" | grep -qxF "${module_path}"; then
      label="direct"
    else
      transitive_updates=$((transitive_updates + 1))
      continue
    fi
    declared_updates=$((declared_updates + 1))
    echo "  ${module_path} (${label}) ${current_version}: ${highlight}${latest_version} available${reset}"
    echo "    release notes: https://pkg.go.dev/${module_path}@${latest_version}"
  done <<< "${module_updates}"
  # Transitive-only updates still conclude current: minimum version selection
  # holds undeclared modules where the declared requirements put them, so they
  # move when a declared bump requires newer.
  if [ "${declared_updates}" -eq 0 ]; then
    echo "  ✅ all current (${transitive_updates} transitive module update(s) exist deeper in the graph)"
  else
    if [ "${transitive_updates}" -gt 0 ]; then
      echo "  plus ${transitive_updates} transitive module update(s), raised only as far as the declared modules require"
    fi
    echo "  bump every tool: GOWORK=off go -C tools/go get tool && GOWORK=off go -C tools/go mod tidy"
    echo "  bump one module: GOWORK=off go -C tools/go get <module>@latest && GOWORK=off go -C tools/go mod tidy"
    echo "  afterwards run: ./scripts/lint.sh && ./scripts/vulncheck.sh"
  fi
fi

echo
echo "==> Node tooling (tools/node, pinned by package.json + package-lock.json)"
# npm runs with tools/node as its working directory rather than via --prefix
# from the repository root: per-directory Node version managers resolve the
# npm shim against the working directory, and tools/node carries the Node
# version pin (.tool-versions).
# npm outdated exits 0 only when everything is current and exits non-zero
# both when packages are outdated and when npm itself cannot run, so the two
# non-zero cases are told apart by the output: a genuine outdated report is a
# non-empty JSON object with no "error" key. Anything else fails the audit
# rather than passing as currency. Only direct dependencies are reported; the
# lockfile tree follows them on npm install.
npm_status=0
outdated_json="$(cd tools/node && npm outdated --json)" || npm_status=$?
if [ "${npm_status}" -eq 0 ]; then
  echo "  ✅ all current"
elif printf '%s\n' "${outdated_json}" | jq -e 'type == "object" and (has("error") | not) and length > 0' > /dev/null 2>&1; then
  printf '%s\n' "${outdated_json}" | jq -r --arg highlight "${highlight}" --arg reset "${reset}" \
    'to_entries[] | "  \(.key) \(.value.current // "not installed"): \($highlight)\(.value.latest) available\($reset)\n    release notes: https://www.npmjs.com/package/\(.key)?activeTab=versions\n    bump: (cd tools/node && npm install --save-dev --save-exact \(.key)@\(.value.latest))"'
  echo "  afterwards run: ./scripts/spell-check.sh"
else
  echo "npm outdated did not produce an outdated report (exit ${npm_status}); see its message above." >&2
  exit 1
fi

echo
echo "==> Supabase CLI pin (scripts/integration-test.sh)"
pinned_cli_version="$(sed -nE 's/^SUPABASE_CLI_VERSION="([0-9.]+)"$/\1/p' scripts/integration-test.sh)"
latest_cli_version="$(git ls-remote --tags https://github.com/supabase/cli 'v*' | tag_names | versions_with_prefix 'v' | newest_version)"
if [ "${pinned_cli_version}" = "${latest_cli_version}" ]; then
  echo "  supabase/cli ${pinned_cli_version}: ✅ current"
else
  echo "  supabase/cli ${pinned_cli_version}: ${highlight}${latest_cli_version} available${reset}"
  echo "    procedure: DEVELOPMENT.md, 'Upgrading the pinned Supabase CLI'"
  echo "    release notes: https://github.com/supabase/cli/releases"
fi

echo
echo "==> Go toolchain"
# go.dev's download feed lists the newest release of each stable branch the
# Go project supports; the consumer floor policy tracks the oldest of those
# majors (DEVELOPMENT.md, 'Raising the Go consumer floor').
consumer_floor="$(sed -nE 's/^go ([0-9]+\.[0-9]+)$/\1/p' go.work)"
supported_releases="$(curl -fsSL 'https://go.dev/dl/?mode=json' | jq -r '.[] | select(.stable) | .version')"
latest_release="go$(printf '%s\n' "${supported_releases}" | sed 's/^go//' | newest_version)"
oldest_supported_major="$(printf '%s\n' "${supported_releases}" | sed -E 's/^go//; s/\.[0-9]+$//' | sort -t. -k1,1n -k2,2n | head -n 1)"
echo "  latest stable: ${latest_release} (local toolchain: $(go version | awk '{print $3}'))"
if [ "${consumer_floor}" = "${oldest_supported_major}" ]; then
  echo "  consumer floor ${consumer_floor}: ✅ current (matches the oldest supported major)"
else
  echo "  consumer floor ${consumer_floor}: ${highlight}trails the oldest supported major ${oldest_supported_major}${reset}"
  echo "    procedure: ./scripts/raise-consumer-floor.sh (DEVELOPMENT.md, 'Raising the Go consumer floor')"
fi

echo
echo "Tools Audit Complete. Nothing was modified."
