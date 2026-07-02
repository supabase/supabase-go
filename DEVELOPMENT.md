# Developing the Supabase Go SDK

This file holds the Go/SDK-specific guidance for working in this repository.
General, organization-wide contribution policy lives in our [shared `.github` repository](https://github.com/supabase/.github)'s CONTRIBUTING.md file.

## Pre-GA: external code contributions are not being accepted yet

This SDK is being built first-party through its Alpha and Beta phases.
We are deliberately not accepting external code contributions until the SDK reaches its first General Availability release (`v1.0.0`).
Keeping the surface under tight first-party control through the hardening window protects velocity and avoids contention over a design that is still being settled.

## What is welcome now

- Bug reports and reproductions via GitHub issues.
- Feedback on the API surface and developer experience via GitHub Discussions.

## Building locally

The repository is a multi-module monorepo. Intra-repo dependencies are resolved by the committed `go.work` workspace, so each module builds against the local sources of the others without any published tags.

There is no task runner or aggregate linter. Build and test with the standard toolchain in each module directory (`.`, `core`, `postgrest`):

```bash
go build ./...
go test -race -shuffle=on ./...
```

Or, for all:

```bash
./scripts/build-and-test.sh
```

Lint and vulnerability scanning run via two scripts that are *exactly* what CI runs - same commands, same checksum-pinned tool versions (from `tools/go/go.mod` + `tools/go/go.sum`):

```bash
./scripts/lint.sh       # gofumpt, go vet, staticcheck, errcheck, revive - all modules
./scripts/vulncheck.sh  # govulncheck - all modules
```

### Previewing the rendered docs

`pkg.go.dev` is where consumers read our doc comments and runnable examples. To preview that rendering for your local working tree, run [`pkgsite`](https://pkg.go.dev/golang.org/x/pkgsite/cmd/pkgsite). It reads the [`go.work` workspace file](./go.work), so one run from the repository root serves all three modules on a local HTTP server (it prints the address, by default http://localhost:8080).

First you'll need to install it for your user-local environment if you've not done that before:

```bash
go install golang.org/x/pkgsite/cmd/pkgsite@latest
```

After that you can launch it with:

```bash
pkgsite
```

If the shell then reports `pkgsite: command not found`, the installer's target directory - `$(go env GOPATH)/bin`, usually `~/go/bin` - is not on your `PATH`. Note that this is a different directory from the Go toolchain's own `/usr/local/go/bin`, where `go` lives. You can add it to your `PATH` by adding `export PATH="$PATH:$(go env GOPATH)/bin"` to your shell profile (`~/.zshrc` for zsh).

If you don't want to modify your `PATH` then you can launch it directly with:

```bash
"$(go env GOPATH)/bin/pkgsite"
```

`pkgsite` is a personal, read-only previewer, not project tooling: nothing in the repo or CI invokes it and it never ships. So it sits outside the supply-chain pinning below, which covers what our build, test and release pipeline executes. Install the latest when you want to preview.

## Supply-chain pinning

Everything we execute from outside the repository is pinned to an immutable digest, and that applies to **both** GitHub Actions and our Go tooling - first-party included, with no exemption.

- **GitHub Actions:** every `uses:` is pinned to a full-length 40-character commit SHA, with the human-readable version in a trailing comment - for example `uses: actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0 # v7.0.0`. A version tag like `@v7` is a *movable* git pointer: whoever controls it (or compromises the publisher) can re-point it at malicious code that then runs with the workflow's token and secrets. Actions have no lockfile, so the SHA is the only immutable reference. This is GitHub's own [security-hardening guidance](https://docs.github.com/en/actions/security-for-github-actions/security-guidance/security-hardening-for-github-actions) and is now enforceable as a [repository policy](https://github.blog/changelog/2025-08-15-github-actions-policy-now-supports-blocking-and-sha-pinning-actions/) (which this repo has enabled); it aligns with [SLSA](https://slsa.dev/spec/). Tools like [`pinact`](https://github.com/suzuki-shunsuke/pinact) can help you resolve tags to SHAs.
- **Go tooling:** the linters and vuln scanner live in a separate, non-published [`tools` module](tools/go/) and are pinned by checksum in [that module's `go.sum`](tools/go/go.sum) - the Go-native equivalent of a commit-SHA pin. The scripts build those exact, verified versions; nothing floats.
- **Companion control:** do not use `pull_request_target` in any workflow with access to secrets (see the [pwn-requests advisory](https://securitylab.github.com/research/github-actions-preventing-pwn-requests/)).

## Naming

We spell identifiers out in full. Clarity for every reader - including those newer to Go or to English - outweighs brevity. Concretely:

- **No invented or contracted abbreviations.** Write `configuration`, not `config` or `cfg`; `request`, not `req`; `response`, not `resp`; `user`, not `usr`; `index`, not `idx`. This applies to **every name you introduce** - types, methods, functions, struct fields, constants, package-level declarations, local variables, function parameters and the variables you pass as arguments. There is no Go convention requiring short type, field, function, variable or parameter names, so spelling them in full costs nothing.
- **Initialisms stay in their canonical case:** `URL`, `ID`, `HTTP`, `API`, `JSON`, `JWT` (for example `projectURL`, `userID`). Never `Url` or `Id`.
- **A small, closed set of conventional short names is permitted**, because each is either forced by the language or so universal that a longer form would be less clear, not more:
  - `err` for an `error` value (naming it `error` would shadow the builtin type);
  - `ctx` for a `context.Context` (naming it `context` would shadow the package);
  - `t`, `b`, `f` for `*testing.T`, `*testing.B`, `*testing.F` in tests;
  - single letters for pure loop indices (`i`, `j`);
  - the `Err` prefix on exported sentinel error values (for example `ErrMissingURL`) - the universal Go convention that pairs with `errors.Is`; `Error`-prefixed names would be actively non-idiomatic.
- **Method receivers are short (1-2 characters) and consistent**, following the Go standard style (for example `func (c *Configuration)`). This is the one place Go's own style guide mandates brevity, and our tooling and every Go reader expect it, so we follow it rather than fight it. Pick a receiver per type and use it on every method of that type.
- **Filenames are spelled out too, with one canonical exception.** Use `configuration.go`, not `config.go`. The exception is `doc.go` - the long-established Go convention for a package's documentation file, which tooling and readers expect by that exact name, so we keep it rather than renaming it to `documentation.go`. (The `_test.go` suffix is a required toolchain convention, not a naming choice.)

This is a deliberately strong stance. It keeps almost the entire surface in plain words while still reading as idiomatic Go, because the only short names left are the ones Go itself treats as conventional.

## Testing conventions

If you are newer to Go, a few conventions are worth knowing - they are stricter and more file-layout-driven than many other ecosystems.

- **Tests live next to the code they test**, in the same directory, in files ending `_test.go`. There is no separate `tests/` folder. The `_test.go` suffix is special: those files are compiled only by `go test` and never ship in a consumer's build.
- **Tests are discovered by convention, not registration.** [`go test`](https://pkg.go.dev/cmd/go#hdr-Test_packages) runs every function whose name and signature match a known shape - `func TestXxx(t *testing.T)`, `func BenchmarkXxx(b *testing.B)`, `func ExampleXxx()`, `func FuzzXxx(f *testing.F)`. No attributes, no suite registry, no config.
- **Prefer the standard library.** We write tests with [the standard `testing` package](https://pkg.go.dev/testing) and explicit checks (`if got != want { t.Errorf(...) }`), usually as table-driven tests with a subtest per case via [`t.Run`](https://pkg.go.dev/testing#T.Run). We are not (yet) using an assertion or mocking library; keep new tests stdlib-only unless a change is discussed first.
- **Runnable examples are documentation.** `ExampleXxx` functions are compiled and executed by `go test` and rendered on pkg.go.dev, so our usage docs cannot drift from reality. Add an example for notable exported API.
- **In a test helper, call [`t.Helper()`](https://pkg.go.dev/testing#T.Helper) first.** That makes a failure point at the line that called the helper rather than at a line inside it, which keeps failures readable as helpers accumulate. It's acceptable to skip the `t.Helper()` call in a helper method implementation that only does setup and [`t.Cleanup`](https://pkg.go.dev/testing#T.Cleanup) but never calls [`t.Error`](https://pkg.go.dev/testing#T.Error) or [`t.Fatal`](https://pkg.go.dev/testing#T.Fatal), since it has no failure line to relocate.

### Test from the outside in: prefer the external test package

A Go test file in a package directory can declare one of two packages, and both are allowed to sit side by side in the same directory:

- `package foo_test` - an **external test package**. It can only see `foo`'s exported (public) API, exactly as a real consumer would. This is sometimes called *black-box* (or *behavioural* / *clear-from-the-outside*) testing.
- `package foo` - an **in-package test**. It compiles as part of `foo`, so it can reach unexported (private) identifiers. This is sometimes called *white-box* (or *structural*) testing.

**Our default is the external test package (`foo_test`).** Testing through the public API tests what consumers actually use, keeps tests decoupled from internal details so refactoring internals does not spuriously break tests, and applies healthy pressure to keep the exported surface usable. Reach for an in-package test (`foo`) only when you genuinely need to exercise internals that are not observable through the public API, and prefer to keep such tests few and clearly named (for example `something_internal_test.go`).

A note on terminology: the industry terms for these are "black-box" and "white-box" testing, and we mention them so the mapping is clear, but we prefer the precise, Go-native framing - *external test package* versus *in-package test* - which also sidesteps the loaded black/white metaphor. (Where a single word helps, the neutral synonyms *closed-box* and *clear-box* are also in common use.)
