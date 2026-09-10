#!/usr/bin/env bash
# Run the integration tests against a local Supabase stack, exactly as CI does.
# Requires Docker (the stack's services are containers) and curl. Run from the
# repository root.
#
# The Supabase CLI release ships two co-located binaries - a `supabase` shim
# and the `supabase-go` CLI it forwards to - fetched from its pinned GitHub
# release, verified against a committed SHA-256 and installed into Go's own bin
# directory (GOBIN, else GOPATH/bin) - writable, on the developer's PATH and
# outside the checkout, so a read-only working tree is fine. It needs no Node
# and keeps the CLI off the cspell npm manifest (unrelated tools). First-party:
# curl plus a checksum, no third-party action.
#
# Why not `go install` it, given the Go toolchain right here? Its module
# (github.com/supabase/cli) now lives in apps/cli-go while the repo root carries
# no go.mod, so the module proxy resolves that path only to the stale v1
# root-module history rather than the v2 code, and its go.mod additionally
# carries local `replace` directives that `go install pkg@version` refuses
# outright. The verified release binary is the pinned way in.
#
# Bump SUPABASE_CLI_VERSION and the checksums together. The checksums are the
# matching lines from:
#   https://github.com/supabase/cli/releases/download/v${SUPABASE_CLI_VERSION}/checksums.txt
# If the new CLI changes the config.toml schema then integration-testing/supabase/config.toml
# may also need to be updated.
set -euo pipefail

SUPABASE_CLI_VERSION="2.114.0"

case "$(uname -s)" in
  Linux) cli_os="linux" ;;
  Darwin) cli_os="darwin" ;;
  *) echo "Unsupported OS for the Supabase CLI: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  aarch64 | arm64) cli_arch="arm64" ;;
  x86_64 | amd64) cli_arch="amd64" ;;
  *) echo "Unsupported architecture for the Supabase CLI: $(uname -m)" >&2; exit 1 ;;
esac
case "${cli_os}_${cli_arch}" in
  linux_arm64) cli_sha256="7bf55ea89c8ae48a101a9856c1cef22da8e2019ddcd451499249033652cacefa" ;;
  linux_amd64) cli_sha256="f36a33ca867f1cce9ba5efa705863fdc545d1465d3719a721793ea67eb692c5a" ;;
  darwin_arm64) cli_sha256="bdd67161af7c7e537e15ec72db2f2c59bca569b41d959bcaaec7004ba400e653" ;;
  darwin_amd64) cli_sha256="e03bf7cf5c49ba777ff4d65fd77e786a295fd7db4f1d6c6658b1505e7b15d512" ;;
  *) echo "No pinned checksum for ${cli_os}_${cli_arch}" >&2; exit 1 ;;
esac

# Go's own bin directory is the install target: GOBIN if set, else GOPATH/bin.
gobin="$(go env GOBIN)"
[[ -n "${gobin}" ]] || gobin="$(go env GOPATH)/bin"
SUPABASE_CLI="${gobin}/supabase"

# Fetch unless the pinned version is already installed there (a bump re-fetches).
if [[ ! -x "${SUPABASE_CLI}" ]] || [[ ! -x "${gobin}/supabase-go" ]] || ! "${SUPABASE_CLI}" --version 2>/dev/null | grep -qF "${SUPABASE_CLI_VERSION}"; then
  archive="supabase_${SUPABASE_CLI_VERSION}_${cli_os}_${cli_arch}.tar.gz"
  url="https://github.com/supabase/cli/releases/download/v${SUPABASE_CLI_VERSION}/${archive}"
  echo "==> installing pinned Supabase CLI ${SUPABASE_CLI_VERSION} to ${gobin} (${cli_os}/${cli_arch})"
  mkdir -p "${gobin}"
  curl -fsSL -o "${gobin}/${archive}" "${url}"
  # Verify the download against the committed checksum before trusting it.
  # sha256sum on Linux, shasum on macOS (which ships no sha256sum by default).
  if command -v sha256sum >/dev/null 2>&1; then
    echo "${cli_sha256}  ${gobin}/${archive}" | sha256sum -c -
  else
    echo "${cli_sha256}  ${gobin}/${archive}" | shasum -a 256 -c -
  fi
  # `supabase` is a thin shim that forwards to its co-located `supabase-go`
  # sibling (the actual CLI), so both must land in the same directory - extract
  # the whole archive, not just one member.
  tar -xzf "${gobin}/${archive}" -C "${gobin}"
  rm -f "${gobin}/${archive}"
  chmod +x "${gobin}/supabase" "${gobin}/supabase-go"
fi

echo "Integration Test..."

# The CLI is pointed at a disposable copy of the committed integration-testing/
# project directory, because it writes scratch state (supabase/.branches and
# supabase/.temp) inside whatever project it runs. The copy keeps the
# committed tree pristine by construction - no scratch to gitignore, and
# read-only checkouts (such as a read-only guest mount) work. --workdir is
# the same invocation shape as the CLI repository's own e2e harness, and an
# absolute path keeps every invocation location-independent; in particular
# the EXIT trap fires from whatever directory the script is in by then.
# Stopping from a copy still finds the running stack, which the CLI
# identifies by the project_id in config.toml, not by path.
project_directory="$(mktemp -d)"
cp -R integration-testing/. "${project_directory}/"

cleanup() {
  echo "==> stopping local stack"
  "${SUPABASE_CLI}" --workdir "${project_directory}" stop --no-backup || true
  rm -rf "${project_directory}"
}
trap cleanup EXIT

echo "==> starting local stack (pinned CLI $("${SUPABASE_CLI}" --version))"
"${SUPABASE_CLI}" --workdir "${project_directory}" start

# Export the stack's URL and key for the tests. `status -o env` prints
# KEY="value" lines. The stack's API keys - PUBLISHABLE_KEY included - are
# emitted only while the auth service is enabled (verified in the pinned CLI's
# status.go), which is why integration/supabase/config.toml keeps auth on.
# PUBLISHABLE_KEY is the successor to the deprecated ANON_KEY and is what the
# CLI repository's own e2e tests consume.
eval "$("${SUPABASE_CLI}" --workdir "${project_directory}" status -o env)"

# The modules with an adjacent integrationtest module - deliberately a curated
# subset of the workspace (core has none), so this is NOT derived from go.work
# like the build/lint/vuln lists are. Each integrationtest directory is its own
# non-published module outside the workspace, hence GOWORK=off with its replace
# directives resolving the SDK modules from the local tree. Selection is the
# module boundary alone - ./... with no -run name filter - so a test there can
# never be silently skipped by its name.
modules=(supabase postgrest auth)

echo "==> running integration tests (-race)"
for module in "${modules[@]}"; do
  echo "==> ${module}"
  (
    cd "${module}/integrationtest"
    GOWORK=off \
    SUPABASE_URL="${API_URL}" \
    SUPABASE_PUBLISHABLE_KEY="${PUBLISHABLE_KEY}" \
      go test -v -race -shuffle=on ./...
  )
done

echo "✅ Integration Test Passed."
