#!/usr/bin/env bash
# Build and test every module with the same commands CI runs, so a green run here
# is a strong signal CI will pass. Uses the committed go.work workspace, so each
# module resolves its in-repo siblings from local source. Run from the repository
# root.
set -euo pipefail

modules=(. configuration postgrest)

for module in "${modules[@]}"; do
  echo "==> ${module}"
  (
    cd "${module}"
    go build ./...
    go test -race -shuffle=on ./...
  )
done

echo "All modules built and tested."