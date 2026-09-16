#!/usr/bin/env bash
# Build and test every module with the same commands CI runs, so a green run here
# is a strong signal CI will pass. Uses the committed go.work workspace, so each
# module resolves its in-repo siblings from local source. Run from the repository
# root.
set -euo pipefail

source "$(dirname "$0")/common.sh"
workspace_modules="$(enumerate_workspace_modules)"

echo "Build and Test..."

for module in ${workspace_modules}; do
  echo "==> ${module}"
  (
    cd "${module}"
    go build ./...
    go test -v -race -shuffle=on ./...
  )
done

# Compile-only guard for the adjacent test modules and example programs, which
# the workspace loop above never reaches: vet compiles as it analyzes, giving
# the integration tier fast signal here and for local developers without
# Docker (that tier runs them, but later and needing the stack).
for module in $(enumerate_adjacent_test_modules) $(enumerate_example_modules); do
  echo "==> ${module} (vet only)"
  ( cd "${module}" && GOWORK=off go vet ./... )
done

echo "✅ Build and Test Passed."
