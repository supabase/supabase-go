#!/usr/bin/env bash
# Build and test every module with the same commands CI runs, so a green run here
# is a strong signal CI will pass. Uses the committed go.work workspace, so each
# module resolves its in-repo siblings from local source. Run from the repository
# root.
set -euo pipefail

modules=(. configuration postgrest)

echo "Build and Test..."

for module in "${modules[@]}"; do
  echo "==> ${module}"
  (
    cd "${module}"
    go build ./...
    go test -race -shuffle=on ./...
  )
done

# Compile-only guard for the integration-tagged tests, so they get fast signal
# here and for local developers without Docker (the integration tier also
# compiles them, but later and slower).
echo "==> compile integration-tagged tests (no execution)"
(cd postgrest && go vet -tags integration ./...)

echo "✅ Build and Test Passed."
