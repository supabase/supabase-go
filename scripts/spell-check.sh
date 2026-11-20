#!/usr/bin/env bash
# Spell-check the repository's Go and Markdown sources with the pinned
# cspell in tools/node. Run by CI and locally, identically, from the repository
# root. Installing the tooling is a separate step (npm ci from tools/node),
# deliberately NOT done here: this check runs repeatedly, so it must not
# reinstall each time. It fails, naming that step, until the installed tree is
# the one tools/node/package-lock.json pins.
set -euo pipefail

source "$(dirname "$0")/common.sh"

echo "Spell Check..."

require_pinned_node_tools

# npm run puts tools/node/node_modules/.bin on PATH, so cspell resolves by name -
# no hard-coded path. The npm script itself cd's from tools/node up to the repo
# root and passes the globs there, because cspell resolves CLI globs against the
# current directory. This wrapper only enters tools/node so npm finds the package.
( cd tools/node && npm run spell-check )

echo "✅ Spell Check Passed."
