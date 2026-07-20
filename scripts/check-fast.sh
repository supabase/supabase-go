#!/usr/bin/env bash
# Run every fast local check in sequence, from the repository root: build and
# test, telemetry header test, lint, vulnerability scan and spell check. A
# convenience for a pre-push sweep that invokes each sibling exactly as CI does
# (./scripts/<name>.sh). CI
# does NOT call this aggregate - it runs each script as its own job so a
# failure stays clearly attributable to one concern.
#
# This is the fast tier: everything here needs only the repository's own
# toolchains (Go, plus Node for the spell check). The second tier,
# ./scripts/integration-test.sh, needs Docker and is run separately - see
# DEVELOPMENT.md for when to run each.
set -euo pipefail

./scripts/build-and-test.sh
./scripts/telemetry-test.sh
./scripts/lint.sh
./scripts/vulncheck.sh
./scripts/spell-check.sh

echo "All fast checks passed."
