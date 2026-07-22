#!/usr/bin/env bash
# Vulnerability scan across every module, kept separate from lint.sh so a finding
# fails its own job. govulncheck's version is checksum-pinned in tools/go/go.mod.
set -euo pipefail

source "$(dirname "$0")/common.sh"
workspace_modules="$(enumerate_workspace_modules)"

echo "Vulnerability Scan..."

toolbin="$(mktemp -d)"
trap 'rm -rf "${toolbin}"' EXIT
(
  cd tools/go
  GOWORK=off GOBIN="${toolbin}" go install golang.org/x/vuln/cmd/govulncheck
)

for module in ${workspace_modules}; do
  echo "==> ${module}"
  ( cd "${module}" && "${toolbin}/govulncheck" ./... )
done

echo "✅ Vulnerability Scan Passed."
