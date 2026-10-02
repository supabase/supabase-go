#!/usr/bin/env bash
# Spell-check the repository's Go and Markdown sources with the pinned
# cspell in tools/node. Run by CI and locally, identically, from the repository
# root. Installing the tooling is a separate one-time step (npm ci from
# tools/node), deliberately NOT done here: this check runs repeatedly, so it
# must not reinstall each time.
set -euo pipefail

echo "Spell Check..."

if [ ! -d tools/node/node_modules ]; then
  echo "tools/node dependencies are not installed. Run this once, then retry:" >&2
  echo "  (cd tools/node && npm ci)" >&2
  exit 1
fi

# npm run puts tools/node/node_modules/.bin on PATH, so cspell resolves by name -
# no hard-coded path. The npm script itself cd's from tools/node up to the repo
# root and passes the globs there, because cspell resolves CLI globs against the
# current directory. This wrapper only enters tools/node so npm finds the package.
( cd tools/node && npm run spell-check )

echo "✅ Spell Check Passed."
