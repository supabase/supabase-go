#!/usr/bin/env bash
# Validate the external URLs in the published modules' comments with the
# commentchecker tool (tools/go/commentchecker): HTTPS only, domains from the
# committed allowed list (comment-checker.yaml), an HTTP 200 HTML response
# within two permanent-redirect hops and any URL fragment resolving to an id
# in the document. Run by CI and locally, identically, from the repository
# root. Probes live websites, so it is not part of check-fast.sh - see
# DEVELOPMENT.md for when to run it.
set -euo pipefail

source "$(dirname "$0")/common.sh"
workspace_modules="$(enumerate_workspace_modules)"

echo "Comment Check..."

# Build the tool standalone, the same way lint.sh builds the pinned linters:
# GOWORK=off so the tools/go module's own go.mod governs, and a throwaway
# GOBIN so the binary runs from the repository root, keeping the reported file
# paths workspace-relative.
toolbin="$(mktemp -d)"
trap 'rm -rf "${toolbin}"' EXIT
( cd tools/go && GOWORK=off GOBIN="${toolbin}" go install ./commentchecker )

"${toolbin}/commentchecker" -configuration comment-checker.yaml ${workspace_modules}

echo "✅ Comment Check Passed."
