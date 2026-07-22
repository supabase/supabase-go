#!/usr/bin/env bash
# Assert the X-Client-Info header each SDK entry point sends when the SDK is
# consumed from outside this repository, covering the two consumer build shapes
# the in-repo suites cannot produce. Run from the repository root.
#
# Module-mode leg: the telemetrytest module requires the SDK modules at
# fabricated versions and replaces them with the local working tree, so the
# binary resolves client versions from build-information dependency records the
# way every consumer build does. GOWORK=off detaches the run from the
# repository workspace, which does not list that module.
#
# GOPATH-mode leg (GO111MODULE=off): the same program rebuilt where binaries
# carry build information without module records, so every header must fall
# back to the version-unknowable 0.0.0 sentinel. The repository is linked into
# a scratch GOPATH at its own import path to satisfy the SDK imports.
set -euo pipefail

echo "Telemetry Header Test..."

echo "==> module-mode consumer (replaced dependencies)"
(
  cd telemetrytest
  GOWORK=off TELEMETRY_TEST_MODE=replaced-dependencies go run .
)

echo "==> GOPATH-mode consumer (no module information)"
scratch_gopath="$(mktemp -d)"
trap 'rm -rf "${scratch_gopath}"' EXIT
mkdir -p "${scratch_gopath}/src/github.com/supabase"
ln -s "$(pwd)" "${scratch_gopath}/src/github.com/supabase/supabase-go"
mkdir -p "${scratch_gopath}/src/telemetrytest"
cp telemetrytest/main.go "${scratch_gopath}/src/telemetrytest/"
(
  cd "${scratch_gopath}/src/telemetrytest"
  GOPATH="${scratch_gopath}" GO111MODULE=off GOWORK=off TELEMETRY_TEST_MODE=no-module-information go run .
)

echo "✅ Telemetry Header Test Passed."
