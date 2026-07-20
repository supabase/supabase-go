#!/usr/bin/env bash
# Assert the X-Client-Info header each SDK entry point sends when the SDK is
# consumed as a module dependency rather than built within this repository.
# The telemetrytest module requires the SDK modules at fabricated versions and
# replaces them with the local working tree, so the binary resolves client
# versions from build-information dependency records the way every consumer
# build does. GOWORK=off detaches the run from the repository workspace, which
# does not list that module. Run from the repository root.
set -euo pipefail

echo "Telemetry Header Test..."

(
  cd telemetrytest
  GOWORK=off go run .
)

echo "✅ Telemetry Header Test Passed."
