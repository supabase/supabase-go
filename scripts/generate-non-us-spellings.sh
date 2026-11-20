#!/usr/bin/env bash
# Write tools/scowl/non-us-spellings.txt, the spellings the spell check reports
# as forbidden, from the SCOWL release pinned below
# (https://github.com/en-wl/wordlist). Up to SCOWL size 80, the list holds every
# word that SCOWL spells the British (B, Z) or Canadian (C) way, less those in
# SCOWL's American word list (A) at variant level 2 or below, which holds the
# preferred spellings and any equal to them. Australian spellings are left out.
#
# Run from the repository root after moving the pin, which
# scripts/tools-audit.sh reports on. Needs network access, git, Python 3.7 or
# later and SQLite 3.33 or later.
set -euo pipefail

SCOWL_RELEASE="rel-2026.02.25"
SCOWL_COMMIT="7e99edab8e32f9f9ea2b15f249ca8d4d67237410"

output="tools/scowl/non-us-spellings.txt"

echo "Generate Non-US Spellings..."

work_directory="$(mktemp -d)"
trap 'rm -rf "${work_directory}"' EXIT
scowl_directory="${work_directory}/scowl"

# The release tag is a movable pointer, so the clone must land on the pinned
# commit or nothing is written.
git -c advice.detachedHead=false clone --quiet --depth 1 --branch "${SCOWL_RELEASE}" \
  https://github.com/en-wl/wordlist "${scowl_directory}"
cloned_commit="$(git -C "${scowl_directory}" rev-parse HEAD)"
if [ "${cloned_commit}" != "${SCOWL_COMMIT}" ]; then
  echo "SCOWL ${SCOWL_RELEASE} is commit ${cloned_commit}, not the pinned ${SCOWL_COMMIT}." >&2
  exit 1
fi

# SCOWL's own build step, the one its Makefile runs. Its progress output is
# shown only when it fails.
if ! (cd "${scowl_directory}" && python3 combine.py create-db scowl.db) > "${work_directory}/create-db.log" 2>&1; then
  cat "${work_directory}/create-db.log" >&2
  exit 1
fi

# The list is assembled beside the clone and moved into place whole, so a
# failure part way leaves the committed list as it was.
{
  echo "# The spellings the spell check reports as forbidden, written by"
  echo "# scripts/generate-non-us-spellings.sh from SCOWL ${SCOWL_RELEASE}"
  echo "# (commit ${SCOWL_COMMIT}). Rerun that script rather"
  echo "# than edit this file."
  echo "#"
  # SCOWL's notice: everything its Copyright file holds above the first ===.
  awk '
    /^===/ { exit }
    { sub(/[ \t]+$/, ""); lines[++count] = $0 }
    END {
      while (count > 0 && lines[count] == "") count--
      for (line = 1; line <= count; line++) print (lines[line] == "" ? "#" : "# " lines[line])
    }' "${scowl_directory}/Copyright"
  (cd "${scowl_directory}" && python3 -) <<'PYTHON'
import contextlib
import io
import sqlite3

import libscowl

connection = sqlite3.connect("scowl.db")
# getWords reports each query it runs on stderr.
with contextlib.redirect_stderr(io.StringIO()):
    american = set(libscowl.getWords(connection, 80, ["A"], 2))
    british_and_canadian = set(libscowl.getWords(connection, 80, libscowl.Include("B", "Z", "C", noDefault=True), 9))
for word in sorted(british_and_canadian - american):
    print(word)
PYTHON
} > "${work_directory}/non-us-spellings.txt"
mv "${work_directory}/non-us-spellings.txt" "${output}"

echo "✅ Wrote $(grep -cv '^#' "${output}") spellings to ${output}."
