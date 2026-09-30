# Developing the Supabase Go SDK

<!-- cSpell:ignore darwin linux mvdan startswith -->

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

If you're working from a new machine or perhaps inside a minimal sandbox, then you might want to first look at [Build Prerequisites](#build-prerequisites).

There is no task runner or aggregate linter. Build and test with the standard toolchain in each module directory
([`auth`](auth/), [`core`](core/), [`postgrest`](postgrest/), [`supabase`](supabase), etc..):

```bash
go build ./...
go test -race -shuffle=on ./...
```

Or, for all, from repository root:

```bash
./scripts/build-and-test.sh
```

Lint and vulnerability scanning run via two scripts that are *exactly* what CI runs - same commands, same checksum-pinned tool versions (from `tools/go/go.mod` + `tools/go/go.sum`):

```bash
./scripts/lint.sh       # gofumpt, go vet, staticcheck, errcheck, revive - all modules; then gopls check workspace-wide
./scripts/vulncheck.sh  # govulncheck - all modules
```

Spell-checking uses [cSpell](https://cspell.org), via Node/npm:

```bash
npm ci --prefix tools/node   # one-time setup (re-run only when the tools/node/package-lock.json lockfile changes)
./scripts/spell-check.sh     # cspell - Go and Markdown sources
```

The published module graph is guarded too: every first-party `require` in a workspace module (`go.mod` file) must name another workspace module - the modules we actually publish - so a consumer's build can resolve them. Our [`go.work`](go.work) overlay otherwise resolves siblings from local source and hides a require pointing at an unpublished or non-existent first-party path until consumers hit it after tags exist. The same check also holds the require graph to its layering DAG - core requires nothing first-party, a domain module requires only core and only the root `supabase` module composes the domains:

```bash
./scripts/check-module-paths.sh
```

To run the whole fast tier before pushing - build and unit test plus the module-path guard, lint, vulnerabilities and spelling - use the aggregate:

```bash
./scripts/check-fast.sh
```

### Checking doc-comment URLs

Doc comments across the published modules link out to external documentation, and those links rot silently. The comment check validates every URL that `go/doc/comment` recognizes in the published packages' comments (internal packages, test files and non-published modules are out of scope): each must use HTTPS, sit on a domain listed in [`comment-checker.yaml`](comment-checker.yaml) and resolve to an HTTP 200 HTML document within two permanent-redirect (301/308) hops, and a URL fragment must match an `id` in the resolved document. An HTTP 429 response from a domain listed under `rate-limited-domains` is tolerated and announced rather than failed - rate limiting of shared CI egress IPs says nothing about link health - and each tolerated URL surfaces as a warning annotation on the GitHub Actions run. A 429 from a domain not so listed fails the run. A URL that cannot pass yet can be excused under `ignored-urls` in the same file: entries are matched exactly and never fetched, and an entry that matches no URL in the comments fails the run so the list cannot outlive its reasons. Each distinct URL (query and fragment aside) is fetched once per run - each fetch is echoed as progress - and every failing URL is reported with its file path and line number:

```bash
./scripts/comment-check.sh
```

It probes live websites, so it is deliberately not part of `./scripts/check-fast.sh`. CI runs it as its own `comment-check` job; locally, run it on demand - typically after editing doc comments that carry URLs, or to reproduce a failure of that CI job.

### Running checks at the consumer floor

[CI](.github/workflows/ci.yml) proves consumer-facing behavior on two toolchains: the published floor (the oldest Go a consumer may hold us to - see [Supported Go versions](README.md#supported-go-versions)) and current stable. A local run uses whatever Go is installed, so to reproduce the floor legs name the toolchain for that run:

```bash
GOTOOLCHAIN=go1.26.8 ./scripts/check-fast.sh
GOTOOLCHAIN=go1.26.8 ./scripts/integration-test.sh
```

You can discover the current, full version number for a particular major version of Go (in this example, for Go `1.26`):

```bash
curl -s "https://go.dev/dl/?mode=json" | jq -r '.[].version | select(startswith("go1.26"))' | head -n 1
```

### Fixing Formatting for `gofumpt`

When running [`lint.sh`](scripts/lint.sh), either directly or via [`check-fast.sh`](scripts/check-fast.sh), you may see a message in this form:

```
gofumpt would reformat:
some/path/to/a/file.go
```

In this scenario you can run the following from repository root to ask `gofumpt` to fix what it didn't like:

```
GOWORK=off go -C tools/go run mvdan.cc/gofumpt -w ../..
```

This ensures you fix the formatting using the exact versions of tools used by our [build `scripts/`](scripts/) (including in CI) as specified in [the `tools/go/` module](tools/go/).

### Integration tests

The fast tier above needs only the repository's own toolchains (Go, plus Node for the spell check) so should be treated as the default gate before every push. The second tier exercises the SDK against a local Supabase stack (Postgres + PostgREST), has prerequisites and takes longer to run. Prerequisites:

- **Docker**: Installed and running - the stack's services are containers.
- **`curl`**: The script fetches the version-pinned Supabase CLI binary from its GitHub release on first run, verifies it against a committed SHA-256 and installs it into Go's own bin directory (`$(go env GOPATH)/bin`).
- **Network and disk on first run**: The CLI download and the stack's container images are fetched once and cached.
- **Free default ports**: The stack binds the API on `54321` and Postgres on `54322`.

```bash
./scripts/integration-test.sh
```

The script starts the stack against a disposable copy of [`integration-testing/`](integration-testing/), seeds it, runs each `integrationtest` module's tests under `-race` and always stops the stack on exit, including on failure. A plain `go test ./...` in a published module never runs these tests - each `integrationtest` directory is its own non-published module outside its parent's package pattern, and the tests are environment-gated besides - so the fast tier stays Docker-free by construction.

Integration tests live in adjacent `integrationtest` modules - one beside each module with integration coverage, sharing fixtures (stack credentials, end-user signup) through the non-published [`integration-testing/testkit` module](integration-testing/testkit/) - and are selected by the module boundary alone: the script runs `./...` in each with no `-run` name filter, so a test there can never be silently skipped by its name. These modules sit outside the [`go.work`](go.work) workspace, which lists exactly the published set, so the script enters them with `GOWORK=off` and their `replace` directives resolve the SDK modules from the local tree; `./scripts/build-and-test.sh` gives the same modules a compile-only `go vet` pass, so integration code gets fast signal without Docker. Their sources are ordinary untagged Go, so editors need no build-tag configuration; running an integration test from the editor's test lens fails fast with the env guidance unless the stack is up and its variables are exported in the editor's environment.

### Example programs

Runnable consumer-shaped programs live under [`examples/`](examples/), one non-published module per example with `replace` directives to the local tree - the same shape as the `integrationtest` modules, entered with `GOWORK=off` for the same reason. The fast tier compile-guards them (`go vet` via `./scripts/build-and-test.sh`) and lint covers them, while `./scripts/integration-test.sh` additionally runs each one against the local stack, so a drifted example fails the build rather than a reader. Example-only dependencies (OpenTelemetry, for `tracing-otel`) stay quarantined in the example's own module and never enter a consumer's graph.

### Telemetry header test

Suites in the workspace always see the `(devel)` sentinel in the `X-Client-Info` header, because an in-tree build cannot resolve SDK module versions from build information. The consumer-view check covers the resolution real consumers exercise: the `telemetrytest/` module requires the SDK modules at fabricated versions, `replace`s them to the local tree and asserts the exact header every entry point sends. A second leg rebuilds the same program in GOPATH mode, where binaries carry no module records, and asserts the version-unknowable `0.0.0` fallback. It needs only the Go toolchain and runs as part of the fast tier via `./scripts/check-fast.sh`, or alone:

```bash
./scripts/telemetry-test.sh
```

### Previewing the rendered docs

`pkg.go.dev` is where consumers read our doc comments and runnable examples. To preview that rendering for your local working tree, run [`pkgsite`](https://pkg.go.dev/golang.org/x/pkgsite/cmd/pkgsite). It reads the [`go.work` workspace file](go.work), so one run from the repository root serves every workspace module on a local HTTP server (it prints the address, by default http://localhost:8080).

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

### Build Prerequisites

Our scripts require a few things of your local environment:

- [Go](https://go.dev/doc/install) - a recent version, we suggest the latest available
- Ability to run [cgo](https://go.dev/wiki/MinimumRequirements#cgo)
- [The `jq` command](https://jqlang.org/) must be available

For the cgo requirement on a minimal Ubuntu install, the following should be enough:

```bash
sudo apt update
sudo apt-get install --no-install-recommends gcc libc6-dev
```

This avoids the heavier weight `build-essential` meta-package, as well as optional dependencies like man pages and extra tooling (what `--no-install-recommends` strips away).

## Supply-chain pinning

Everything we execute from outside the repository is pinned to an immutable digest, and that applies to **both** GitHub Actions and our Go tooling - first-party included, with no exemption.

- **GitHub Actions:** every `uses:` is pinned to a full-length 40-character commit SHA, with the human-readable version in a trailing comment - for example `uses: actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0 # v7.0.0`. A version tag like `@v7` is a *movable* git pointer: whoever controls it (or compromises the publisher) can re-point it at malicious code that then runs with the workflow's token and secrets. Actions have no lockfile, so the SHA is the only immutable reference. This is GitHub's own [security-hardening guidance](https://docs.github.com/en/actions/security-for-github-actions/security-guidance/security-hardening-for-github-actions) and is now enforceable as a [repository policy](https://github.blog/changelog/2025-08-15-github-actions-policy-now-supports-blocking-and-sha-pinning-actions/) (which this repo has enabled); it aligns with [SLSA](https://slsa.dev/spec/). Tools like [`pinact`](https://github.com/suzuki-shunsuke/pinact) can help you resolve tags to SHAs.
- **Go tooling:** the linters and vuln scanner live in a separate, non-published [`tools` module](tools/go/) and are pinned by checksum in [that module's `go.sum`](tools/go/go.sum) - the Go-native equivalent of a commit-SHA pin. The scripts build those exact, verified versions; nothing floats.
- **Companion control:** do not use `pull_request_target` in any workflow with access to secrets (see the [pwn-requests advisory](https://securitylab.github.com/research/github-actions-preventing-pwn-requests/)).
- **Node tooling:** the spell checker (cspell) is pinned the same way, one ecosystem over. Its entire dependency tree is locked by SHA-512 integrity hash in [`tools/node/package-lock.json`](tools/node/package-lock.json), the npm-native equivalent of `go.sum`, strictly used by `npm ci`.

## Degraded Dependabot "Dependency Graph" runs on GitHub

The Actions tab lists a "Dependency Graph" workflow that this repository does not define. It is a [Dependabot graph job](https://docs.github.com/en/code-security/concepts/supply-chain-security/dependency-graph-data#dependabot-graph-jobs), run when a push to `main` changes a `go.mod`, and it feeds Dependabot alerts.

A Degraded result naming one of our modules at the zero pseudo-version (`unknown revision 000000000000`) is expected while any published module requires an untagged sibling. The job runs `go mod graph` under the root [`go.work`](go.work), where every published module's requirements apply, and that version exists nowhere.

It is safe to ignore. The dependency list survives, so alerts still cover everything and only the edges between dependencies are lost. Only tags fix it, and disabling the graph would lose the alerts too. [`tools/go`](tools/go/) has its own `go.work`, but the examples have none, because their `go` line is the consumer floor and a `go.work` would add a second line to raise each time.

A Degraded message naming anything else, such as a third-party module, is worth investigating.

## Naming

We spell identifiers out in full. Clarity for every reader - including those newer to Go or to English - outweighs brevity. Concretely:

- **No invented or contracted abbreviations.** Write `configuration`, not `config` or `cfg`; `request`, not `req`; `response`, not `resp`; `user`, not `usr`; `index`, not `idx`. This applies to **every name you introduce** - types, methods, functions, struct fields, constants, package-level declarations, local variables, function parameters and the variables you pass as arguments. There is no Go convention requiring short type, field, function, variable or parameter names, so spelling them in full costs nothing.
- **Initialisms stay in their canonical case:** `URL`, `ID`, `HTTP`, `API`, `JSON`, `JWT` (for example `projectURL`, `userID`). Never `Url` or `Id`.
- **PostgREST operator names are domain vocabulary, not abbreviations.** Filter methods take the form of PostgREST's wire-protocol operators - `Eq`, `Neq`, `Gt`, `Gte`, `Lt`, `Lte`, `ILike` and family.
- **A small, closed set of conventional short names is permitted**, because each is either forced by the language or so universal that a longer form would be less clear, not more:
  - `err` for an `error` value (naming it `error` would shadow the builtin type);
  - `ctx` for a `context.Context` (naming it `context` would shadow the package);
  - `t`, `b`, `f` for `*testing.T`, `*testing.B`, `*testing.F` in tests;
  - single letters for pure loop indices (`i`, `j`);
  - the `Err` prefix on exported sentinel error values (for example `ErrMissingURL`) - the universal Go convention that pairs with `errors.Is`; `Error`-prefixed names would be actively non-idiomatic.
- **Method receivers are short (1-2 characters) and consistent**, following the Go standard style (for example `func (c *Configuration)`). This is the one place Go's own style guide mandates brevity, and our tooling and every Go reader expect it, so we follow it rather than fight it. Pick a receiver per type and use it on every method of that type.
- **Filenames are spelled out too, with one canonical exception.** Use `configuration.go`, not `config.go`. The exception is `doc.go` - the long-established Go convention for a package's documentation file, which tooling and readers expect by that exact name, so we keep it rather than renaming it to `documentation.go`. (The `_test.go` suffix is a required toolchain convention, not a naming choice.)

This is a deliberately strong stance. It keeps almost the entire surface in plain words while still reading as idiomatic Go, because the only short names left are the ones Go itself treats as conventional.

## Commentary

Every comment is read by a consumer of the boundary it sits on, so it describes the contract of that boundary and nothing else. For public doc comments the consumer is an API end-user, often an AI builder. For internal doc comments and inline comments the consumer is a maintainer of this codebase, often an AI refactoring tool. Neither reader is served by narration of the authoring process. That every exported identifier carries a doc comment at all is required by [`standard.md`](standard.md) - this section governs what any comment may say.

Concretely:

- **Contract, not rationale ("what", not "why").** A comment states behavior, inputs, outputs, guarantees and caller obligations. When considering the modification or refactoring of a comment, consider that:
    - The reasoning behind a design often belongs in [`decisions.md`](decisions.md), but check the advice at the top of that file for when to use it and when not to use
    - Working procedures belong here in [`DEVELOPMENT.md`](DEVELOPMENT.md)
    - A comment that argues for its own design has leaked
- **Self-contained at the boundary.** Never document an identifier by pointing at its neighbors: no "mirrors the reference implementation", no "same pattern as package X", no "the Y job relies on this", nor any other note about what upstream or downstream code happens to do. When a collaborator's behavior constrains this interface, state the resulting obligation as this interface's own ("the path is not escaped here, so it must be escaped on query assembly") without touring the collaborator.
- **No provenance notes on received values.** A type whose values a caller receives rather than constructs states what a value is and guarantees, never which function returns it: no "instances come only from [X]". The producer's own signature and doc carry that fact, and unexported fields already enforce exclusive construction at compile time. Pointing a type the caller must build at its constructor states an obligation, not provenance ("the zero value is not usable, so build it with [New]"), and an error naming the operations that report it states the condition under which it arises ("reported by [X] when ...").
- **No language tutoring.** State what is returned or accepted, as types and sentinels. Do not teach `errors.Is` / `errors.As` mechanics, struct tag semantics or any other standard craft - the reader, human or AI, knows their tools.
- **Exception: runnable examples may frame a convention.** Examples render on pkg.go.dev as consumer-facing documentation, and part of what an example demonstrates can be community convention rather than anything the API's types enforce. Where the intended shape is invisible from signatures alone (middleware wrapping, functional options), an example's comments may name the convention and its shape in a sentence or two. The tutoring line still holds: frame the convention being demonstrated, never teach the language itself.
- **No roadmap narration.** Nothing "arrives in a later block", is "the first real X" or holds "for now". Comments describe the code as it stands, timelessly. Sequencing lives in the issue tracker and history lives in git.
- **Each fact once, at the site that owns it.** Type-level guarantees such as immutability or concurrency safety are documented on the type, not restated by every method or call site that touches it.

These rules apply to every commentary surface in the repository: Go doc comments (exported and internal), inline comments and comments in scripts, workflows, SQL and configuration files.

## Testing conventions

If you are newer to Go, a few conventions are worth knowing - they are stricter and more file-layout-driven than many other ecosystems.

- **Tests live next to the code they test**, in the same directory, in files ending `_test.go`. There is no separate `tests/` folder. The `_test.go` suffix is special: those files are compiled only by `go test` and never ship in a consumer's build.
- **Tests are discovered by convention, not registration.** [`go test`](https://pkg.go.dev/cmd/go#hdr-Test_packages) runs every function whose name and signature match a known shape - `func TestXxx(t *testing.T)`, `func BenchmarkXxx(b *testing.B)`, `func ExampleXxx()`, `func FuzzXxx(f *testing.F)`. No attributes, no suite registry, no config.
- **Prefer the standard library.** We write tests with [the standard `testing` package](https://pkg.go.dev/testing) and explicit checks (`if got != want { t.Errorf(...) }`), usually as table-driven tests with a subtest per case via [`t.Run`](https://pkg.go.dev/testing#T.Run). We are not (yet) using an assertion or mocking library; keep new tests stdlib-only unless a change is discussed first.
- **Runnable examples are documentation.** `ExampleXxx` functions are compiled and executed by `go test` and rendered on pkg.go.dev, so our usage docs cannot drift from reality. Add an example for notable exported API.
- **In a test helper, call [`t.Helper()`](https://pkg.go.dev/testing#T.Helper) first.** That makes a failure point at the line that called the helper rather than at a line inside it, which keeps failures readable as helpers accumulate. It's acceptable to skip the `t.Helper()` call in a helper method implementation that only does setup and [`t.Cleanup`](https://pkg.go.dev/testing#T.Cleanup) but never calls [`t.Error`](https://pkg.go.dev/testing#T.Error) or [`t.Fatal`](https://pkg.go.dev/testing#T.Fatal), since it has no failure line to relocate.

### Test from the outside in: prefer the external test package

A Go test file in a package directory can declare one of two packages, and both are allowed to sit side by side in the same directory:

- `package foo_test` - an **external test package**. It can only see `foo`'s exported (public) API, exactly as a real consumer would. This is sometimes called *black-box* (or *behavioral* / *clear-from-the-outside*) testing.
- `package foo` - an **in-package test**. It compiles as part of `foo`, so it can reach unexported (private) identifiers. This is sometimes called *white-box* (or *structural*) testing.

**Our default is the external test package (`foo_test`).** Testing through the public API tests what consumers actually use, keeps tests decoupled from internal details so refactoring internals does not spuriously break tests, and applies healthy pressure to keep the exported surface usable. Reach for an in-package test (`foo`) only when you genuinely need to exercise internals that are not observable through the public API, and prefer to keep such tests few and clearly named (for example `something_internal_test.go`).

A note on terminology: the industry terms for these are "black-box" and "white-box" testing, and we mention them so the mapping is clear, but we prefer the precise, Go-native framing - *external test package* versus *in-package test* - which also sidesteps loaded language.

## Raising the Go consumer floor

When a new Go major ships, the oldest major still supported by the Go project rises and our floor follows.
Major Go releases happen infrequently, twice a year in February and August, so the task of raising the consumer floor is classed as a 'manual' job for which you run [the `raise-consumer-floor.sh` script](./scripts/raise-consumer-floor.sh) locally:

```bash
./scripts/raise-consumer-floor.sh
```

It requires the Git working tree to be clean and creates a commit for the bump for you.

At this point it's worth checking for floor-relative claims in [`decisions.md`](decisions.md) because some entries might justify a design by where a Go version sits against the floor.
Search that document for "floor" and rewrite anything the raise has invalidated.

See also: [Running checks at the consumer floor](#running-checks-at-the-consumer-floor)

## Local development environment troubleshooting and tips

### Upgrading Go from the terminal (CLI) on macOS

Periodically required, often preferable in terms or predictability and control over downloading via browser and then running the installer interactively.
For example, upgrading from version `1.26.5` to version `1.26.6` (in this case for an M5 MacBook Pro, thus Apple silicone).

```bash
curl -fsSLO https://go.dev/dl/go1.26.6.darwin-arm64.pkg
shasum -a 256 go1.26.6.darwin-arm64.pkg
# expect 477fb579ba85bbfd44120a0a51068bfba99300968e1d9df35d9d89e316a38733 (published at https://go.dev/dl/)
# installer(8) requires -target (it is not defaulted); / selects the booted volume
sudo installer -pkg go1.26.6.darwin-arm64.pkg -target /
go version             # expect go1.26.6 darwin/arm64
./scripts/vulncheck.sh
./scripts/check-fast.sh
```

And another example, on the same machine, but for a [Lima](https://lima-vm.io/)-provided Ubuntu VM installation (would be the same for any `arm64` Linux host or guest OS):

```bash
curl -fsSLO https://go.dev/dl/go1.26.6.linux-arm64.tar.gz
sha256sum go1.26.6.linux-arm64.tar.gz
# expect d0507e9e9d7fe012aae570108cbd76c15de879e17130ab8cb90d4d7445cb1f2e (published at https://go.dev/dl/)
# Remove the old tree first: tar -x unions files rather than replacing the target,
# so a stale file from 1.26.5 would otherwise linger silently under /usr/local/go.
# This is the sequence go.dev/doc/install prescribes for Linux upgrades.
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.26.6.linux-arm64.tar.gz
go version             # expect go1.26.6 linux/arm64
./scripts/vulncheck.sh
./scripts/check-fast.sh
```

### When the vulnerability scan fails on the Go standard library

`govulncheck` checks both the dependencies in our `go.mod` files and the standard library of whichever Go toolchain runs the scan. The `Found in:` line of a finding tells you which case you have. A module path such as `golang.org/x/crypto@v0.32.0` is a dependency, fixed in the affected module's `go.mod`. `Standard library` with a version like `crypto/tls@go1.26.4` means the flaw is in the machine's Go toolchain, which no repository file declares or can fix, and which never reaches consumers - the SDK ships as source, so their binaries carry their own toolchain's standard library. A concrete example of hitting this was [GO-2026-5856](https://pkg.go.dev/vuln/GO-2026-5856) ([CVE-2026-42505](https://www.cve.org/CVERecord?id=CVE-2026-42505)), where scans running with go1.26.4 failed until the machine's toolchain moved to go1.26.5 with no repository change needed.

Nothing here pins a build toolchain (we have directives that set a consumer floor, in respect of what we publish to users - see [`decisions.md`](decisions.md)) and Go only [switches toolchains](https://go.dev/doc/toolchain) when a version is named explicitly, so the remedy is to name one, choosing how long it should stick:

- **One run:** prefix the command, for example `GOTOOLCHAIN=go1.26.5 ./scripts/vulncheck.sh`. The toolchain is downloaded (checksum-verified) and used for that invocation only, with nothing persisted. Good for confirming a diagnosis, and later plain runs remain on the installed Go.
- **Machine-wide until removed:** `go env -w "GOTOOLCHAIN=$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -n 1)+auto"` resolves the newest release and writes it to your user-level Go environment file (located by `go env GOENV`), governing every go command you run anywhere. Remove it with `go env -u GOTOOLCHAIN` once you have done the actual upgrade below, as the setting outlives Go installations and would keep forcing the older version.
- **The actual upgrade:** install a Go at or beyond the `Fixed in:` version via your original install channel. For the official distribution that means running the newest installer from [go.dev/dl](https://go.dev/dl/), which replaces `/usr/local/go` (Go has no self-update command).

**CI needs no action**: the `vulnerabilities-check` job resolves `go-version: stable` against GitHub's [go-versions manifest](https://github.com/actions/go-versions/blob/main/versions-manifest.json) on every run, so it picks up a fixed release as soon as the manifest lists it, typically within a day or two.

Do not commit a `toolchain` line to `go.work` or a `go.mod` in response: under the default `GOTOOLCHAIN=auto` it would hoist the CI matrix's consumer version floor leg onto the newer toolchain, ending the proof that the published floor still builds, and it would be a convention change requiring a [`decisions.md`](decisions.md) entry.

### Upgrading the pinned Supabase CLI

Integration tests run against a version-pinned Supabase CLI release, fetched and SHA-256-verified by [`scripts/integration-test.sh`](scripts/integration-test.sh) on first use. That script is the single source of truth for the pin: `SUPABASE_CLI_VERSION` plus the four `cli_sha256` values for the platforms we support (Linux and macOS, on `amd64` and `arm64`). A bump touches only that one file.

For example, upgrading from `2.109.1` to `2.114.0`:

```bash
export VERSION=2.114.0
curl -fsSL "https://github.com/supabase/cli/releases/download/v${VERSION}/checksums.txt" \
  | grep -E "supabase_${VERSION}_(linux|darwin)_(amd64|arm64)\.tar\.gz$"
```

Copy each of the four SHA-256 values printed by that command into the matching `cli_sha256=` line of the `case "${cli_os}_${cli_arch}"` block in `scripts/integration-test.sh`, and set `SUPABASE_CLI_VERSION` to the new version (without the `v` prefix). Then run the integration tests to prove the pin fetches, verifies and starts the stack:

```bash
./scripts/integration-test.sh
```
