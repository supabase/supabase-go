# Development Decisions for `supabase-go`

<!-- cSpell:ignore Cheney claude Cname Getenv iter mktemp openai pgrst pgx Seq sqlc vnd WHATWG -->

This document has been created to capture decisions that have been made during development on this SDK which felt like worth recording for future reference.
It's designed to be quick and friction-less to populate, a friction log inspired micro decisions list, often expected to be imperfect but with the ethos of "something is better than nothing" in terms of what we capture.

There is, of course, the chance that this document might drift away from what's present in the wider codebase.
Such drift will only ever be accidental in nature and, as such, this document is to be read carefully and judiciously.

All decisions documented here clearly state 'why', justifying the 'what'.
They're loosely held, acknowledging that reasons change and rebalance over time, so we should feel able to change or revert decisions as we learn more about what this codebase needs.
The ideal situation is that this document will be updated as that happens, as an atomic component of codebase changes that reflect that decision change.

**present-tense-only**: Every entry in this document justifies the codebase as it stands right now, never how it got here.
When a decision changes, rewrite its entry to describe the new present, or delete it outright when its subject or rationale no longer earns a place - git history is the only ledger of what came before (that is, the journey that the codebase took to get to its current state), so supersession notes and narration of renames or reversals are noise wherever they appear here.

While the entries in this document are presented as a series of lightweight Architectural Decisions Records (ADRs), this document is not append-only.
Deleting a stale entry is correct maintenance and therefore encouraged.

When the reason for something concerns only the file it sits in (a configuration file that exists to satisfy one tool, for example), state it in a comment in that file rather than as an entry here in [`decisions.md`](decisions.md), naming the tool that needs it when you write that comment.

## No `Makefile` or task runner

**What**:  
CI and local dev use plain `go` commands only.

**Why**:  
A `Makefile` is a borrowed-from-C convention that earns its place only when a repo orchestrates non-Go work (docker, migrations, codegen, cross-compile, release packaging).
A pure multi-module library has none of that - every task is a single `go`-toolchain invocation.

## CI uses only first-party Actions (GitHub's `actions` org)

**What**:  
The only step-level actions permitted are those owned by GitHub's first-party [`actions` org](https://github.com/actions/); no golangci/* or golang/* actions.
The one job-level `uses:` of a reusable workflow is Supabase's own SDK compliance gate in [`validate-capabilities.yml`](.github/workflows/validate-capabilities.yml).

