#!/usr/bin/env bash
# Run the integration tests and example programs against a local Supabase
# stack, exactly as CI does. Always stops the stack on exit, including on failure.
# Requires Docker (the stack's services are containers) and curl. Run from the
# repository root.
#
# The Supabase CLI binary is fetched from its pinned GitHub release, verified
# against a committed SHA-256 and installed into Go's own bin directory (GOBIN,
# else GOPATH/bin) - writable, on the developer's PATH and outside the
# checkout, so a read-only working tree is fine. It needs no Node and keeps the
# CLI off the cspell npm manifest (unrelated tools). First-party: curl plus a
# checksum, no third-party action.
#
# Bump SUPABASE_CLI_VERSION and the checksums together, via
# scripts/upgrade-to-latest-supabase-cli.sh. If the new CLI changes the
# config.toml schema then integration-testing/supabase/config.toml may also
# need to be updated.
set -euo pipefail

source "$(dirname "$0")/common.sh"

SUPABASE_CLI_VERSION="3.0.0"

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
# Arms ordered alphabetically. scripts/upgrade-to-latest-supabase-cli.sh
# rewrites each arm's checksum in place, looked up by archive name in the
# release's checksums.txt.
case "${cli_os}_${cli_arch}" in
  darwin_amd64) cli_sha256="d14cdb8536eb37b0f3b6d8c70fa5497b941f71005f0b2ac152b6e6c62864529c" ;;
  darwin_arm64) cli_sha256="3b0bfc4602cd7d55ffc78a6b72443d3b8e5fdd856b777e75977978dceb3e44cc" ;;
  linux_amd64) cli_sha256="eeb61edcd59a4735fcb5e58596574f25d03d420efca779f43ddb307e1cc5c754" ;;
  linux_arm64) cli_sha256="243474fb5b491a0df04682fc1941cc5519559a1178ae01c225c633eda2e8acb8" ;;
  *) echo "No pinned checksum for ${cli_os}_${cli_arch}" >&2; exit 1 ;;
esac

# Go's own bin directory is the install target: GOBIN if set, else GOPATH/bin.
gobin="$(go env GOBIN)"
[[ -n "${gobin}" ]] || gobin="$(go env GOPATH)/bin"
SUPABASE_CLI="${gobin}/supabase"

# Fetch unless the pinned version is already installed there (a bump re-fetches).
if [[ ! -x "${SUPABASE_CLI}" ]] || ! "${SUPABASE_CLI}" --version 2>/dev/null | grep -qF "${SUPABASE_CLI_VERSION}"; then
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
  tar -xzf "${gobin}/${archive}" -C "${gobin}" supabase
  rm -f "${gobin}/${archive}"
  chmod +x "${SUPABASE_CLI}"
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

# Read the stack's URL and keys for the tests. `status -o env` prints
# KEY="value" lines. The stack's API keys - PUBLISHABLE_KEY included - are
# emitted only while the auth service is enabled (verified in the pinned CLI's
# status.go), which is why integration/supabase/config.toml keeps auth on.
# PUBLISHABLE_KEY is the successor to the deprecated ANON_KEY and is what the
# CLI repository's own e2e tests consume. SECRET_KEY is the privileged
# counterpart. Neither is exported here: each is handed below only to the
# code whose posture holds it.
eval "$("${SUPABASE_CLI}" --workdir "${project_directory}" status -o env)"

# The modules with an adjacent integrationtest module - deliberately a curated
# subset of the workspace (core has none), so this is NOT derived from go.work
# like the build/lint/vuln lists are. Each integrationtest directory is its own
# non-published module outside the workspace, hence GOWORK=off with its replace
# directives resolving the SDK modules from the local tree.
modules=(supabase postgrest auth)

