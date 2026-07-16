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
# Bump SUPABASE_CLI_VERSION and the checksums together with
# integration/supabase/config.toml, whose schema tracks the CLI. The checksums
# are the matching lines from (verify after any bump):
#   https://github.com/supabase/cli/releases/download/v${SUPABASE_CLI_VERSION}/checksums.txt
set -euo pipefail

SUPABASE_CLI_VERSION="2.109.1"

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
  linux_arm64) cli_sha256="a7f7c771acd3a2a13938b5832029beb24787b165d5119ed57ad6e964f3593291" ;;
  linux_amd64) cli_sha256="36d87b7fe6b4bcfe89ac47a4354e526cff22480224de426d7b370f6934556976" ;;
  darwin_arm64) cli_sha256="e36776717a56d704769229649349b3a382f413cb31f1fb2ba4647ef8bcf7339b" ;;
  darwin_amd64) cli_sha256="fee962ecf455c69497f93c19b369443b114a934161b8cecbd8a5b812c3c8c013" ;;
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

# The CLI is pointed at a disposable copy of the committed integration/
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
cp -R integration/. "${project_directory}/"

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

# Every module with integration-tagged tests runs here. The -run filter is
# anchored so it selects exactly the TestIntegration-prefixed functions and
# can never match a unit test compiled into the same package.
modules=(. postgrest)

echo "==> running integration tests (-race, tag: integration)"
for module in "${modules[@]}"; do
  echo "==> ${module}"
  (
    cd "${module}"
    SUPABASE_URL="${API_URL}" \
    SUPABASE_PUBLISHABLE_KEY="${PUBLISHABLE_KEY}" \
      go test -race -shuffle=on -tags integration -run '^TestIntegration' ./...
  )
done

echo "✅ Integration Test Passed."