**Why**:  
A wrapper action is a CI-only black box a developer can't run locally.
Keeps CI transparent and the supply-chain surface minimal.
The compliance gate is the accepted exception because its checks are defined org-wide in [`supabase/sdk`](https://github.com/supabase/sdk), so a local re-implementation would only drift from the canonical one.

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
The `go` directive in published modules (the consumer floor) is separate from, and unaffected by, the toolchain CI and tooling run on (latest stable).

**Why**:  
They are different concerns: the published `go` directive is a compatibility contract for the consumer's unknown environment, while the CI/lint toolchain is our own deterministic environment (latest, our choice).
A latest toolchain compiles a floor-versioned module fine.
Tool-pinning machinery must never live in the published modules, or it would drag our environment's needs into the consumer's contract and force the floor up.

## Use a committed go.work workspace for intra-repo module resolution
  
**What**:  
The multi-module repository (`auth`, `core`, `postgrest`, `supabase` and future domain modules) wires its internal cross-module dependencies through a single `go.work` file committed at the repository root, rather than through replace directives in each `go.mod` file.
Each module's `go.mod` file declares its sibling dependencies with ordinary require lines, which [`scripts/prepare-release.sh`](scripts/prepare-release.sh) pins to released sibling versions at release time.
The workspace's use directives supply the actual source for every in-repo build, locally and in CI.

**Why**:  
Every in-repo build must compile a module against its siblings' current source - unreleased changes included - rather than the released versions its require lines name, which needs either `replace` directives or a workspace.
`go.work` is the purpose-built mechanism (Go 1.18+) and gives a cleaner separation of "what we publish to customers" (the `go.mod` files, free of dev-only redirects) from "how we develop locally" (one workspace file), stating the wiring once instead of repeating `replace … => ../core` in every consumer.
Committing it is the Go-team-endorsed practice for monorepos ([golang/go#53502](https://github.com/golang/go/issues/53502) explicitly declined a "never commit" warning; the relative paths are identical for every clone, gopls configures multi-module editing from it, and Dependabot understands it), and it is safe for consumers: `go.work` is never included in a published module zip and is ignored by `go get`, so it cannot affect anyone importing the SDK.
The one workspace hazard is the overlay masking a missing or wrong `require`: every in-repo build resolves siblings from workspace source, so a `require` defect surfaces only in consumer builds.
[`scripts/check-module-paths.sh`](scripts/check-module-paths.sh) guards the path case: it fails when a workspace (published) module requires a first-party path that is not itself a workspace module, which a consumer could not resolve. [`scripts/check-release-consistency.sh`](scripts/check-release-consistency.sh) guards most of the version case, failing a require that names a version its sibling's changelog never declares. A missing require or a pin to the wrong declared version still rests on review, since there is no tidy gate yet (zero external dependencies).
The decision is cheaply reversible (delete `go.work`, add `replace` blocks).

## Integration tests and their shared fixtures are adjacent non-published modules

**What**:  
Each module's `integrationtest` directory is its own module - never published, absent from `go.work`, entered with `GOWORK=off` and resolving its requirements through `replace` directives to the local tree - and the fixtures the suites share (stack credentials, end-user signup) live once in the sibling [`integration-testing/testkit` module](integration-testing/testkit/).
Test-module import paths stay under the parent module's path (`…/postgrest/integrationtest`), and the files carry no build tag.

**Why**:  
A shared fixtures package can live nowhere inside the published set: a published module must not require a never-published path (`scripts/check-module-paths.sh` fails exactly that, because a consumer could not resolve it), and a copy per module drifts.
Adjacent modules keep every published `go.mod` consumer-resolvable while the test tier composes freely.
The nested import path preserves `internal` package access, which is prefix-based and indifferent to module boundaries.
The module boundary already isolates integration code from every `./...` a published module runs, so a build tag would gate nothing extra and editors need no build-tag configuration; the symbol extractor likewise skips directories carrying their own `go.mod`, so the `integrationtest` package name recurring across the repository cannot trip its package-name uniqueness guard.

## Error model

### Sentinel errors are compile-time constants, not package variables

**What**:  
Exported sentinel errors (e.g. `configuration.ErrMissingURL`) are declared as `const` values of an unexported string-backed error type, not as `var`s built with `errors.New`.

**Why**:  
An exported package-level `var` is writable by any importing package (`configuration.ErrMissingKey = nil` compiles), so the standard `var = errors.New(...)` idiom leaves a public SDK's sentinels reassignable - protected only by convention.
A string-backed error type can be `const`, which the compiler enforces as immutable, removing that footgun entirely.
Consumers use the sentinels identically (`errors.Is`); the only behavioral change is value- rather than pointer-identity comparison, which is safe for distinct messages.

### const sentinels for kinds, struct types for data

**What**:  
Data-less "which kind of failure" errors are exported `const` sentinels (a string-backed error type), matched with `errors.Is`.
Failures that carry data a caller may need are struct error types with typed fields, read back with `errors.As`, optionally wrapping a sentinel via `Unwrap`.
Dynamic context is added by wrapping (`fmt.Errorf("...: %w", value, err)`) - that is, we do not capture stack traces as Go's idiom is wrapped context, not stack frames.

**Why**:  
Go has no rich exception hierarchy, so these two shapes span the spectrum: identity-style matching for kinds, programmatic field access for data, without leaking internal types onto the public surface.
Value comparison of the const sentinels is safe because the error type is unexported and package-local, so the type itself acts as a namespace - errors from different packages can never compare equal even with identical messages, and same-package clashes are avoided by keeping messages distinct and package-prefixed (e.g. `configuration: ...`).

### Error messages carry a package prefix, applied once in `Error()`

**What**:  
Every error message from a package is prefixed with that package's name (`configuration: project URL is required`), and the prefix is the importable package name, never a sub-concept or type within it (not `transport:`).
The prefix is written once, in the string-backed type's `Error()` method (`return "configuration: " + string(e)`), so each sentinel definition carries only its own distinct message text rather than repeating the prefix on every declaration.

**Why**:  
Naming the originating package is the dominant Go convention - the standard library does it everywhere (`json:`, `http:`, `os:`) - and it preserves provenance once an error is wrapped, logged or surfaced far from where it was created.
The package is the unit a consumer imports and reasons about, so it is the right granularity for provenance. Finer-grained "which kind of failure" information is carried by the error's identifier and type (`ErrMissingURL`, `configurationError`) and its message text, not duplicated into the prefix.
Package granularity also stays consistent as a package grows more error sources (for example the internal `transport` package behind `configuration`), so every error from the package reads with the same token regardless of which code produced it.
Centralizing the literal in `Error()` rather than baking `configuration: ` into each sentinel removes the repetition. Our single string-backed error type gives us one render choke point that the scattered `errors.New` calls in the standard library do not have.
The rendered prefix does not affect `errors.Is`, which compares the underlying sentinel values (the unprefixed message strings). The prefix is purely for the human reading the message.

## Everything executed from outside the repo is digest-pinned (Actions and tooling)

**What**:  
Every GitHub Actions `uses:` is pinned to a full 40-character commit SHA with a trailing version comment - first-party `actions/*` included, no exemption.
The Go tooling (linters, govulncheck) is pinned by checksum in dedicated tool modules under `tools/go/`, each with a committed `go.sum`.
The spell checker (cspell) is pinned the same way one ecosystem over: its full dependency tree is locked by integrity hash in a committed [`tools/node/package-lock.json`](tools/node/package-lock.json), installed via `npm ci`.
GitHub's "require SHA-pinned actions" setting is enabled for this repository.

**Why**:  
Actions have no lockfile and version tags are mutable git pointers - re-pointing a tag runs attacker code with the workflow token and secrets - so a commit SHA (and, for Go tools, a committed `go.sum` checksum) is the only immutable reference.
On-demand refreshes keep "pinned" and "latest" close without scheduled churn, and Dependabot security updates still catch advisories with a reviewable diff, so pinning trades off against neither freshness nor safety.
This follows Supabase's org-wide policy ([Git & GitHub](https://app.notion.com/p/c4922b923c544a2ea0377d60a0f21aec), Linear [PRODSEC-21](https://linear.app/supabase/issue/PRODSEC-21/) and [PRODSEC-67](https://linear.app/supabase/issue/PRODSEC-67/)) and extends the same discipline to our Go tooling.

## gopls builds from its own tool module, apart from the other Go tools

**What**:  
[`tools/go/gopls`](tools/go/gopls/) is a dedicated module holding only the gopls tool directive, while every other Go tool shares [`tools/go`](tools/go/).

**Why**:  
gopls imports `golang.org/x/tools` internal packages, which carry no compatibility promise, so each gopls release requires the exact `golang.org/x/tools` commit it was built against.
In a shared module, minimum version selection merges every tool's requirements, and a sibling bump (for example revive requiring a newer tagged `golang.org/x/tools`) floats gopls's dependency past that commit, breaking its compilation.
A module of its own leaves gopls's requirement as the only `golang.org/x/tools` constraint, matching how the Go team ships gopls - installed standalone so its own `go.mod` governs.

## CI runner images are versioned labels, never `ubuntu-latest`

**What**:  
Every `runs-on` names a versioned image label (currently `ubuntu-26.04`), bumped deliberately when a new image is ready.

**Why**:  
Jobs lean on the image-provided baseline (Docker for the integration stack, Node for cspell and jq), and an alias migration swaps that baseline with no repository diff.
A versioned label turns the OS move into a reviewable one-line bump - the digest-pinning discipline above at the coarser granularity GitHub offers for images.

## `setup-go`'s implicit cache is disabled

**What**:  
Every `actions/setup-go` step sets `cache: false`.
Caching that earns its place is an explicit `actions/cache` step with a reasoned key, as the integration-test job does for the Supabase CLI binaries.

**Why**:  
The implicit cache keys on a root `go.mod`, which this `go.work`-rooted repository does not have, so its restore has never succeeded here.
The published modules are dependency-free, leaving a module cache little to restore, and the action's job-agnostic cache key would hand heterogeneous jobs one shared, first-save-wins entry.

## Regular dependency update cadence is manual, not driven by dependabot

**What**:  
Versions are resolved to latest at setup and refreshed on demand by the maintainer while the repo is under solo active development.
Discovery is scripted while application stays manual: read-only [`scripts/tools-audit.sh`](scripts/tools-audit.sh) reports where every pin sits against its canonical origin and the bump route for each, and never applies anything.
Dependabot security updates stay enabled via repo settings so advisories still raise a PR, but scheduled version-update PRs are deferred until the repo opens to broader contribution.

**Why**:  
During early development on this codebase it's going to be actively iterated upon by a single developer and so is not likely to be left idle for long periods of time with no activity.
This means that the benefits of regular (weekly) dependabot PRs are less obvious, and perhaps might even turn into a distraction or nuisance to that singular development flow.

## Public API doc comments use the Go doc-comment syntax (links, lists, prose)

**What**:  
Doc comments on exported identifiers use the Go 1.19+ "Go Doc Comments" syntax, not plain prose alone.
The features we rely on:

- Doc links - `[Name]`, `[pkg.Name]` and `[pkg.Type.Method]` - to cross-reference other identifiers and packages.
- Bullet or numbered lists for enumerable behavior, such as the set of sentinel errors a constructor returns.
- Parameters and return values referenced by name in running prose (Go has no `@param` or `@return` tags; the rendered signature supplies the parameter list).
- Runnable `ExampleXxx` functions as executable usage documentation.

We do not use Markdown in doc comments. Bold, italics and inline backtick code spans are unsupported, so backticks never appear in doc comments because they would render literally.

**Why**:  
This is the one syntax that `gofmt` canonicalizes and that every Go documentation consumer renders identically: `go doc` at the command line, pkg.go.dev on the web and gopls on editor hover.
One comment therefore serves all three without divergence.
Doc links become navigable cross-links on the rendered page, lists make conditions like the error-return set scannable, and runnable examples cannot drift from the code because `go test` executes them.
Holding to the standard syntax lets `gofmt` keep formatting consistent and stops contributors inventing ad hoc conventions.

## Domain navigation is context-free and cannot fail

**What**:  
`supabase.New` constructs every domain client up front and holds each in an unexported field.
The methods that reach domain behavior (for example, the fluent `From`) take no `context.Context` and return no error.
`context.Context` is taken only by the functions that perform I/O, such as the database `Collect`.

**Why**:  
Reaching domains through methods keeps the handle fields unexported, so the client stays immutable and safe for concurrent use.
Construction does no I/O - `supabase.New` parses the project URL and wraps the HTTP transport, with no network call - so there is nothing at access time for a context to bound or cancel, and nothing that can fail.
The context-and-error accessor shape (Firebase's `app.Auth(ctx)`) earns its place only when the returned client owns background work bound to that context, as `golang.org/x/oauth2` documents of its client ("is not valid beyond the lifetime of the context"), and it carries a footgun when it does not: a request-scoped context passed to such a constructor and then cached breaks the client's background refresh once the request ends.
Our handles own no background work, so a context parameter would import that footgun for no gain.

## One HTTP customization seam, and a sealed HTTPClient across modules

**What**:  
The transport customization seam is `WithHTTPClient` alone: the caller supplies an `*http.Client` whose `Transport` is any `http.RoundTripper` chain they want, and `configuration` wraps its own header-injecting `RoundTripper` in front of it.
There is deliberately no `WithRoundTripper` or middleware option.
Internally, `configuration` hands each domain module a one-method `HTTPClient` interface (`Do(*http.Request) (*http.Response, error)`), never the concrete `*http.Client`.

**Why**:  
The single seam matches the dominant Go convention. Google's API libraries and Stripe expose only a whole-client seam, and Google's own docs tell callers to add behavior "via RoundTripper middleware" on their own client rather than through an SDK option. AWS SDK v2 is the exception, but its extra knob is a bespoke Smithy middleware stack, not an `http.RoundTripper` shortcut. A `WithRoundTripper` convenience can be added additively later if demand appears.
Handing out the interface rather than the `*http.Client` stops the configured transport being swapped out through the accessor - a caller holding the concrete client could set `Transport = nil` and silently disable auth, or race on it - and it keeps the `configuration` public surface small, which is part of the `v1` promise.
The interface is named `HTTPClient` with a single `Do` method, following AWS SDK v2's interface of the same name and shape. `Do` is chosen because `*http.Client` already has that method, so the standard client satisfies the interface with no adapter.

## No module is served from the repository root

**What**:  
The repository root carries no `go.mod`.
Every published module lives in a subdirectory named after its package - `auth/`, `core/`, `postgrest/` and `supabase/`, the convenience entry point.
Consumers import the root client as `github.com/supabase/supabase-go/supabase`, never `github.com/supabase/supabase-go` itself.
No Go source file lives outside a module directory, which [`scripts/check-module-paths.sh`](scripts/check-module-paths.sh) enforces.

**Why**:  
pkg.go.dev renders the README it finds in a module's own directory, so a root-served module's documentation page carries the repository README - GitHub-audience content (status banner, module table, contribution pointers) that has no place in consumer API documentation.
The go command still treats a repository root without `go.mod` as an implicit module whose `go.mod` it synthesizes ([Go Modules Reference](https://go.dev/ref/mod#non-module-compat)), so anyone's `go get` of the root path has the proxy cache a pseudo-version of it, README included.
pkg.go.dev refuses a module that contains no packages ([pkgsite](https://github.com/golang/pkgsite/blob/b0feb34c6d91fdea7d471ec6026383042ba8aa12/internal/fetch/fetch.go#L305)), so while every Go file sits inside a module directory that pseudo-version stays an inert proxy entry, the README never renders there and each module's page stays scoped to what that module ships.
A single stray Go file at the root would hand the synthesized module a package and pkg.go.dev a page, which is what the check guards.
The cost accepted is a doubled segment in the entry module's import path (`supabase-go/supabase`), and in exchange the layout is uniform: every published module follows the one directory-per-module shape, with no special root case in scripts, docs or the workspace.

## The postgrest module is Supabase-agnostic in code but not a supported general-purpose client

**What**:  
The `postgrest` module carries almost no Supabase-specific behavior - the `apikey` header and token handling live in `core` and `supabase` - so its code could in principle talk to any PostgREST server.
The one Supabase convention it does carry is `New` deriving its base URL under the project's `/rest/v1` path.
It is not, however, a tested or supported general-purpose PostgREST client. It is documented as the Supabase Database client, and standalone use against a non-Supabase server is not promised.

**Why**:  
The agnostic-code claim is asserted cheaply, by the module boundary: beyond the `/rest/v1` mount, `postgrest` imports and names none of the Supabase-specific pieces, which review and the build enforce, with no extra test infrastructure.
A supported general-purpose promise would cost far more - a bare PostgREST server stood up in CI, a way to build `postgrest` without the base URL and apikey it is handed today, and testing across PostgREST versions - none of which is planned for the first releases.
Keeping the promise narrow now forecloses nothing: promotion to a supported general-purpose client is additive (add the harness and a Supabase-free constructor) and breaks no existing Supabase user, mirroring how the JS SDK ships a standalone `@supabase/postgrest-js`.

## The client presumes neither a deployment runtime context nor an API/Supabase key type

**What**:  
The key parameter to `supabase.New` (and `configuration.New`) is named neutrally as `apiKey`, never `publishableKey` or `secretKey`, and the SDK neither inspects the key nor assumes where the calling code runs.
A caller may pass any of the project's keys - a publishable key, a secret key or, while they last, a legacy `anon`/`service_role` key - and the SDK carries it as an opaque credential.

**Why**:  
Go can run on both sides of the trust boundary.
A backend may deliberately choose a publishable key to stay inside Row Level Security as a least-privilege posture rather than reach for the RLS-bypassing secret key ([Understanding API keys](https://supabase.com/docs/guides/getting-started/api-keys)), and a Go program compiled to WebAssembly is as public as any browser app, where only a publishable key is safe.
Not inspecting the key also keeps the SDK forward-compatible as key formats evolve.

## The project key travels only on the `apikey` header, never on `Authorization`

**What**:  
The transport sets `apikey` on every request and never sets `Authorization`.

An `Authorization: Bearer <jwt>` header is populated only by the application - per request or by an optional auth integration - when it acts for a signed-in end user.
It is never copied or otherwise derived from the project key.
A caller-supplied `Authorization` header should pass through untouched.

**Why**:  
Supabase's guidance is explicit - "Send publishable and secret keys on the `apikey` header only" ([Migrating to new API keys](https://supabase.com/docs/guides/getting-started/migrating-to-new-api-keys)) - the platform rejects the key on `Authorization: Bearer` unless its value exactly equals the `apikey` header ([Understanding API keys](https://supabase.com/docs/guides/getting-started/api-keys), known limitations).
Mirroring the key onto both headers, as other SDKs do by default, is therefore correct only by landing inside that narrow exception - a coincidence, not a design.
`apikey` alone produces the intended Postgres role with no precedence logic to reconcile (publishable is `anon`, publishable plus an end-user JWT on `Authorization` is `authenticated`, secret is `service_role`), and reserving `Authorization` for the end-user token draws the "what is calling" against "who is signed in" boundary cleanly at the transport, so a later per-request user token simply takes effect.

## Legacy keys are not verified against the `apikey`-only transport, an accepted risk

**What**:  
This SDK is not tested against a legacy `anon` or `service_role` key.
The transport is validated only against the contemporary `sb_publishable_...` and `sb_secret_...` keys.
Whether a legacy `anon` JWT sent on the `apikey` header alone, with no `Authorization`, resolves to the `anon` role is left unverified and not promised.

**Why**:  
Contemporary keys are current best practice and the only keys new projects receive [since 1 November 2025](https://supabase.com/changelog/29260-upcoming-changes-to-supabase-api-keys), so building and testing solely against them concentrates effort where it matters for every new consumer.
The cost is a bounded uncertainty rather than a known defect: the legacy path may well work, since a legacy key's role claim rode the `Authorization` header and PostgREST ["switches into the anonymous role"](https://docs.postgrest.org/en/stable/references/auth.html) when a request carries no JWT, but it stays unverified.
The exposure also shrinks on its own, because the same timeline deletes legacy keys at the end of 2026.

## `SECURITY.md` and `CONTRIBUTING.md` are org-delegated, not repo-local

**What**:  
The repository carries no `SECURITY.md` or `CONTRIBUTING.md`.
Both are provided org-wide by `supabase/.github`, and a CI check asserts their absence here (covering the repo root, `.github/` and `docs/`).
`CODEOWNERS` stays repo-local.

**Why**:  
GitHub falls back to the organization's `supabase/.github` files for any repository that lacks its own, so an org-level `SECURITY.md` and `CONTRIBUTING.md` already apply.
A repo-local copy would silently shadow the org default and drift from it, so asserting absence beats maintaining a duplicate.
The one posture that does not belong at org level - that external code contributions are not accepted before the first GA release - lives in `DEVELOPMENT.md` instead.

## Merge strategy and commit conventions

Both decisions below deliberately diverge from the approach taken by other Supabase repositories - the "house defaults" (squash-only merging, Conventional Commit PR titles) - including those in the SDK domain.
Those defaults serve downstream release automation, which wants exactly one conventional commit per PR from which to infer changelogs and version bumps.
This SDK is taking a progressive changelog update approach, where changelog-worthy updates to the codebase are required to atomically submit a changelog entry under the 'Unreleased' heading for relevant modules.
Each module's next version is then chosen by hand at release time, from the entries under its 'Unreleased' heading.

### PRs land as merge commits, not squashes

**What**:  
Repository merge settings enable only "Allow merge commits"; squash and rebase merging are disabled.
Every PR lands with its individual commits as ancestors of `main`, under a merge commit recording the PR boundary.

**Why**:  
A squash merge keeps the granular history only as GitHub platform metadata (the PR's Commits tab, backed by hidden `refs/pull/N/head` refs), not in the repository: a fresh clone sees one commit per PR, and `git log`, `git blame` and `git bisect` cannot reach the individual steps.
Merge commits keep that history in Git itself, portable to any clone or mirror and addressable by every Git tool, while GitHub-side PR metadata (review threads, per-commit checks) is identical under either strategy.
The standard objection to merge commits - that intermediate commits are WIP noise which pollutes `main` and defeats `bisect` - does not apply under the working discipline here: every commit moves the codebase from one working state to another.
The one-entry-per-PR reading that squash exists to provide remains available through `git log --first-parent main`, and squash has no inverse, since discarded ancestry cannot be recovered from the repository afterwards.
The trade-off accepted: the full log of `main` is busier than a squash log, and the clean-history guarantee rests on solo discipline rather than enforcement.
When the repo opens to external contributions that guarantee weakens, so this is revisited then - a cheap change, as enabling squash is a repository setting that applies only to future merges.

### No Conventional Commits in commit messages or PR titles

**What**:  
Commit messages and PR titles are ordinary well-formed Git messages - an imperative summary line, with a body explaining why where needed - carrying no `type(scope):` grammar and no `BREAKING CHANGE` footers.

**Why**:  
Conventional Commits is a machine-facing grammar whose purpose is to let release tooling infer version bumps and generate changelogs.
In this SDK both are done by hand, so no machine would read the grammar.

## Agent guidance lives in `.agents/skills`, and a root `.gitignore` keeps other agent surfaces out

**What**:  
Direct AI guidance, where that guidance is designed primarily for consumption by agents, is contained within [`.agents/skills/`](.agents/skills/), whose [README](.agents/skills/README.md) states the full approach (progressive disclosure, routing, etc..).
A root [`.gitignore`](.gitignore) aims to keep other agent entry points out, for cleanliness (for example, `.claude/` and `.cursor/`).

**Why**:  
How we equip AI coding agents is a concern about how this repository is worked on, not a decision about the SDK's code, so this decision record carries only the signpost and the reason the tree excludes what it does.
The agent surface is deliberately singular: a second entry point, branded or neutral, would only duplicate the metadata the skill mechanism already loads at session start or drift from it over time (maintainability concern).

## `sdk-compliance.yaml` is sparse

**What**:  
[`sdk-compliance.yaml`](sdk-compliance.yaml) declares only the canonical [`supabase/sdk`](https://github.com/supabase/sdk) capability ids this SDK actually implements, omitting those that would end up being listed as `not_implemented`.
[`validate-capabilities.yml`](.github/workflows/validate-capabilities.yml) gates the declarations in CI through the canonical reusable Go compliance workflow from [`supabase/sdk`](https://github.com/supabase/sdk).

**Why**:  
The canonical tooling in `supabase/sdk` treats a missing id as `not_implemented` everywhere it matters (parity scoring, site generation, the CLI's own informational-only "not declared" listing), and its README says the file is sparse by design.
A full declaration would be over 200 lines of mostly ceremony, and would rot the way an explicit default always does: as `supabase/sdk` adds, renames or retires ids over the time it takes to build this SDK, an untouched `not_implemented` line for a since-renamed id becomes an "unknown feature id" CI failure that has nothing to do with any change we made.
A sparse file only ever names ids we've verified against the live matrix at the moment we implement them, so it can't go stale that way.

## Compliance capability ids never appear in doc comments

**What**:  
[`sdk-compliance.yaml`](sdk-compliance.yaml)'s `symbols:` list is the only place a `supabase/sdk` capability id (for example `client.request_configuration.custom_http_client`) is tied to the Go symbol that implements it.
Doc comments on the mapped symbol never restate the id.

**Why**:  
A capability id is internal `supabase/sdk` taxonomy: meaningless to someone reading the rendered comment on pkg.go.dev or via `go doc`, so putting it there is noise leaking into public documentation.
`sdk-compliance.yaml` already names the symbol in its own `symbols:` list, so the mapping is fully discoverable from that one file already as the canonical source of truth.
Also, a second copy in the doc comment is another place for it to go stale.

## Queries are pure values and the client appears only at execution

**What**:  
`postgrest.From` is a package-level function returning builders that carry only query state, with no client reference.
The generic read functions take the client explicitly - `Collect(ctx, client, query)`, reporting `ErrMissingClient` on nil - and the root `supabase.Client` reaches the Database through the `Database()` accessor rather than hoisting `From`.

**Why**:  
A client captured in the request model would pin otherwise-pure values to a constructed query for no representational need (as raised [in review on #27](https://github.com/supabase/supabase-go/pull/27#pullrequestreview-4790009535)).
Pure builders let query fragments live wherever values live, including package-level variables initialized before any client exists, and make the I/O dependency visible at the one call that performs I/O.

## Query builders are immutable values over an internal request model

**What**:  
`postgrest` exposes concrete builder value types (e.g. `QueryBuilder` and `FilterBuilder`) whose every method returns a new independent builder.
The request state they carry lives in `postgrest/internal/request`, a package whose single concern is the production of immutable `Request` values - unexported fields, read-only getters, copy-on-write `With*` methods, and no getter that returns reference-typed state.

**Why**:  
Some sibling SDKs mutate builders in place and consequently find themselves having to document "one chain per operation" caveats ([`supabase-swift`](https://github.com/supabase/supabase-swift)) or rely on single-threaded runtimes ([`supabase-js`](https://github.com/supabase/supabase-js)).
This Go SDK promises "safe for concurrent use by multiple goroutines" on the postgrest Client, and copy-on-write value builders deliver that with zero locks while letting callers fork partially-built queries.
Placing the state behind an internal package makes the immutability compiler-bounded rather than convention-across-the-codebase: only that one small, exhaustively-testable package can even express a mutation, and `internal/` keeps the micro-API off the public surface so its representation can change freely.
Reference types are avoided inside the model (the parameter list is an ordered slice of immutable pairs, cloned on write) so a struct copy is a genuinely deep copy.

## Request parameters are an ordered multimap, not a map

**What**:  
The internal request model stores query parameters as an ordered slice of key/value pairs.
A key may appear any number of times and insertion order is preserved.
`map[string]string` was rejected outright, while `map[string][]string` (the shape of `url.Values`) was considered and passed over.

**Why**:  
A query string is an ordered multimap, and PostgREST's dialect gives repeated keys meaning: repeated filter keys AND together (`age=gte.18&age=lte.65` is exactly how `Gte("age", 18).Lte("age", 65)` serializes) and repeated `or=` groups combine.
`map[string]string` is the one shape that cannot represent valid PostgREST queries - a second filter on a column would silently overwrite the first.

The sibling SDKs store the same multimap shape, for example [`postgrest-js`](https://github.com/supabase/supabase-js/tree/master/packages/core/postgrest-js) appending to `URLSearchParams`.
Between the two faithful shapes, the slice of pairs was preferred over `map[string][]string` because it keeps the model free of reference-typed fields: a plain struct copy is safe (pairs are immutable values; writes append after `slices.Clone`), whereas a map field aliases on copy, `maps.Clone` is shallow over the value slices, and one forgotten deep clone in a future `With*` method is a data race - the exact hazard the model exists to remove (supabase-py needed a third-party persistent-collections library to make the map shape safe; the slice gets the same guarantee from the stdlib).

Singleton keys take replace-semantics through `WithParameterReplacing`, which `Limit` uses so that a repeated call replaces the earlier value - the last-write-wins behavior the sibling SDKs implement, and the safe choice given PostgREST documents no behavior for a repeated limit key.

Comma-separated list keys take join-semantics through `WithParameterJoining`, which `Order` uses so that repeated calls grow one `order` pair instead of repeating the key: PostgREST reads only the first `order` parameter for a query level and silently ignores the rest, so a repeated key would drop every term after the first call's.
The ordering refinements extend the newest term through `WithParameterValueAppended`, whose plain string append is sound because the term grammar forbids commas inside a term, so the flat value's tail is always the newest term.

## The query string is rendered by an in-model RFC 3986 writer, not url.Values.Encode

**What**:  
`HTTPRequest` renders parameters through `rawQuery` - pairs in insertion order, each key and value escaped by `escapeQueryComponent` - which percent-encodes only what the pair grammar reads as structure (`&`, `=`, `+`, `%`, `#`), what RFC 3986's query production forbids (spaces, double quotes, controls, non-ASCII octets) and the historical pair separator `;`.
The remaining query characters - the commas, parentheses, dots, colons and asterisks PostgREST's dialect leans on - pass through literally, and a space renders `%20`, never `+`.

**Why**:  
`url.Values.Encode` is form-encoding: it escapes every byte outside the unreserved set and sorts pairs by key, so PostgREST queries render as `select=id%2Cname` noise in logs and tests, in an order no caller wrote, and measurably longer in a comma-dense dialect (three bytes per comma across select lists, in-lists and multi-column order values).
RFC 3986 permits the sub-delimiters literally in a query, PostgREST URL-decodes before parsing - both spellings are identical to the server, as the sibling SDKs demonstrate by shipping both - and PostgREST's documentation writes the literal form throughout, so readability, size and order fidelity are the only stakes.
Cross-key order is semantically inert - PostgREST reads each parameter out by name - so insertion-order rendering is for the human reading the wire, not the server.
`+` and `;` are escaped despite being sub-delimiters because form-decoders - PostgREST's own query parsing included - read `+` as a space, and Go's `url.ParseQuery` rejects `;` outright.
A space renders `%20` rather than form-encoding's `+` because `%20` is the uniform unsafe-octet rule's own output and the only spelling that decodes to a space under both RFC 3986 percent-decoding and form-decoding, where `+` would need a special-case branch borrowed from the form-encoding family ([supabase-swift pins the same spelling](https://github.com/supabase/supabase-swift/blob/ebef170a4a6820d064e5909dd4f54e4341f12eb5/Tests/PostgRESTTests/PostgrestTransformBuilderTests.swift#L34)).
`RawQuery` is the documented home for pre-encoded query text and round-trips byte-for-byte through `url.Parse`, so the writer composes with `http.NewRequestWithContext` without re-encoding.
The escaper is octet-oriented rather than rune-oriented because percent-encoding is defined on octets (RFC 3986 section 2.5's UTF-8-then-escape rule), so any string renders losslessly - arbitrary non-UTF-8 bytes included - where a rune-based walk would silently corrupt invalid sequences to U+FFFD.

## Builder phases are distinct concrete types (typestate); embedding only narrows, interfaces only at the terminals

**What**:  
`From` returns a concrete `QueryBuilder`; `Select` returns a concrete `FilterBuilder`; every builder method returns a concrete type, never an interface.
`QueryBuilder` and the ordering refinement wrappers (`OrderedFilterBuilder`, `OrderedDescendingFilterBuilder`) embed `FilterBuilder` to extend its method set, satisfying `Query` through promotion.
The read functions accept the sealed `Query` interface, whose sealed accessors return a `queryState` token binding the row type; builder states satisfy it and nothing outside the package can.
These wrapper structs have identical definitions on purpose: a builder type's identity is its method set - which chain steps are legal from here - not its field set.

**Why**:  
The types encode the phase of the chain, so illegal chains are compile errors: `Select` twice is unrepresentable, because `Select` consumes the `QueryBuilder` and `FilterBuilder` has no `Select`.
The sibling SDKs accept the double call and resolve it silently, last write wins (postgrest-js `searchParams.set('select', ...)`).

Interface-typed returns would hide the fluent surface from godoc and autocomplete without buying substitutability we need, so chain methods return concrete types and the read functions instead accept the sealed `Query` interface - interfaces in, concrete types out, the posture Google's Go style guidance names outright, with the interface living in the package that consumes query values.
A type parameter no method signature mentions is inert - `Query[Instrument]` and `Query[Section]` would define identical type sets and so be the same type, letting a wrong-row `Collect[Section](instrumentsQuery)` compile while `Row` inference fails at every call site - so the row type is bound where the read path already has a genuine method, `state`'s return type, keeping the public interface free of never-called members (a dedicated phantom anchor method was rejected for exactly that deadness).
Inference through interface method signatures is defined behavior since Go 1.21, below the module floor.
The mockability seam remains the injected HTTP client, not the builders.

Embedding appears exactly where promotion's behavior is the wanted semantics: a promoted method returns the embedded `FilterBuilder` or one of its ordering wrappers, ending the window its own type held open - `QueryBuilder`'s projection window, the ordering wrappers' refinement window - and after `Select` there is no phase method left for promotion to leak.
Embedding that would surface an earlier phase's methods on a later phase (`Select` on `FilterBuilder`) remains rejected, since it would destroy the typestate guarantee.

## A bare `From` is a complete read and `Select` is optional projection

**What**:  
`QueryBuilder` embeds `FilterBuilder`, so `From[Row]("table")` alone satisfies `Query` through promotion and every `FilterBuilder` method chains directly off `From`.
A bare `From` sends no `select` parameter at all, while `Select` narrows the projection and remains callable at most once, only as the first step of a chain.

**Why**:  
PostgREST does not require `select` on a read: its reference marks the parameter optional with "The default is `*`, meaning all columns" ([Vertical Filtering](https://docs.postgrest.org/en/latest/references/api/tables_views.html#vertical-filtering)) and its horizontal-filtering examples carry none, so a mandatory `Select("")` for the all-columns case would be SDK ceremony the wire never asks for.
The one deep reason the reference SDK makes `select()` central does not translate to Go: postgrest-js infers the TypeScript result type from the select string (`GetResult`), where this SDK names the decode type at `From[Row]` before `Select` is ever reachable.
The absent parameter relies on the server's documented `*` default rather than restating it, and `Select("")` keeps its documented `select=*` meaning because argument values, unlike chain steps, cannot be policed by the type system.
Opening the select-less boundary is sound exactly when the HTTP method is already fixed, as it is at this SDK's `From` (supabase-swift bakes `.get` in at `from()` and serves the select-less GET, where postgrest-dart leaves the method null and pays with a runtime `ArgumentError`).

## Builder state serializes immediately into the request model

**What**:  
Every builder method serializes its effect into the internal request model at call time - `Select` writes the `select` parameter immediately.
Builders hold no structured intermediate state (no columns, filters or limit fields), and the read functions' shared `execute` path performs no assembly beyond handing the model a base URL.

**Why**:  
The wire format is the canonical state in the sibling SDKs (postgrest-js mutates `URLSearchParams` inside each method), so behavior parity with the reference implementation is auditable call by call: our `Select` does what theirs does, at the same moment.
A structured representation assembled at execution time would be a second source of truth whose serializer must track the reference forever, for no validation gain: ordering rules ("X not before/after Y") are enforced earlier and stronger by the typestate split, and value-level conflict rules, when a concrete one arrives, can read the model through a narrow predicate (a `HasParameter`-style query added then) - normalization loses no state a known rule needs.
The only information call-time serialization erases is which method wrote a pair; no PostgREST rule branches on that provenance, and the siblings validate almost nothing themselves, delegating conflicts to PostgREST's own errors, which this SDK surfaces as `*Error`.

## Ordering is refined by postfix typestate builders

**What**:  
`FilterBuilder.Order(column)` writes the bare column as a new order term and returns `OrderedFilterBuilder`, which embeds `FilterBuilder` and adds `Descending` and `NullsFirst`. `Descending` returns `OrderedDescendingFilterBuilder`, which adds only `NullsLast`, and the null-placement methods return the plain `FilterBuilder`. Each state offers only departures from what the term already implies, direction strictly before nulls, and every refinement appends its token to the newest order term. A refinement restating a server default (`NullsLast` while ascending, `NullsFirst` once descending) has no method, so each direction and null-placement combination has exactly one incantation. `Order` itself takes no direction or null-placement parameter.

**Why**:  
An order term's programmable space is two independent binary axes - direction and null placement - and encoding the remaining choices in the returned type makes every meaningless sequence a compile error instead of a runtime ruling: a duplicated `Descending`, contradictory null placements and nulls-before-direction do not build, and a default-restating spelling does not exist, so no combination has two spellings. Direction-before-nulls keeps every refinement a plain string append onto already-serialized state, where a commutative surface would need the request model to parse and reorder the term it wrote. The alternatives each answered the two axes worse: a four-value enum (`Order("name", Descending)`) was the runner-up with zero new types and untouched terminals, but it spells the ordering as an argument rather than chain steps and its un-suffixed names silently carry SQL's coupled null defaults where the postfix methods surface them as explicit, documented steps; bare boolean pairs are unreadable at call sites; a two-boolean struct's nulls zero value silently diverges from SQL's descending default; variadic tokens re-admit the contradictions the typestate forbids. An unrefined axis relies on the server's documented defaults.

## The `Range` modifier is inclusive at both ends and compiles onto PostgREST's `limit` and `offset`

**What**:  
`FilterBuilder.Range(from, to)` narrows the result to the rows at zero-based positions `from` through `to` inclusive, serialized immediately as `offset=from` and `limit=to-from+1` through the request model's replace semantics on both keys. A later Range replaces both pairs, Range and Limit replace each other's row cap in either order and a start offset outlives a later Limit. Bounds are computed verbatim with no validation: `Range(2, 1)` sends `limit=0` and requests zero rows, anything smaller sends a negative cap for the server to reject, and a negative `from` is forwarded untouched. There is no half-open spelling, no standalone Offset method and no referenced-table parameter.

**Why**:  
The zero-based inclusive contract is family-wide (the sibling SDKs compute `offset=from` and `limit=to-from+1` with set-semantics on both keys, and the Supabase documentation teaches `range(0, 9)` returns ten rows), so a half-open Go spelling in the slice tradition would silently return one fewer row to anyone porting a documented example - an invisible off-by-one this SDK refuses to create. PostgREST's Range-header mechanism carries the same information but no sibling uses it, it cannot address embedded resources and it would open a second serialization surface beside the query-string model. The Limit interplay is not bespoke code: both methods write the singleton `limit` key through `WithParameterReplacing`, so last-cap-wins follows from replace semantics - exactly the observable contract the sibling SDKs pin in their tests. A typestate exclusion of a second cap writer was considered and passed over: caps have no grammar to enforce (unlike direction-before-nulls), and a capped state would have to re-expose the whole filter surface for one unrepresentable-repeat guarantee the sibling family spells as last-write-wins. A standalone Offset method exists only in supabase-py, has no capability id in the canonical matrix and adds nothing Range does not express. The referenced-table variant is deferred to arrive with relationship embedding, so Order, Limit and Range get one uniformly spelled referenced-table story rather than three ad hoc ones.

## No `Or`/`And` methods - `RawLiteralCondition` is the escape hatch for logical operators

**What**:  
`FilterBuilder` offers no `Or`, `And` or grouping API. `RawLiteralCondition(key, value)` is the only route to PostgREST's logical operators: it sends one verbatim query-string pair, where `key` is a column or a logical operator (`or`, `and`, optionally `not`-prefixed) and `value` passes through with no rendering, quoting or validation.

**Why**:  
PostgREST's logical grammar is recursive - nested groups, negation at any depth and embedded-resource prefixes - so faithful `Or`/`And` methods mean designing a whole expression-tree API for advanced usage most queries never need. One verbatim pass-through covers the entire grammar at the accepted cost that the caller owns the syntax and a malformed condition invalidates the whole query. The friction is deliberate: the long method name and the wire-level `key`/`value` parameters (PostgREST has no term unifying column and logical-operator keys) signal an unguarded surface, where a `column` parameter would falsely advertise the safety of the dedicated methods.

## Reads execute in package-level generic functions, context-first

**What**:  
The builder chain is generic from its root - `From[Row]("table")` names the row type once, threads it through `QueryBuilder[Row]` and `FilterBuilder[Row]`, performing no I/O.
Execution happens only in package-level generic functions - `Collect(ctx, client, query)` returning `([]Row, Response, error)`, with `Row` inferred from the query - which share one unexported `execute` path.
`Response` carries `HTTPStatus` and `Count` as exported scalar fields on a by-value record, where `Count` is `-1` when the server reported no total, following `net/http.Response.ContentLength`'s convention.

**Why**:  
Naming the row type at `From[Row]` lets every read function infer it like [`slices.Collect`](https://pkg.go.dev/slices#Collect), keeps package-level query variables typed so reuse sites cannot diverge and gives future write verbs compile-checked payloads (`Insert(rows ...Row)`), whereas explicit instantiation (pgx's [`CollectRows[T]`](https://pkg.go.dev/github.com/jackc/pgx/v5#CollectRows), sqlc's per-query structs) repeats an unchecked bracket at every read site.
Typing is per-query, never per-table: `select` is a projection language, so the row shape belongs to the query (a second shape is another `From[U]`), and a per-table registry would centralize a binding Go can never check against the selected columns.
The accepted costs - `Row` is a phantom threading through builders whose state never depends on it, and a finished query cannot fork into differently-typed decodes - stay shallow: an in-package `Retype[U](query)` is purely additive ([partial type argument lists](https://go.dev/ref/spec#Instantiations)) and `From[json.RawMessage]` covers raw rows.
Execution is a package-level function - generic methods need go1.27, above the module floor - context-first per the standard's context mandate, following `slices.Collect` and [`iter.Pull`](https://pkg.go.dev/iter#Pull) as free generic functions over values, and array-ness as the return contract makes destination-pointer questions (nil-ness, preallocation) unrepresentable.
`Response` stays a plain exported-field record because it is returned by value and holds only scalars, so consumers hold independent copies and no aliasing exists to defend against, while unexported fields would stop consumers fabricating a `Response` in their own test doubles.
This argument is scalar-dependent: a reference-typed field (headers, raw body) must not be added to `Response` without revisiting it.

## `postgrest.Error` carries the parsed PostgREST body plus HTTP status

**What**:  
Non-2xx PostgREST responses become `*postgrest.Error` with exported `HTTPStatus`, `Code`, `Message`, `Details`, `Hint` fields, matched via `errors.As`, plus an `Unwrap` returning a usually-nil underlying cause.
Unparsable error bodies are preserved raw in `Message`.
Transport, request-building and decode failures are wrapped `fmt.Errorf("postgrest: ...: %w", err)` values, not `*Error`.

**Why**:  
The field set mirrors the reference SDK (postgrest-js `PostgrestError`), whose docs establish the read order (Hint carries the database's fix; Code is the stable branching key).
Distinguishing "the server answered with an error" (`*Error`) from "we never got an answer" (wrapped transport error) lets callers branch with one `errors.As`.
`Unwrap` exists despite the usually-nil cause because this is the SDK's first public error type and its shape gets copied by every later module; retrofitting wrapping onto a shipped error type is harder than carrying a nil cause now.

## Integration harness: pinned-binary Supabase CLI, minimal services, floor + stable matrix

**What**:  
CI's integration job and `scripts/integration-test.sh` run the same script, which starts a local stack using the Supabase CLI, a committed minimal `config.toml` (only db, api and auth enabled), a committed schema migration and a committed data-only `seed.sql`.
The CLI is the pinned release binary, verified against a committed SHA-256 and installed into Go's own bin directory (GOBIN, else GOPATH/bin), never taken from npm.
Integration tests are env-gated and run under `-race`.
They live in `integrationtest` modules beside the code they exercise, consuming only the public API, and selection is by the module boundary alone: the script runs `./...` in each with no `-run` name filter.
The CI job runs the same floor + stable matrix as build-and-test; a `GOWORK=off go vet` pass over the same modules in the unit script additionally keeps them compiling for fast local signal.

**Why**:  
The CLI cannot be installed with `go install` at v2 for two independent reasons: its module (`github.com/supabase/cli`) now lives in `apps/cli-go/` while the repo root carries no `go.mod`, so the module proxy resolves that path only to the stale v1 root-module history rather than the v2 code, and its `go.mod` carries local `replace` directives, which `go install pkg@version` refuses outright.
It is fetched instead as the pinned release binary, verified against a committed SHA-256 and installed into Go's own bin directory (GOBIN, else GOPATH/bin) - a writable, on-PATH location outside the checkout, so a read-only working tree is fine - the same first-party curl-and-checksum pattern as the local Go toolchain install.
npm was rejected as the channel even though it pins equally well, because bundling the CLI into `tools/node` conflated it with the unrelated cspell tool - every `npm ci` pulling both - and forced a node_modules write into the checkout, whereas cspell stays on npm as a genuine JS tool whose deep dependency tree is what a lockfile exists for.
The auth service stays enabled despite no test calling it, because `supabase status -o env` emits the stack's API keys (PUBLISHABLE_KEY included) only while auth is enabled - the harness reads its credentials from that output, consuming PUBLISHABLE_KEY exactly as the CLI repository's own e2e harness does (ANON_KEY is deprecated upstream).
Schema lives in `migrations/` and only data in `seed.sql` because the CLI applies the seed as a single batch whose statements are prepared before earlier ones execute, so DDL cannot ride with inserts that depend on it (SQLSTATE 42P01 on a fresh stack) - the same layout as the CLI repository's own e2e project.
The script runs the CLI against a disposable `mktemp -d` copy of `integration-testing/` because the CLI writes scratch state (`supabase/.branches`, `supabase/.temp`) into whatever project directory it runs: the copy keeps committed trees pristine by construction and lets the harness run from a read-only checkout, while `stop` still finds the stack because the CLI identifies it by `config.toml`'s `project_id`, not by path.
Every other unused service is disabled because this harness's startup time and flakiness set the floor for all future CI.
A name-anchored `-run` filter (`^TestIntegration`) would spare the unit re-run, but its failure mode is silence: a tagged test named outside the anchor compiles cleanly, never runs and lets the suite pass vacuously, whereas the re-run it prevents is hermetic and costs seconds.
The dedicated test-only package makes the consumer stance structural - every test package is external, so unexported access never exists to lose - and keeps the integration namespace decoupled from the unit test files, so suite selection never depends on function names and names never collide across suites.
The floor leg exists because only a live-stack run exercises the consumer floor end to end on the floor toolchain, and the legs run in parallel so wall-clock cost is unchanged.

## `X-Client-Info` resolution is verified by an out-of-tree consumer program

**What**:  
The `telemetrytest/` module is a stand-in consumer: it requires the SDK modules at fabricated, self-labeled versions (`v1.999.1-fabricated` supabase, `v1.999.2-fabricated` postgrest), `replace`s them to the local working tree and its main program asserts the exact `X-Client-Info` value each entry point sends to a local HTTP server.
`scripts/telemetry-test.sh` runs it with `GOWORK=off` and the module is not listed in `go.work`.
A second leg rebuilds the same program in GOPATH mode (`GO111MODULE=off`), where binaries carry build information without module records, and asserts the version-unknowable `0.0.0` fallback in every header.
The `TELEMETRY_TEST_MODE` environment variable tells the program which expectations to hold.
The check is part of the fast tier (`check-fast.sh`) and runs in CI as a step of the build-and-test job, on its floor + stable matrix.
The probe is a plain program, not a `go test` suite.

**Why**:  
Every binary the in-repo suites produce has one of this repository's modules as its main module, so header resolution takes the in-tree branch and reports `(devel)`.
The branch every published-module consumer exercises - reading client versions from build-information dependency records - is reachable only from a main module outside the SDK's module tree.
It must be a plain program because `go build` and `go run` stamp dependency records into binaries while `go test` binaries record the main module and no dependencies (observed on go1.26), which rules out expressing the probe as a test suite.
Workspace membership would defeat the vantage from the other side - a workspace build supplies the SDK modules as local source with no resolvable versions - so the module stays out of `go.work` and the script forces `GOWORK=off`.
The fabricated versions are distinct from every sentinel the header can otherwise carry (`(devel)` in-tree, `0.0.0` without build information), so a pass is unambiguous provenance, and their `-fabricated` prerelease label keeps the header values in check output from reading as release claims.
Each must outrank every other require of the same module path in this build so minimal version selection keeps it as the selected, recorded version: `1.999.x` outranks every real `v1` version, pre-releases included, and a `v2` would live at a new module path.
The floor leg exists because the header is consumer-facing behavior, so it must hold at the consumer floor.
The GOPATH leg exists because module-record-free binaries are otherwise not exercised at all - every matrix toolchain is now 1.24 or later, so even test binaries carry module records - while GOPATH mode produces them deterministically on every toolchain.
The expected versions come from the environment rather than from the binary's own build information, which would assert whatever branch actually ran and pass even when a leg lands in the wrong branch.

## Module information is judged by `Main.Path`, not by `ReadBuildInfo`'s ok

**What**:  
`buildClientInformationHeaderValues` treats the running binary as carrying module information only when `debug.ReadBuildInfo()` succeeds and `Main.Path` is non-empty.
Otherwise every registered client synthesizes `<name>/0.0.0`, the same version-unknowable sentinel used when build information is absent entirely.
The construction panic remains for a module-aware binary built outside the SDK's tree whose dependency records omit the named entry module, and for a module that is not a registered telemetry client.

**Why**:  
Since Go 1.18 every binary the go command produces embeds build information, so ok answers "is there a blob" and not "is module identity known": binaries built with `GO111MODULE=off` (and test binaries from toolchains before Go 1.24, [golang/go#33976](https://github.com/golang/go/issues/33976), now all below the consumer floor) report ok with a zero-valued `Main` and nil `Deps`.
Trusting ok alone would therefore panic every client construction in a GOPATH-mode build, including this repository's GOPATH telemetry leg.
A binary in that state carries no module identity at all, so nothing distinguishes an in-tree build from a consumer's, no `(devel)` claim is honest and the version-unknowable sentinel is the only truthful value.
The panic survives only where module identity is present and contradicts the caller's declared entry module, which is a programmer error rather than an environment degradation.

## The consumer floor is a policy: the oldest Go major the Go project still supports

**What**:  
Every floor-carrying artefact - the published `go.mod` directives, [`go.work`](go.work), the consumer-shaped modules outside the workspace and [CI](.github/workflows/ci.yml)'s floor matrix legs - carries the oldest Go major release still supported by the Go project, raised in lockstep by [`scripts/raise-consumer-floor.sh`](scripts/raise-consumer-floor.sh).
This floor should be raised opportunistically after each Go release rather than on release day.
The policy is stated consumer-facing in [our root `README.md`](README.md) ("Supported Go versions").

**Why**:  
The standard library is statically linked into every consumer binary and only the two newest majors receive security fixes, so a floor inside Go's support window never claims compatibility with toolchains whose binaries cannot be patched.
A lower floor buys no reach: every Go line below the floor is end of life, so no supported-toolchain consumer distinguishes the floor from anything lower.

## `request_timeout` is satisfied by `http.Client.Timeout` through `WithHTTPClient`, not a dedicated option

**What**:  
The `database.configuration.request_timeout` capability is claimed in [`sdk-compliance.yaml`](sdk-compliance.yaml) with `configuration.WithHTTPClient` as its symbol and no new API: consumers set a construction-time deadline by passing an `http.Client` whose `Timeout` field is set.
The wrap in [`transport.WrapClient`](core/internal/transport/transport.go) clones the caller's client, so the field survives construction, and the deadline spans exactly the SDK's I/O - connection, headers and the response-body read - cancelling in-flight requests when it elapses.
A per-request context deadline composes with it the Go-native way: whichever fires first cancels, and both surface as errors matching `context.DeadlineExceeded`.

**Why**:  
Go's standard `http.Client` already expresses the canonical semantic natively ("Timeout specifies a time limit for requests made by this Client", cancelling as if the request's context ended), so a dedicated `WithTimeout` option would be a second spelling of a field the injected client already carries - configuration surface without new capability.
The sibling precedent is Python's, which claims the capability through the language-native mechanism plus an explanatory note.
JS and Flutter needed explicit options only because `fetch` and Dart's `http` lack a native construction-time whole-request client timeout.

## Mutations are verb methods with compile-checked payloads, sharing one terminal `MutationBuilder` and its `Returning` projection

**What**:  
`Insert(rows ...T)` is a method on `QueryBuilder[T]`, always sent as one JSON array - a single row, many rows or, for no arguments, the empty array `[]`, never `null`.
No client-side column union is computed for a bulk insert: each row sends exactly the keys its own marshalling produces.
Every write verb returns the same terminal `MutationBuilder[T]`, whose one shaping method, `Returning(columns)`, writes the `select` parameter with replace semantics where an empty string means every column.

**Why**:  
`From[Row]("table")` is the one place the table and row type are named, so writes belong on the builders it produces.
Sending a variadic as one array gives the single-row and bulk cases one wire shape, where an empty array is the server's to rule on rather than a no-op the SDK invents.
A client-side column union is refused because it would send keys a row never named, silently overriding the table's column defaults - what is not said is not sent, so a ragged batch is the server's `PGRST102` to raise.
One terminal type serves every verb because every write's end state is identical - a fully-specified mutation whose only remaining choice is what a representation-returning execution reports.
`Returning` is named to align with PostgreSQL's own `RETURNING`, deliberately not a `Select`: on a write "select" would misname the act, and whether any representation returns at all is an execution-layer choice, not builder state.

## Whether a mutation returns its rows is chosen at execution

**What**:  
A `MutationBuilder` carries no return-preference state.
`Execute(ctx, client, mutation)` applies the write and decodes nothing, so the request takes PostgREST's default minimal return and no rows travel back.
Passing the same builder to a read function - `Collect`, `CollectSingle` or `CollectSingleMaybe` - instead adds `Prefer: return=representation` at execution and decodes the affected rows.
`Execute` accepts the sealed `Mutation[Row]` interface, which a read query does not satisfy, so passing a read to `Execute` is a compile error.

**Why**:  
A write request is identical whether or not its rows come back - only the `Prefer` header differs - so the choice belongs beside the choice of decoding (executing functions), not in the builder's state.
Minimal by default matches the PostgREST server default, and never transfers rows a caller does not read - the representation is opt-in through choosing `Collect` over `Execute`.
This is also why the row-level-security interaction is a pure function of the executing function: an insert whose role may not `SELECT` succeeds through `Execute` yet fails through `Collect` with PostgreSQL's `42501`, because only that representation path emits the `RETURNING` that needs the `SELECT` right.

## Update changes are an opaque `any` payload marshaled to one JSON object

**What**:  
`Update(changes any)` takes the column assignments as an opaque value, marshaled with `encoding/json` to one JSON object and sent as the `PATCH` body.
A `map[string]any` is the recommended shape, where a key carrying `nil` clears its column to SQL `null`. An absent key leaves the column untouched, and a struct is accepted with the documented caution that every marshaled field is assigned, its zero value included.
There is no typed-changes type and no client-side pruning of which fields to send.

**Why**:  
An insert names whole rows, so its payload is the query's row type `T`, but an update assigns an arbitrary subset of columns that no single Go type expresses without a per-table partial-update wrapper or pervasive pointer fields.
Taking `any` and marshaling it straight to JSON matches how other Supabase SDKs update values and lets the caller pick the shape that fits: a `map[string]any` to send exactly the named columns with explicit nulls, or a tagged struct when a fixed shape is more convenient.
The map's null-versus-absent distinction is the one PostgREST acts on, so the SDK carries it faithfully rather than inventing a sentinel for "clear this column", and the struct caution is documented rather than hidden because Go's zero values are indistinguishable from unset without field tags.

## `Update` and `Delete` are terminal verbs on `FilterBuilder`, so writes reuse the read filter surface

**What**:  
`Update(changes)` and `Delete()` are methods on `FilterBuilder[T]`: the row-choosing filters chain first, exactly as they do on a read, and the verb ends the chain by returning the terminal `MutationBuilder`, which offers no filter methods.
Reachable through promotion, a verb called directly on `From`'s builder - no filters - addresses every row of the table.
`Insert` stays on `QueryBuilder` alone, so a filtered chain reaching `Insert` is impossible to formulate (it does not compile).
`Order`, `Limit`, `Range` and `Select` remain reachable before the verb, traveling for the server to rule on.

**Why**:  
A row-choosing write scopes its rows with the same filters a read scopes its result, and placing the verb after the filters lets the one `FilterBuilder` surface serve both sides - one representation of every operator, with every future filter extending reads and writes at once, and a stored filtered scope reusable as a read through `Collect` and as a write through the verb.
Matching sibling SDKs' verb-first ordering (`update(...).eq(...)`) would require a mutation-typed duplicate of the entire filter surface, because the sealed `Mutation` typestate must survive the filter chain and a chained Go method cannot return its receiver's concrete type generically: a mirror of every filter method plus a compliance registration per method, a cost out of all proportion to the ordering familiarity it buys.
The read-shaped modifiers are deliberately not fenced off the write path: PostgREST 13 dropped limited update/delete ("The feature was complicated and largely unused", [PostgREST changelog](https://github.com/PostgREST/postgrest/blob/main/CHANGELOG.md)), so `order` or `limit` riding a mutation is the server's to rule on - the same posture this SDK takes for a negative limit or an empty list - and a `Select` written before the verb genuinely projects the returned representation, exactly as `Returning` does, with `Returning` replacing any projection `Select` wrote.

## Upsert refines a POST through postfix typestate, merging duplicates by default

**What**:  
`Upsert(rows ...T)` is a method on `QueryBuilder[T]` that sends its rows exactly as `Insert` does and carries `Prefer: resolution=merge-duplicates` from the outset, returning an `UpsertBuilder[T]`.
The builder refines the conflict handling: `OnConflict(column, additional...)` writes the `on_conflict` parameter with replace semantics, `IgnoreDuplicates` swaps the resolution to `ignore-duplicates` and `Returning` projects the returned representation, all self-typed so they compose in any order.
The resolution is carried on the request model as a single replace-on-write token (`WithPreference`), not a general header store, and the execution layer renders every `Prefer` header from it.

**Why**:  
`Prefer: resolution` is what turns a `POST` into an upsert - PostgREST has no server-side default resolution - so a resolution always travels, defaulting to merge because that is what "upsert" means to a caller, matching the JS SDK's `onConflict`/`ignoreDuplicates` default.
Variadic rows leave no room for a trailing options argument, so the refinements are postfix chain methods (the shape this package already uses for ordering), and the first-plus-rest `OnConflict` signature makes an empty conflict target unrepresentable rather than a runtime error.
Resolution is the only preference any builder writes, so the model carries one replace-on-write token rather than a header multimap, and `IgnoreDuplicates` replaces the merge. The execution layer is the single place `Prefer` is set - it appends the carried resolution and, for a representation-returning execution, `return=representation`, as separate field-lines the server reads as one list per RFC 7240.

## RPC is mode-first: the caller declares the result shape, and read-only is an opt-in transport

**What**:  
`RPC[T](function)` is inert until a result shape is chosen - `Rows` for a set decoded per row, `Value` for one JSON value decoded whole - while `RPCVoid(function)` is the shapeless entry for a function returning nothing.
`Arguments` chains the inputs, and omitting it runs the function on its defaults.
Every call is a POST by default, and `ReadOnly` refines a `Rows` or `Value` call into a GET.
No RPC type carries filters, ordering or write verbs.

**Why**:  
The sibling SDKs return one undifferentiated builder from `rpc` and leave the caller to decode whatever comes back. Declaring the shape up front makes the returned type offer only the operations that shape supports, so decoding a scalar as rows or reading a void call back is unrepresentable rather than a runtime error.
POST is the bare default because it calls every function, so such a call makes no read-only claim to get wrong, where a read-only default would fail against every volatile function. `ReadOnly` is thus the opt-in claim, matching PostgREST's rule that GET is the conditional privilege earned by not writing.
`RPCVoid` is separate and non-generic because a function returning nothing has no result type or shape to name, so a forced type argument and mode call would carry no information.
No type embeds the filter builder because the rpc endpoint has no filter or write surface, so embedding it would compile calls the server always rejects.

## Whole-body decoding, row decoding and execution are three separate sealed interfaces

**What**:  
Three sealed interfaces gate the executing functions: `RawQuery` for `CollectRaw`'s whole-body decode, `Query` (embedding `RawQuery`) for the `Collect` family's per-row decode and `Mutation` for `Execute`.
`Mutation` embeds neither, carrying its own executable marker.
The table write builders satisfy all three, a read builder satisfies `Query` and a void function call satisfies `Mutation` alone.

**Why**:  
In an early API design for this SDK `Mutation` embedded `Query`, encoding the claim that anything executable also decodes as rows, which every table write honored by returning its affected rows.
A void function call is executable with nothing to decode, so the embed is dropped and the two promises become independent - `Collect` over a void call is now a compile error rather than a runtime decode of an empty body.
Go's structural typing forces the three accessors to carry distinct names, since one shared name would let a type satisfy an interface it should not.
`CollectRaw` takes the weakest interface so one path reads a scalar function, a whole table array or any other whole body.

## Row Level Security integration fixtures sign up real users through the local auth service

**What**:  
The tests obtain user tokens from `POST /auth/v1/signup` on the already-enabled local auth service, which auto-confirms and returns a session under the pinned CLI's defaults, rather than minting JWTs from the stack's `JWT_SECRET`.

**Why**:  
`JWT_SECRET` is deprecated in the CLI's status output and the platform is moving to asymmetric signing keys, so tokens minted locally from it would exercise a shrinking path.
Issued tokens travel the same path a production consumer's do.

## Example programs are non-published modules run by the integration tier

**What**:  
`examples/` holds standalone main-package modules outside `go.work` with replace directives to the local tree. The fast tier compiles them (vet), lint covers them and `scripts/integration-test.sh` runs each against the local stack.

**Why**:  
`Example` test functions cannot demonstrate a consumer-shaped module graph - a dependency quarantine or a scoped import is a property of a separate `go.mod`'s requires - nor run against a live stack, and a separate non-published module keeps example-only dependencies (OpenTelemetry, for `tracing-otel`) out of every consumer's graph.

## Trace propagation is the context path and the transport seam, not SDK machinery

**What**:  
`client.observability.trace_propagation` is claimed on what already exists: every call's context reaches its wire request and `WithHTTPClient` injects a caller-instrumented transport (`otelhttp.NewTransport`) that writes the W3C headers. No trace option, extractor, host allow-list or OpenTelemetry dependency is added.

**Why**:  
The sibling SDKs built opt-in trace machinery because their runtimes hold ambient global trace context and their HTTP layers lack a per-request context, so they must extract, filter by host and inject themselves; Go's context is explicit and the injected transport serves only this SDK's requests to the project URL, so the caller's transport choice already scopes propagation.
Vendor neutrality is a fixed constraint: no OpenTelemetry type may enter the public surface, and a forced OTel version would create diamond-dependency conflicts for teams already running it.

## The logging seam takes *slog.Logger and emits at debug only

**What**:  
`configuration.WithLogger` accepts a `*slog.Logger` rather than a `slog.Handler`, and no emission rises above `slog.LevelDebug`.

**Why**:  
The logger is the unit applications already hold, and a caller with only a handler recovers the other shape with one `slog.New` call.
Failures already reach callers as returned errors, so a louder emission would report the same failure twice.

## Placeholder credentials in rendered examples name the real key type

**What**:  
Example code that renders on pkg.go.dev constructs clients with `https://PROJECT_ID.supabase.co` and the key literal `sb_publishable_...`, passes end-user tokens as `END_USER_ACCESS_TOKEN` and never embeds a JWT-shaped or entropy-bearing literal.
Runnable example programs carry no credential literals, reading `SUPABASE_URL` and `SUPABASE_PUBLISHABLE_KEY` from the environment.

**Why**:  
`sb_publishable_...` mirrors the prefix-plus-ellipsis form the official API keys guide prints, so a reader pastes the right one of the platform's four key types, which a generic `API_KEY` or the `your-publishable-key` style in the JS and Swift READMEs leaves ambiguous, while the truncated body carries no entropy to read as a leaked credential.
The legacy `anon` and `service_role` vocabulary is deprecated by the platform and appears on no consumer-facing surface.
SCREAMING placeholders cannot pass for live values, where the docs-site style `your-project.supabase.co` reads as a plausible real subdomain.
This deliberately diverges from the generic `YOUR_API_KEY` placeholder in the Supabase Writing Style Guide, which names no key type.

## Constructor key parameters stay apiKey while example variables name the key type they hold

**What**:  
Public constructors and the plumbing behind them name the credential parameter `apiKey`, with constructor doc comments stating the accepted key types.
Example code that fetches a known key type names its variable for it, as in `publishableKey := os.Getenv("SUPABASE_PUBLISHABLE_KEY")`.

**Why**:  
The parameter accepts any Supabase project API key and a server-side SDK is routinely constructed with the secret key, so a narrowed name like `publishableApiKey` would misdirect privileged callers - among the sibling SDKs only client-side Flutter narrows the name, where secret keys are forbidden outright.
In an example the variable's content is certain, so naming the type it holds lets the flow read against the env var it mirrors.

## Auth requests are single-attempt, outside the automatic-retry option

**What**:  
The Auth client sends each request once and ignores the `configuration.WithRetry` setting.

**Why**:  
Auth's requests are GETs and safe to repeat, so honoring the option was possible.
Regardless, the decision was made not to honor that option because verification sits on the request-handling hot path of the caller's own server (in our anticipated, likely use-case for this SDK), where invisible backoff multiplies the latency of the inbound request being served.
