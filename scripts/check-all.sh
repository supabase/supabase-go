#!/usr/bin/env bash
# Run every local check in sequence, from the repository root: build and test,
# lint, vulnerability scan and spell check. A convenience for a pre-push sweep that
# invokes each sibling exactly as CI does (./scripts/<name>.sh). CI does NOT call
# this aggregate - it runs each script as its own job so a failure stays clearly
# attributable to one concern. Run from the repository root.
set -euo pipefail

./scripts/build-and-test.sh
./scripts/lint.sh
./scripts/vulncheck.sh
./scripts/spell-check.sh

echo "All checks passed."
