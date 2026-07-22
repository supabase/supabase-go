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
    go test -race -shuffle=on ./...
    # Compile-only guard for integration-tagged tests, so they get fast signal
    # here and for local developers without Docker (the integration tier also
    # compiles them, but later and slower).
    go vet -tags integration ./...
  )
done

echo "✅ Build and Test Passed."
