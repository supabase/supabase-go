#!/usr/bin/env bash
# Runs go fix on every public module. To be used when upgrading Go, but probably
# at the consumer floor if possible, so fully check the build after.
# Run from the repository root.
set -euo pipefail

source "$(dirname "$0")/common.sh"
workspace_modules="$(enumerate_workspace_modules)"

echo "Running go fix on all public modules..."

for module in ${workspace_modules}; do
  echo "==> ${module}"
  (
    cd "${module}"
    go fix ./...
  )
done

echo "✅ Fix Finished. Check your diff!"