# Every package in an integrationtest module sits under one of two posture
# directories, and the stack's keys reach a package by that posture alone. An
# asuser package acts as a signed-in user: it gets the publishable key only,
# with SUPABASE_SECRET_KEY unset in its environment by `env -u` so a key
# exported by the developer's shell cannot leak in. An asproject package acts
# as the project: it gets both keys. A package under neither directory is
# refused here rather than run, so no test sits in an undeclared posture.
# Selection is the package boundary alone - every package `go list` finds,
# with no -run name filter - so a test can never be silently skipped by its
# name. The listing is captured before the loop so a failing `go list` stops
# the script under `set -e` instead of running nothing, and an empty array is
# never expanded, which bash 3.2 (macOS) refuses under `set -u`.
echo "==> running integration tests (-race)"
for module in "${modules[@]}"; do
  echo "==> ${module}"
  (
    cd "${module}/integrationtest"
    export GOWORK=off
    packages="$(go list ./...)"
    if [[ -z "${packages}" ]]; then
      echo "${module}/integrationtest: go list found no packages" >&2
      exit 1
    fi
    asuser_packages=()
    asproject_packages=()
    while IFS= read -r package; do
      case "${package}" in
        */integrationtest/asuser | */integrationtest/asuser/*) asuser_packages+=("${package}") ;;
        */integrationtest/asproject | */integrationtest/asproject/*) asproject_packages+=("${package}") ;;
        *)
          echo "${package}: an integration test package lives under asuser/ or asproject/ - see DEVELOPMENT.md, Integration tests" >&2
          exit 1
          ;;
      esac
    done <<< "${packages}"
    if [[ "${#asuser_packages[@]}" -gt 0 ]]; then
      echo "==> ${module}: as a signed-in user (publishable key only)"
      SUPABASE_URL="${API_URL}" \
      SUPABASE_PUBLISHABLE_KEY="${PUBLISHABLE_KEY}" \
        env -u SUPABASE_SECRET_KEY go test -v -race -shuffle=on "${asuser_packages[@]}"
    fi
    if [[ "${#asproject_packages[@]}" -gt 0 ]]; then
      echo "==> ${module}: as the project (secret key)"
      SUPABASE_URL="${API_URL}" \
      SUPABASE_PUBLISHABLE_KEY="${PUBLISHABLE_KEY}" \
      SUPABASE_SECRET_KEY="${SECRET_KEY}" \
        go test -v -race -shuffle=on "${asproject_packages[@]}"
    fi
  )
done

# The example programs are consumer-shaped documentation: each must actually
# run against the stack, not merely compile, so a drifted example fails here
# rather than in a reader's hands. Each is handed the one key its posture
# holds, as its own documentation states: the operator tool the secret key,
# every other example the publishable key.
echo "==> running example programs"
for example in $(enumerate_example_modules); do
  echo "==> ${example}"
  (
    cd "${example}"
    export GOWORK=off SUPABASE_URL="${API_URL}"
    case "${example}" in
      examples/admin-provisioning)
        # The operator tool does one lifecycle stage per invocation, so the
        # harness walks all three verbs in order.
        export SUPABASE_SECRET_KEY="${SECRET_KEY}"
        user="provisioning-$(date +%s)-$$@example.com"
        go run . onboard "${user}"
        go run . reconcile
        go run . offboard "${user}"
        ;;
      examples/auth-session-service)
        # The service signs in as an account its operator created, so the
        # harness registers one over the Auth REST API first, as an
        # application's own sign-up flow would.
        export SUPABASE_PUBLISHABLE_KEY="${PUBLISHABLE_KEY}"
        export SERVICE_EMAIL="service-$(date +%s)-$$@example.com"
        export SERVICE_PASSWORD="service-password-$$"
        curl --fail --silent --show-error --output /dev/null \
          --header "apikey: ${PUBLISHABLE_KEY}" \
          --header "Content-Type: application/json" \
          --data "{\"email\":\"${SERVICE_EMAIL}\",\"password\":\"${SERVICE_PASSWORD}\"}" \
          "${API_URL}/auth/v1/signup"
        go run . -run-for=3s
        ;;
      *)
        export SUPABASE_PUBLISHABLE_KEY="${PUBLISHABLE_KEY}"
        go run .
        ;;
    esac
  )
done

echo "✅ Integration Test Passed."
