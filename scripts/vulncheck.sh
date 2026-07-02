#!/usr/bin/env bash
# Vulnerability scan across every module, kept separate from lint.sh so a finding
# fails its own job. govulncheck's version is checksum-pinned in tools/go/go.mod.
set -euo pipefail

modules=(. core postgrest)

toolbin="$(mktemp -d)"
trap 'rm -rf "${toolbin}"' EXIT
(
  cd tools/go
  GOWORK=off GOBIN="${toolbin}" go install golang.org/x/vuln/cmd/govulncheck
)

for module in "${modules[@]}"; do
  echo "==> ${module}"
  ( cd "${module}" && "${toolbin}/govulncheck" ./... )
done
