#!/usr/bin/env bash
# Static-analysis suite across every module. Run by CI and locally, identically.
# Tool versions are checksum-pinned in tools/go/go.mod + tools/go/go.sum; we build those
# pinned tools once, then run the binaries against each module.
set -euo pipefail

source "$(dirname "$0")/common.sh"
workspace_modules="$(enumerate_workspace_modules)"

echo "Lint..."

# Build the pinned tools standalone - GOWORK=off so the 1.25 workspace does not
# interfere with the 1.25.0 tools/go module - into a throwaway bin directory.
toolbin="$(mktemp -d)"
trap 'rm -rf "${toolbin}"' EXIT
(
  cd tools/go
  GOWORK=off GOBIN="${toolbin}" go install \
    mvdan.cc/gofumpt \
    honnef.co/go/tools/cmd/staticcheck \
    github.com/kisielk/errcheck \
    github.com/mgechev/revive
)

for module in ${workspace_modules}; do
  echo "==> ${module}"
  (
    cd "${module}"
    # gofumpt walks the filesystem and, unlike the go command, does not stop at
    # nested module boundaries, so drop any node_modules hits (npm tooling can
    # ship third-party .go files, e.g. cspell's flatted dependency).
    unformatted="$("${toolbin}/gofumpt" -l . | grep -v /node_modules/ || true)"
    if [ -n "${unformatted}" ]; then
      echo "gofumpt would reformat:"
      echo "${unformatted}"
      exit 1
    fi
    go vet ./...
    "${toolbin}/staticcheck" ./...
    "${toolbin}/errcheck" ./...
    # revive resolves ./... by walking the filesystem (mgechev/dots), not the
    # go/packages module graph, so - like gofumpt above - it ignores the
    # tools/node nested module and descends into node_modules. Exclude that subtree.
    "${toolbin}/revive" -exclude ./tools/node/... -set_exit_status ./...
  )
done

echo "✅ Lint Passed."
