# Development Decisions for `supabase-go`

This document has been created to capture decisions that have been made during development on this SDK which felt like worth recording for future reference.
It's designed to be quick and friction-less to populate, a friction log inspired micro decisions list, often expected to be imperfect but with the ethos of "something is better than nothing" in terms of what we capture.

There is, of course, the chance that this document might drift away from what's present in the wider codebase.
Such drift will only ever be accidental in nature and, as such, this document is to be read carefully and judiciously.

All decisions documented here clearly state 'why', justifying the 'what'.
They're loosely held, acknowledging that reasons change and rebalance over time, so we should feel able to change or revert decisions as we learn more about what this codebase needs.
The ideal situation is that this document will be updated as that happens, as an atomic component of codebase changes that reflect that decision change.

## No `Makefile` or task runner

**What**:  
CI and local dev use plain `go` commands only.

**Why**:  
A `Makefile` is a borrowed-from-C convention that earns its place only when a repo orchestrates non-Go work (docker, migrations, codegen, cross-compile, release packaging).
A pure multi-module library has none of that - every task is a single `go`-toolchain invocation.

## CI uses only first-party Actions (GitHub's `actions` org)

**What**:  
The only actions permitted are actions/checkout and actions/setup-go; no golangci/* or golang/* actions.

**Why**:  
A wrapper action is a CI-only black box a developer can't run locally.
Keeps CI transparent and the supply-chain surface minimal.

## Linting is native and unbundled (no `golangci-lint`)

**What**:  
Run each linter as a plain `go vet` / `go run <tool>@<version>` command rather than via the `golangci-lint` aggregator and a `.golangci.yml`.

**Why**:  
Every CI check must be byte-for-byte reproducible at a developer's workstation - the same command locally and in CI.
The aggregator hides that behind one bundled tool and config.
The trade-off accepted: more verbose CI/local instructions, in exchange for transparency and Go-nativeness.

## Linter selection filtered by "prevent expensive/breaking-API mistakes"

**What**:  
Keep `gofumpt`, `go vet`, `staticcheck`, `errcheck`, `revive` (+ `govulncheck`).
Don't adopt `cyclop`, `exhaustruct` or `goimports` for now.

**Why**:  
The guiding test is whether a check helps avoid mistakes that would later force a major refactor or a breaking public-API change.
`revive` earns its place because its default rules enforce doc comments and idiomatic naming on the exported surface (a bad exported name today is a breaking rename tomorrow).

## The two Go-version environments are kept discrete

**What**:  
The `go` directive in published modules (`1.22.0`) is separate from, and unaffected by, the toolchain CI and tooling run on (latest stable).

**Why**:  
They are different concerns: the published `go` directive is a compatibility contract for the consumer's unknown environment (conservative floor), while the CI/lint toolchain is our own deterministic environment (latest, our choice).
A latest toolchain compiles a go 1.22 module fine.
Tool-pinning machinery (e.g. Go 1.24 tool directives) must never live in the published modules, or it would drag our environment's needs into the consumer's contract and force the floor up.

## Use a committed go.work workspace for intra-repo module resolution
  
**What**:  
The multi-module repository (`root`, `core`, `postgrest`, and future domain modules) wires its internal cross-module dependencies through a single `go.work` file committed at the repository root, rather than through replace directives in each `go.mod` file.
Each module's `go.mod` file declares its sibling dependencies with ordinary require lines carrying the zero pseudo-version (`v0.0.0-00010101000000-000000000000`) until real tags exist.
The workspace's use directives supply the actual source for every in-repo build, locally and in CI.
The published `go` directive stays at the conservative consumer floor (`1.22.0`) independently of the toolchain version CI runs.

**Why**:  
Pre-tag, a module that imports an unpublished sibling cannot resolve it without either `replace` directives or a workspace.
`go.work` is the purpose-built mechanism (Go 1.18+) and gives a cleaner separation of "what we publish to customers" (the `go.mod` files, free of dev-only redirects) from "how we develop locally" (one workspace file), stating the wiring once instead of repeating `replace … => ../core` in every consumer.
Committing it is the Go-team-endorsed practice for monorepos ([golang/go#53502](https://github.com/golang/go/issues/53502) explicitly declined a "never commit" warning; the relative paths are identical for every clone, gopls configures multi-module editing from it, and Dependabot understands it), and it is provably safe for consumers: `go.work` is never included in a published module zip and is ignored by `go get`, so it cannot affect anyone importing the SDK.
The one workspace hazard - the overlay masking a missing `require` - cannot bite at this stage, as there are zero external dependencies.
The decision is cheaply reversible (delete `go.work`, add `replace` blocks).

## Error model

### Sentinel errors are compile-time constants, not package variables

**What**:  
Exported sentinel errors (e.g. `core.ErrMissingURL`) are declared as `const` values of an unexported string-backed error type, not as `var`s built with `errors.New`.

**Why**:  
An exported package-level `var` is writable by any importing package (`core.ErrMissingKey = nil` compiles), so the standard `var = errors.New(...)` idiom leaves a public SDK's sentinels reassignable - protected only by convention.
A string-backed error type can be `const`, which the compiler enforces as immutable, removing that footgun entirely.
Consumers use the sentinels identically (`errors.Is`); the only behavioural change is value- rather than pointer-identity comparison, which is safe for distinct messages.

### const sentinels for kinds, struct types for data

**What**:  
Dataless "which kind of failure" errors are exported `const` sentinels (a string-backed error type), matched with `errors.Is`.
Failures that carry data a caller may need are struct error types with typed fields, read back with `errors.As`, optionally wrapping a sentinel via `Unwrap`.
Dynamic context is added by wrapping (`fmt.Errorf("...: %w", value, err)`) - that is, we do not capture stack traces as Go's idiom is wrapped context, not stack frames.

**Why**:  
Go has no rich exception hierarchy, so these two shapes span the spectrum: identity-style matching for kinds, programmatic field access for data, without leaking internal types onto the public surface.
Value comparison of the const sentinels is safe because the error type is unexported and package-local, so the type itself acts as a namespace - errors from different packages can never compare equal even with identical messages, and same-package clashes are avoided by keeping messages distinct and package-prefixed (e.g. `core: ...`).
Sentinel immutability is covered by the separate "sentinel errors are compile-time constants" decision.

### Error messages carry a package prefix, applied once in `Error()`

**What**:  
Every error message from a package is prefixed with that package's name (`core: project URL is required`), and the prefix is the importable package name, never a sub-concept or type within it (not `configuration:`).
The prefix is written once, in the string-backed type's `Error()` method (`return "core: " + string(e)`), so each sentinel definition carries only its own distinct message text rather than repeating the prefix on every declaration.

**Why**:  
Naming the originating package is the dominant Go convention - the standard library does it everywhere (`json:`, `http:`, `os:`) - and it preserves provenance once an error is wrapped, logged or surfaced far from where it was created.
The package is the unit a consumer imports and reasons about, so it is the right granularity for provenance. Finer-grained "which kind of failure" information is carried by the error's identifier and type (`ErrMissingURL`, `configurationError`) and its message text, not duplicated into the prefix.
Package granularity also stays consistent as a package grows more error sources (for example `transport` alongside `configuration` in `core`), so every error from the package reads with the same token regardless of which file or type produced it.
Centralising the literal in `Error()` rather than baking `core: ` into each sentinel removes the repetition. Our single string-backed error type gives us one render chokepoint that the scattered `errors.New` calls in the standard library do not have.
The rendered prefix does not affect `errors.Is`, which compares the underlying sentinel values (the unprefixed message strings). The prefix is purely for the human reading the message.

## Everything executed from outside the repo is digest-pinned (Actions and tooling)

**What**:  
Every GitHub Actions `uses:` is pinned to a full 40-character commit SHA with a trailing version comment - first-party `actions/*` included, no exemption.
The Go tooling (linters, govulncheck) is pinned by checksum in a dedicated `tools/go/go.mod` + committed `tools/go/go.sum`.
GitHub's "require SHA-pinned actions" setting is enabled for this repository.

**Why**:  
Actions have no lockfile and version tags are mutable git pointers - re-pointing a tag runs attacker code with the workflow token and secrets - so a commit SHA (and, for Go tools, a committed `go.sum` checksum) is the only immutable reference.
On-demand refreshes keep "pinned" and "latest" close without scheduled churn, and Dependabot security updates still catch advisories with a reviewable diff, so pinning trades off against neither freshness nor safety.
This follows Supabase's org-wide policy ([Git & GitHub](https://app.notion.com/p/c4922b923c544a2ea0377d60a0f21aec), Linear [PRODSEC-21](https://linear.app/supabase/issue/PRODSEC-21/) and [PRODSEC-67](https://linear.app/supabase/issue/PRODSEC-67/)) and extends the same discipline to our Go tooling.

## Regular dependency update cadence is manual, not driven by dependabot

**What**:  
Versions are resolved to latest at setup and refreshed on demand by the maintainer while the repo is under solo active development.
Dependabot security updates stay enabled via repo settings so advisories still raise a PR, but scheduled version-update PRs are deferred until the repo opens to broader contribution.

**Why**:  
During early development on this codebase it's going to be actively iterated upon by a single developer and so is not likely to be left idle for long periods of time with no activity.
This means that the benefits of regular (weekly) dependabot PRs are less obvious, and perhaps might even turn into a distraction or nuisance to that singular development flow.

## No root `.gitignore`

**What**:  
The repository carries no root `.gitignore`.
An exception is [`tools/node/.gitignore`](tools/node/.gitignore), scoped to the npm tooling folder so we ignore the `node_modules` folder that `npm ci` materializes there.

**Why**:  
An empty-of-purpose ignore file is configuration without a need - the same reasoning that keeps `.editorconfig` out - so a `.gitignore` earns its place only in the change that first produces an artifact worth ignoring, scoped to where that artifact appears, and not before.
The Go build still emits no artifacts, coverage output or environment files, so the tree needs no root ignore file.
The spell-check tooling is the first thing to produce an ignore-worthy artifact - `npm ci` populating `node_modules/` - so an ignore file earns its place there and then, scoped to `tools/node/` rather than a catch-all at the root.
`go.work.sum` remains neither committed nor ignored, so its first appearance once an external dependency lands still shows up in `git status` for a considered call then.

## Public API doc comments use the Go doc-comment syntax (links, lists, prose)

**What**:  
Doc comments on exported identifiers use the Go 1.19+ "Go Doc Comments" syntax, not plain prose alone.
The features we rely on:

- Doc links - `[Name]`, `[pkg.Name]` and `[pkg.Type.Method]` - to cross-reference other identifiers and packages.
- Bullet or numbered lists for enumerable behaviour, such as the set of sentinel errors a constructor returns.
- Parameters and return values referenced by name in running prose (Go has no `@param` or `@return` tags; the rendered signature supplies the parameter list).
- Runnable `ExampleXxx` functions as executable usage documentation.

We do not use Markdown in doc comments. Bold, italics and inline backtick code spans are unsupported, so backticks never appear in doc comments because they would render literally.

**Why**:  
This is the one syntax that `gofmt` canonicalises and that every Go documentation consumer renders identically: `go doc` at the command line, pkg.go.dev on the web and gopls on editor hover.
One comment therefore serves all three without divergence.
Doc links become navigable cross-links on the rendered page, lists make conditions like the error-return set scannable, and runnable examples cannot drift from the code because `go test` executes them.
Holding to the standard syntax lets `gofmt` keep formatting consistent and stops contributors inventing ad hoc conventions.

## Domain clients are reached through context-free accessor methods

**What**:  
`NewClient` constructs every domain client up front, and the root client exposes each through an accessor method that returns the concrete handle (`Database() *postgrest.Client`, later `Auth() *auth.Client`).
The accessors take no `context.Context` and return no error.
`context.Context` is taken only by the terminal methods that perform I/O, such as the database `Execute`.

**Why**:  
Accessor methods keep the handle fields unexported, so the client stays immutable and safe for concurrent use, which an exported field would not be - a public field is reassignable and races if written while read.
Construction does no I/O - `NewClient` parses the project URL and wraps the HTTP transport, with no network call - so there is nothing at access time for a context to bound or cancel, and nothing that can fail.
Google's SDKs are the cautionary contrast. Firebase's `app.Auth(ctx)` and `app.Firestore(ctx)` take a context and return an error because they lazily construct clients that resolve credentials and dial connections, and the context is then kept for the client's life: the `cloud.google.com/go` docs warn "Do not set a timeout on the context passed to NewClient: dialing happens asynchronously, and the context is used to refresh credentials in the background", and `golang.org/x/oauth2` states its client "is not valid beyond the lifetime of the context".
That shape only earns its place when the returned client owns background work bound to the context, and it carries a footgun when it does not: a request-scoped context passed to such a constructor and then cached breaks the client's background refresh once the request ends.
Our handles own no background work, so a context parameter would import that footgun for no gain.

## One HTTP customisation seam, and a sealed HTTPClient across modules

**What**:  
The only way a caller customises outbound HTTP is `WithHTTPClient`: they supply an `*http.Client` whose `Transport` is any `http.RoundTripper` chain they want, and `core` wraps its own auth `RoundTripper` (apikey and Authorization injection) in front of it.
There is deliberately no `WithRoundTripper` or middleware option.
Internally, `core` hands each domain module a one-method `HTTPClient` interface (`Do(*http.Request) (*http.Response, error)`), never the concrete `*http.Client`.

**Why**:  
The single seam matches the dominant Go convention. Google's API libraries and Stripe expose only a whole-client seam, and Google's own docs tell callers to add behaviour "via RoundTripper middleware" on their own client rather than through an SDK option. AWS SDK v2 is the exception, but its extra knob is a bespoke Smithy middleware stack, not an `http.RoundTripper` shortcut, so it is no precedent for one. A `WithRoundTripper` convenience can be added additively later if demand appears, so nothing is foreclosed.
Handing out the interface rather than the `*http.Client` stops the configured transport being swapped out through the accessor - a caller holding the concrete client could set `Transport = nil` and silently disable auth, or race on it - and it keeps the `core` public surface small, which is part of the `v1` promise.
The interface is named `HTTPClient` with a single `Do` method, following AWS SDK v2's interface of the same name and shape. `Do` is chosen because `*http.Client` already has that method, so the standard client satisfies the interface with no adapter, and the same one-method contract appears as the `HttpRequestDoer` that `oapi-codegen` generates in Supabase's own Auth code.

## supabase.Option is an alias of core.Option

**What**:  
The root package's `Option` type is a type alias for `core.Option`, so a setting written for either works for both, and the root's convenience options (`WithHTTPClient`, `WithHeaders`) are the `core` options.

**Why**:  
Every option the plan gives the root client - custom HTTP client, global headers, the `slog` logger and tracing context - configures the shared `core` plumbing, so a shared type is enough and a second parallel option type would be waste.
The alias would only need to break if the root ever had to carry a setting `core` does not own, for example tuning one domain's behaviour from the root, which the plan does not call for.
Any such need would surface during the Alpha or Beta pre-releases, where changing the type is still free, so keeping the alias bakes in no known breaking change.

## The postgrest module is Supabase-agnostic in code but not a supported general-purpose client

**What**:  
The `postgrest` module carries no Supabase-specific behaviour - the `apikey` header, the `/rest/v1` base path and token handling live in `core` and the root - so its code could in principle talk to any PostgREST server.
It is not, however, a tested or supported general-purpose PostgREST client. It is documented as the Supabase Database client, and standalone use against a non-Supabase server is not promised.

**Why**:  
The agnostic-code claim is asserted cheaply, by the module boundary: `postgrest` imports and names none of the Supabase-specific pieces, which review and the build enforce, with no extra test infrastructure.
A supported general-purpose promise would cost far more - a bare PostgREST server stood up in CI, a way to build `postgrest` without the base URL and apikey it is handed today, and testing across PostgREST versions - none of which is planned for the first releases.
Keeping the promise narrow now forecloses nothing: promotion to a supported general-purpose client is additive (add the harness and a Supabase-free constructor) and breaks no existing Supabase user, mirroring how the JS SDK ships a standalone `@supabase/postgrest-js`.

## `SECURITY.md` and `CONTRIBUTING.md` are org-delegated, not repo-local

**What**:  
The repository carries no `SECURITY.md` or `CONTRIBUTING.md`.
Both are provided org-wide by `supabase/.github`, and a CI check asserts their absence here (covering the repo root, `.github/` and `docs/`).
`CODEOWNERS` stays repo-local.

**Why**:  
GitHub falls back to the organisation's `supabase/.github` files for any repository that lacks its own, so an org-level `SECURITY.md` and `CONTRIBUTING.md` already apply.
A repo-local copy would silently shadow the org default and drift from it, so asserting absence beats maintaining a duplicate.
The one posture that does not belong at org level - that external code contributions are not accepted before the first GA release - lives in `DEVELOPMENT.md` instead.
