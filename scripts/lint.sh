#!/usr/bin/env bash
# Static-analysis suite across every module. Run by CI and locally, identically.
# Tool versions are checksum-pinned in tools/go.mod + tools/go.sum; we build those
# pinned tools once, then run the binaries against each module.
set -euo pipefail

modules=(. core postgrest)

# Build the pinned tools standalone - GOWORK=off so the 1.22 workspace does not
# interfere with the 1.24 tools module - into a throwaway bin directory.
toolbin="$(mktemp -d)"
trap 'rm -rf "${toolbin}"' EXIT
(
  cd tools
  GOWORK=off GOBIN="${toolbin}" go install \
    mvdan.cc/gofumpt \
    honnef.co/go/tools/cmd/staticcheck \
    github.com/kisielk/errcheck \
    github.com/mgechev/revive
)

for module in "${modules[@]}"; do
  echo "==> ${module}"
  (
    cd "${module}"
    unformatted="$("${toolbin}/gofumpt" -l .)"
    if [ -n "${unformatted}" ]; then
      echo "gofumpt would reformat:"
      echo "${unformatted}"
      exit 1
    fi
    go vet ./...
    "${toolbin}/staticcheck" ./...
    "${toolbin}/errcheck" ./...
    "${toolbin}/revive" -set_exit_status ./...
  )
done
