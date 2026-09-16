#!/usr/bin/env bash
# Static-analysis suite across every module. Run by CI and locally, identically.
# Tool versions are checksum-pinned in tools/go/go.mod + tools/go/go.sum; we build those
# pinned tools once, then run the binaries against each module.
set -euo pipefail

source "$(dirname "$0")/common.sh"
workspace_modules="$(enumerate_workspace_modules)"

echo "Lint..."

# Build the pinned tools standalone into a throwaway bin directory.
# We build/install with GOWORK=off so the workspace Go version (this SDK's
# consumer floor) does not interfere with the tools/go module's Go version.
toolbin="$(mktemp -d)"
trap 'rm -rf "${toolbin}"' EXIT
(
  cd tools/go
  GOWORK=off GOBIN="${toolbin}" go install \
    mvdan.cc/gofumpt \
    honnef.co/go/tools/cmd/staticcheck \
    github.com/kisielk/errcheck \
    github.com/mgechev/revive \
    golang.org/x/tools/gopls
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

# The adjacent test modules and example programs sit outside the workspace, so
# the loop above never reaches them. GOWORK=off lets the go-package-graph
# tools resolve each one the way its own replace directives declare; gofumpt
# and revive walk the filesystem and need no module resolution (and these
# directories hold no node_modules or nested modules, so neither needs its
# exclusion dance).
for module in $(enumerate_adjacent_test_modules) $(enumerate_example_modules); do
  echo "==> ${module}"
  (
    cd "${module}"
    unformatted="$("${toolbin}/gofumpt" -l .)"
    if [ -n "${unformatted}" ]; then
      echo "gofumpt would reformat:"
      echo "${unformatted}"
      exit 1
    fi
    GOWORK=off go vet ./...
    GOWORK=off "${toolbin}/staticcheck" ./...
    GOWORK=off "${toolbin}/errcheck" ./...
    "${toolbin}/revive" -set_exit_status ./...
  )
done

# gopls is the diagnostic engine behind VS Code and every other LSP editor, and
# its default analyzers cover ground none of the standalone linters above do
# (infertypeargs, for example). Failing the gate on its findings keeps CI and a
# contributor's editor in agreement. One invocation from the repository root
# covers every module: gopls loads the committed go.work workspace, the same
# view an IDE opened at the root sees. The file list is a filesystem walk: a
# check gate reads the disk, not the git index, which reports an unstaged
# rename's old path as still present and would feed gopls a ghost file. Pruned
# from the walk: top-level dot-directories, node_modules (npm tooling ships
# third-party .go files - the same subtree gofumpt and revive exclude above)
# and user.transient (developer scratch space, per .gitignore).
# Like gofumpt above, gopls check reports findings without failing - it always
# exits zero - so fail on any output. The simplifier and unused-symbol
# analyzers (infertypeargs among them) report at information severity, below
# check's default warning-severity cutoff, so -severity=info is required to
# fail on everything an editor's problems panel shows.
echo "==> workspace-wide gopls check"
diagnostics="$(find . \( -path './.*' -o -name node_modules -o -name user.transient \) -prune -o -name '*.go' -print0 | xargs -0 "${toolbin}/gopls" check -severity=info)"
if [ -n "${diagnostics}" ]; then
  echo "gopls check found:"
  echo "${diagnostics}"
  exit 1
fi

echo "✅ Lint Passed."
