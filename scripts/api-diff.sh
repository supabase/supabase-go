#!/usr/bin/env bash
# Report a published module's public API changes since one of its releases:
# the version given, or else the newest version its changelog declares.
# golang.org/x/exp/cmd/apidiff lists the incompatible changes and then the
# compatible ones, leaving internal packages out. API changes, incompatible
# ones included, never fail the script. Downloading the release takes network
# access.
#
# gorelease cannot stand in: it loads the module through its pinned sibling
# requires rather than go.work, so it cannot load a module that needs a
# sibling releasing alongside it.
set -euo pipefail

source "$(dirname "$0")/common.sh"

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "usage: $0 <module> [<version>]" >&2
  echo "  module   a published module directory listed in go.work, e.g. core" >&2
  echo "  version  the release to compare with, by default the newest its changelog declares" >&2
  exit 2
fi
module="$1"
if ! is_workspace_module "${module}"; then
  echo "'${module}' is not a published (go.work) module. Choose one of:" >&2
  enumerate_workspace_modules >&2
  exit 1
fi
version="${2:-$(declared_module_versions "${module}" | head -n 1)}"
if [ -z "${version}" ]; then
  echo "${module} has no release to compare with."
  exit 0
fi
module_path="$(module_path_base)/${module}"

scratch="$(mktemp -d)"
trap 'rm -rf "${scratch}"' EXIT
( cd tools/go && GOWORK=off GOBIN="${scratch}/bin" go install golang.org/x/exp/cmd/apidiff )

# The release loads as a main module of its own, from a writable copy of its
# download: a published module carries no go.sum, so -mod=mod lets the go
# command record the checksums of the siblings it requires.
(
  cd "${scratch}"
  export GOWORK=off
  download_module_json "${module_path}@${version}" download.json
  cp -R "$(jq -r '.Dir' download.json)" released
  chmod -R u+w released
  cd released
  GOFLAGS=-mod=mod "${scratch}/bin/apidiff" -m -w "${scratch}/released.x" "${module_path}"
)

# The working tree loads through go.work, so a sibling releasing alongside
# resolves from local source, as the release will pin it.
( cd "${module}" && "${scratch}/bin/apidiff" -m -w "${scratch}/working.x" "${module_path}" )

# apidiff names each internal package it leaves out on standard error, and
# only those lines are dropped from it.
report="$("${scratch}/bin/apidiff" -m "${scratch}/released.x" "${scratch}/working.x" 2> >(grep -v '^Ignoring internal package ' >&2))"
if [ -z "${report}" ]; then
  echo "No public API changes in ${module} since ${version}."
else
  echo "Public API changes in ${module} since ${version}:"
  echo "${report}"
fi
