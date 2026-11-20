#!/usr/bin/env bash
# Vulnerability scan, kept separate from lint.sh so a finding fails its own job.
# Two scans run, and the script fails when either reports a finding:
#
# - govulncheck reports the vulnerabilities that the published (workspace)
#   modules can reach, in their dependencies or in the Go standard library.
# - OSV-Scanner reports every dependency version, declared in any go.mod or npm
#   lockfile in the repository, that a known advisory affects, whether or not
#   any code reaches it.
#
# Each tool's version is checksum-pinned in a tool module: govulncheck in
# tools/go and OSV-Scanner in tools/go/osv-scanner.
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
(
  cd tools/go/osv-scanner
  GOWORK=off GOBIN="${toolbin}" go install github.com/google/osv-scanner/v2/cmd/osv-scanner
)

status=0

for module in ${workspace_modules}; do
  echo "==> ${module}"
  ( cd "${module}" && "${toolbin}/govulncheck" ./... ) || status=1
done

# --recursive reads every manifest below the repository root, skipping what
# .gitignore ignores. --no-call-analysis=go fails the scan on a finding even
# where OSV-Scanner would otherwise judge the vulnerable code unreachable.
# --format=vertical lists every finding in full under its manifest, whatever
# the terminal's width, where the default table cuts its rows to fit.
echo "==> every go.mod and npm lockfile (OSV-Scanner)"
"${toolbin}/osv-scanner" scan source --recursive --no-call-analysis=go --format=vertical . || status=1

if [ "${status}" -ne 0 ]; then
  exit "${status}"
fi

echo "✅ Vulnerability Scan Passed."
