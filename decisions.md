# Development Decisions for `supabase-go`

<!-- cSpell:ignore Cheney claude Cname iter mktemp openai pgrst pgx Seq sqlc vnd WHATWG -->

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

## No `Makefile` or task runner

**What**:  
CI and local dev use plain `go` commands only.

**Why**:  
A `Makefile` is a borrowed-from-C convention that earns its place only when a repo orchestrates non-Go work (docker, migrations, codegen, cross-compile, release packaging).
A pure multi-module library has none of that - every task is a single `go`-toolchain invocation.

## CI uses only first-party Actions (GitHub's `actions` org)

**What**:  
The only actions permitted are those owned by GitHub's first-party [`actions` org](https://github.com/actions/); no golangci/* or golang/* actions.

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
The `go` directive in published modules (`1.25`) is separate from, and unaffected by, the toolchain CI and tooling run on (latest stable).

**Why**:  
They are different concerns: the published `go` directive is a compatibility contract for the consumer's unknown environment (the policy floor recorded in the consumer-floor entry), while the CI/lint toolchain is our own deterministic environment (latest, our choice).
A latest toolchain compiles a go 1.25 module fine.
Tool-pinning machinery must never live in the published modules, or it would drag our environment's needs into the consumer's contract and force the floor up.

## Use a committed go.work workspace for intra-repo module resolution
  
**What**:  
The multi-module repository (`core`, `postgrest`, `supabase` and future domain modules) wires its internal cross-module dependencies through a single `go.work` file committed at the repository root, rather than through replace directives in each `go.mod` file.
Each module's `go.mod` file declares its sibling dependencies with ordinary require lines carrying the zero pseudo-version (`v0.0.0-00010101000000-000000000000`) until real tags exist.
The workspace's use directives supply the actual source for every in-repo build, locally and in CI.
The published `go` directive stays at the policy consumer floor (`1.25`) independently of the toolchain version CI runs.

**Why**:  
Pre-tag, a module that imports an unpublished sibling cannot resolve it without either `replace` directives or a workspace.
`go.work` is the purpose-built mechanism (Go 1.18+) and gives a cleaner separation of "what we publish to customers" (the `go.mod` files, free of dev-only redirects) from "how we develop locally" (one workspace file), stating the wiring once instead of repeating `replace … => ../core` in every consumer.
Committing it is the Go-team-endorsed practice for monorepos ([golang/go#53502](https://github.com/golang/go/issues/53502) explicitly declined a "never commit" warning; the relative paths are identical for every clone, gopls configures multi-module editing from it, and Dependabot understands it), and it is provably safe for consumers: `go.work` is never included in a published module zip and is ignored by `go get`, so it cannot affect anyone importing the SDK.
The one workspace hazard is the overlay masking a missing or wrong `require`: every in-repo build resolves siblings from workspace source, so a `require` defect surfaces only for consumers once tags exist.
[`scripts/check-module-paths.sh`](scripts/check-module-paths.sh) guards the path case: it fails when a workspace (published) module requires a first-party path that is not itself a workspace module, which a consumer could not resolve. A missing require or a wrong version still rests on review, since there is no tidy gate yet (zero external dependencies).
The decision is cheaply reversible (delete `go.work`, add `replace` blocks).

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
Sentinel immutability is covered by the separate "sentinel errors are compile-time constants" decision.

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
The Go tooling (linters, govulncheck) is pinned by checksum in a dedicated `tools/go/go.mod` + committed `tools/go/go.sum`.
The spell checker (cspell) is pinned the same way one ecosystem over: its full dependency tree is locked by integrity hash in a committed [`tools/node/package-lock.json`](tools/node/package-lock.json), installed via `npm ci`.
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
Google's SDKs are the cautionary contrast. Firebase's `app.Auth(ctx)` and `app.Firestore(ctx)` take a context and return an error because they lazily construct clients that resolve credentials and dial connections, and the context is then kept for the client's life: the `cloud.google.com/go` docs warn "Do not set a timeout on the context passed to NewClient: dialing happens asynchronously, and the context is used to refresh credentials in the background", and `golang.org/x/oauth2` states its client "is not valid beyond the lifetime of the context".
That shape only earns its place when the returned client owns background work bound to the context, and it carries a footgun when it does not: a request-scoped context passed to such a constructor and then cached breaks the client's background refresh once the request ends.
Our handles own no background work, so a context parameter would import that footgun for no gain.

## One HTTP customization seam, and a sealed HTTPClient across modules

**What**:  
The only way a caller customizes outbound HTTP is `WithHTTPClient`: they supply an `*http.Client` whose `Transport` is any `http.RoundTripper` chain they want, and `configuration` wraps its own auth `RoundTripper` (`apikey` injection) in front of it.
There is deliberately no `WithRoundTripper` or middleware option.
Internally, `configuration` hands each domain module a one-method `HTTPClient` interface (`Do(*http.Request) (*http.Response, error)`), never the concrete `*http.Client`.

**Why**:  
The single seam matches the dominant Go convention. Google's API libraries and Stripe expose only a whole-client seam, and Google's own docs tell callers to add behavior "via RoundTripper middleware" on their own client rather than through an SDK option. AWS SDK v2 is the exception, but its extra knob is a bespoke Smithy middleware stack, not an `http.RoundTripper` shortcut, so it is no precedent for one. A `WithRoundTripper` convenience can be added additively later if demand appears, so nothing is foreclosed.
Handing out the interface rather than the `*http.Client` stops the configured transport being swapped out through the accessor - a caller holding the concrete client could set `Transport = nil` and silently disable auth, or race on it - and it keeps the `configuration` public surface small, which is part of the `v1` promise.
The interface is named `HTTPClient` with a single `Do` method, following AWS SDK v2's interface of the same name and shape. `Do` is chosen because `*http.Client` already has that method, so the standard client satisfies the interface with no adapter, and the same one-method contract appears as the `HttpRequestDoer` that `oapi-codegen` generates in Supabase's own Auth code.

## No module is served from the repository root

**What**:  
The repository root carries no `go.mod`.
Every published module lives in a subdirectory named after its package - `core/`, `postgrest/` and `supabase/`, the convenience entry point.
Consumers import the root client as `github.com/supabase/supabase-go/supabase`, never `github.com/supabase/supabase-go` itself.

**Why**:  
pkg.go.dev renders the README it finds in a module's own directory, so a root-served module's documentation page carries the repository README - GitHub-audience content (status banner, module table, contribution pointers) that has no place in consumer API documentation.
With no root module, the repository README never reaches pkg.go.dev and each module's page stays scoped to what that module ships.
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

## The transport injects only the `apikey` header, never `Authorization`

**What**:  
HTTP requests have the `apikey` header but do not set `Authorization`.

An `Authorization: Bearer <jwt>` header is populated only by the application - per request or by an optional auth integration - when it acts for a signed-in end user.
It is never copied or otherwise derived from the project key.
A caller-supplied `Authorization` header should pass through untouched.

**Why**:  
Supabase's guidance is explicit - "Send publishable and secret keys on the `apikey` header only" ([Migrating to new API keys](https://supabase.com/docs/guides/getting-started/migrating-to-new-api-keys)) - the platform rejects the key on `Authorization: Bearer` unless its value exactly equals the `apikey` header ([Understanding API keys](https://supabase.com/docs/guides/getting-started/api-keys), known limitations).
Mirroring the key onto both headers, as other SDKs do by default, is therefore correct only by landing inside that narrow exception - a coincidence, not a design.
`apikey` alone produces the intended Postgres role with no precedence logic to reconcile (publishable is `anon`, publishable plus an end-user JWT on `Authorization` is `authenticated`, secret is `service_role`), and reserving `Authorization` for the end-user token draws the "what is calling" against "who is signed in" boundary cleanly at the transport, so a later per-request user token simply takes effect.

This has been proven by probing against a live project, where the model held exactly: `apikey` alone drew ordinary PostgREST responses, the key on `Authorization` alone was reported as no API key at all and a non-JWT bearer beside a valid `apikey` was forwarded and rejected by PostgREST (`PGRST301`).
The untested legacy-JWT path is a separately recorded accepted risk (see ["Legacy keys are not verified against the `apikey`-only transport, an accepted risk"](#legacy-keys-are-not-verified-against-the-apikey-only-transport-an-accepted-risk)).

## Legacy keys are not verified against the `apikey`-only transport, an accepted risk

**What**:  
This SDK is not tested against a legacy `anon` or `service_role` key.
The `apikey`-only transport (see ["The transport injects only the `apikey` header, never `Authorization`"](#the-transport-injects-only-the-apikey-header-never-authorization)) is validated only against the contemporary `sb_publishable_...` and `sb_secret_...` keys.
Whether a legacy `anon` JWT sent on the `apikey` header alone, with no `Authorization`, resolves to the `anon` role is left unverified and not promised.

**Why**:  
The reward is focus and speed: contemporary keys are current best practice and the only keys new projects receive [since 1 November 2025](https://supabase.com/changelog/29260-upcoming-changes-to-supabase-api-keys), so building and testing solely against them concentrates effort where it matters for every new consumer.

The cost is a bounded uncertainty rather than a known defect: the legacy path may well work, since a legacy key's role claim rode the `Authorization` header and PostgREST ["switches into the anonymous role"](https://docs.postgrest.org/en/stable/references/auth.html) when a request carries no JWT, but "may well work" is not the definitive clarity our tested paths carry and we state the gap openly rather than spend effort closing it.
The exposure also shrinks on its own, because the same timeline deletes legacy keys at the end of 2026.
Nothing is foreclosed: if a consumer need surfaces first, adding a legacy-key probe and verifying the path is a small, additive task.

## `SECURITY.md` and `CONTRIBUTING.md` are org-delegated, not repo-local

**What**:  
The repository carries no `SECURITY.md` or `CONTRIBUTING.md`.
Both are provided org-wide by `supabase/.github`, and a CI check asserts their absence here (covering the repo root, `.github/` and `docs/`).
`CODEOWNERS` stays repo-local.

**Why**:  
GitHub falls back to the organization's `supabase/.github` files for any repository that lacks its own, so an org-level `SECURITY.md` and `CONTRIBUTING.md` already apply.
A repo-local copy would silently shadow the org default and drift from it, so asserting absence beats maintaining a duplicate.
The one posture that does not belong at org level - that external code contributions are not accepted before the first GA release - lives in `DEVELOPMENT.md` instead.

## Merge strategy and commit conventions during incubation

Both decisions below deliberately diverge from apparent Supabase house defaults (squash-only merging, Conventional Commit PR titles).
Those defaults serve downstream release automation, which wants exactly one conventional commit per PR from which to infer changelogs and version bumps.
This repository is incubating - private, unreleased, no consumers, no release pipeline - so the constraint that motivates the defaults does not yet apply, and both decisions are revisited as a pair alongside the release-tooling choice ahead of the first release.

### PRs land as merge commits, not squashes

**What**:  
Repository merge settings enable only "Allow merge commits"; squash and rebase merging are disabled.
Every PR lands with its individual commits as ancestors of `main`, under a merge commit recording the PR boundary.

**Why**:  
A squash merge keeps the granular history only as GitHub platform metadata (the PR's Commits tab, backed by hidden `refs/pull/N/head` refs), not in the repository: a fresh clone sees one commit per PR, and `git log`, `git blame` and `git bisect` cannot reach the individual steps.
Merge commits keep that history in Git itself, portable to any clone or mirror and addressable by every Git tool, while GitHub-side PR metadata (review threads, per-commit checks) is identical under either strategy - so nothing is given up in exchange.
The standard objection to merge commits - that intermediate commits are WIP noise which pollutes `main` and defeats `bisect` - does not apply under the working discipline here: every commit moves the codebase from one working state to another, so per-commit `bisect` and `blame` are strictly more capable, never noisier.
The one-entry-per-PR reading that squash exists to provide also remains available on demand, as `git log --first-parent main` collapses the history to PR boundaries; squash has no inverse operation, since discarded ancestry cannot be recovered from the repository afterwards.
Merge commits are therefore the superset while a single disciplined committer is the only author.
The trade-off accepted: the full log of `main` is busier than a squash log, and the clean-history guarantee rests on solo discipline rather than enforcement.
When the repo opens to external contributions that guarantee weakens and the balance shifts, so this is revisited then; the change is cheap, as enabling squash is a repository setting that applies only to future merges and rewrites nothing.

### No Conventional Commits in commit messages or PR titles

**What**:  
Commit messages and PR titles are ordinary well-formed Git messages - an imperative summary line, with a body explaining why where needed - carrying no `type(scope):` grammar and no `BREAKING CHANGE` footers.

**Why**:  
Conventional Commits is a machine-facing grammar whose purpose is to let release tooling infer version bumps and generate changelogs; with nothing released, no consumers and no release automation, no machine reads the prefixes and the grammar is pure ceremony.
Its vocabulary is also semantically empty pre-release: a `BREAKING CHANGE` marker on a library nobody has ever depended on breaks no one, and SemVer itself defines major version zero as initial development in which anything may change at any time.
Adopting the grammar now would also quietly pre-commit the release-tooling decision, which is deliberately open: progressive changelog updates curated as part of each PR remain on the table alongside commit-parsing tools like release-please, and the curated-changelog path needs no commit grammar at all.
Waiting forecloses nothing, because commit-parsing tools read history forward from a configurable starting point (the last release tag, or a bootstrap SHA), so the convention can be adopted at the moment it gains a consumer without the pre-adoption history ever needing to conform.
And if the house squash style is adopted at the same time, the convention collapses to well-formed PR titles alone - cheap to start paying then, pointless to pay now.

## Agent guidance lives in `.agents/skills`, and a root `.gitignore` keeps other agent surfaces out

**What**:  
Direct AI guidance, where that guidance is designed primarily for consumption by agents, is contained within [`.agents/skills/`](.agents/skills/), whose [README](.agents/skills/README.md) states the full approach (progressive disclosure, routing, etc..).
A root [`.gitignore`](.gitignore) aims to keep other agent entry points out, for cleanliness (for example, `.claude/` and `.cursor/`).

**Why**:  
How we equip AI coding agents is a concern about how this repository is worked on, not a decision about the SDK's code, so this decision record carries only the signpost and the reason the tree excludes what it does.
The agent surface is deliberately singular: a second entry point, branded or neutral, would only duplicate the metadata the skill mechanism already loads at session start or drift from it over time (maintainability concern).

## `sdk-compliance.yaml` is sparse, and has no CI gate yet

**What**:  
[`sdk-compliance.yaml`](sdk-compliance.yaml) declares only the canonical [`supabase/sdk`](https://github.com/supabase/sdk) capability ids this SDK actually implements, omitting those that would end up being listed as `not_implemented`.
Unlike other SDK repositories, there is no `validate-capabilities.yml` in [`.github/workflows/`](.github/workflows/) calling a reusable compliance workflow.

**Why**:  
The canonical tooling in `supabase/sdk` treats a missing id as `not_implemented` everywhere it matters (parity scoring, site generation, the CLI's own informational-only "not declared" listing), and its README says the file is sparse by design.
A full declaration would be over 200 lines of mostly ceremony, and would rot the way an explicit default always does: as `supabase/sdk` adds, renames or retires ids over the time it takes to build this SDK, an untouched `not_implemented` line for a since-renamed id becomes an "unknown feature id" CI failure that has nothing to do with any change we made.
A sparse file only ever names ids we've verified against the live matrix at the moment we implement them, so it can't go stale that way.

No CI gate exists yet because `supabase/sdk` only hosts reusable compliance workflows for Swift, JavaScript, Python and Dart, each paired with a language-specific public-symbol extractor (the JS side uses TypeDoc, Dart has its own small `package:analyzer` tool).
No such workflow or extractor exists for Go, so there is nothing to call into from this repo's CI today.
When this gets added is TBC, especially given that this SDK repository is not yet open for public visibility.

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
A query string is an ordered multimap, and PostgREST's dialect gives repeated keys meaning: repeated filter keys AND together (`age=gte.18&age=lte.65` is exactly how the next block's `Gte("age", 18).Lte("age", 65)` serializes) and repeated `or=` groups combine.
`map[string]string` is the one shape that cannot represent valid PostgREST queries - a second filter on a column would silently overwrite the first.

Every sibling SDK stores the same multimap shape: [`postgrest-js`](https://github.com/supabase/supabase-js/tree/master/packages/core/postgrest-js) appends to `URLSearchParams`, [`supabase-swift`](https://github.com/supabase/supabase-swift) appends `URLQueryItem`s to an array of pairs, [`postgrest-dart`](https://github.com/supabase/supabase-flutter/tree/main/packages/postgrest) appends via `queryParametersAll`, and [`supabase-py`](https://github.com/supabase/supabase-py/tree/v3) wraps a persistent map of key to vector of values whose `set` appends.
Between the two faithful shapes, the slice of pairs was preferred over `map[string][]string` because it keeps the model free of reference-typed fields: a plain struct copy is safe (pairs are immutable values; writes append after `slices.Clone`), whereas a map field aliases on copy, `maps.Clone` is shallow over the value slices, and one forgotten deep clone in a future `With*` method is a data race - the exact hazard the model exists to remove (supabase-py needed a third-party persistent-collections library to make the map shape safe; the slice gets the same guarantee from the stdlib).

Keys that must not repeat, such as `select`, are enforced structurally in the public layer: `Select` consumes the `QueryBuilder` and returns a `FilterBuilder` with no `Select` method, so a duplicate is unrepresentable before it ever reaches the model.

Singleton keys take replace-semantics through `WithParameterReplacing`, which `Limit` uses so that a repeated call replaces the earlier value - the last-write-wins behavior every sibling SDK implements, and the safe choice given PostgREST documents no behavior for a repeated limit key.

Comma-separated list keys take join-semantics through `WithParameterJoining`, which `Order` uses so that repeated calls grow one `order` pair instead of repeating the key: PostgREST reads only the first `order` parameter for a query level and silently ignores the rest, so a repeated key would drop every term after the first call's.
The ordering refinements extend the newest term through `WithParameterValueAppended`, whose plain string append is sound because the term grammar forbids commas inside a term, so the flat value's tail is always the newest term.

## The query string is rendered by an in-model RFC 3986 writer, not url.Values.Encode

**What**:  
`HTTPRequest` renders parameters through `rawQuery` - pairs in insertion order, each key and value escaped by `escapeQueryComponent` - which percent-encodes only what the pair grammar reads as structure (`&`, `=`, `+`, `%`, `#`), what RFC 3986's query production forbids (spaces, double quotes, controls, non-ASCII octets) and the historical pair separator `;`.
The remaining query characters - the commas, parentheses, dots, colons and asterisks PostgREST's dialect leans on - pass through literally, and a space renders `%20`, never `+`.

**Why**:  
`url.Values.Encode` is form-encoding: it escapes every byte outside the unreserved set and sorts pairs by key, so PostgREST queries render as `select=id%2Cname` noise in logs and tests, in an order no caller wrote, and measurably longer in a comma-dense dialect (three bytes per comma across select lists, in-lists and multi-column order values).
RFC 3986 permits the sub-delimiters literally in a query, PostgREST URL-decodes before parsing - both spellings are identical to the server, as the sibling SDKs prove by shipping both - and PostgREST's documentation writes the literal form throughout, so readability, size and order fidelity are the only stakes and the writer buys all three.
The re-sort this replaces was never a bug: PostgREST reads each parameter out by name, so cross-key order is semantically inert, and the orders it does assign meaning to - repetition and within-key sequence - were already preserved because `Encode` sorts keys only; insertion-order rendering makes the wire read as the model's documented order, nothing more.
`+` and `;` are escaped despite being sub-delimiters because form-decoders - PostgREST's own query parsing included - read `+` as a space, and Go's `url.ParseQuery` rejects `;` outright.
`RawQuery` is the documented home for pre-encoded query text and round-trips byte-for-byte through `url.Parse`, so the writer composes with `http.NewRequestWithContext` without re-encoding.
The escaper is octet-oriented rather than rune-oriented because percent-encoding is defined on octets (RFC 3986 section 2.5's UTF-8-then-escape rule), so any string renders losslessly - arbitrary non-UTF-8 bytes included - where a rune-based walk would silently corrupt invalid sequences to U+FFFD.

## A space renders %20, never form-encoding's +

**What**:  
`escapeQueryComponent` percent-encodes a space as `%20`.
It never emits `+`, which is data on this wire and always travels as `%2B`.

**Why**:  
Spaces genuinely occur in these queries - quoted identifiers such as `"full name"` are a promised `Select` path - and both spellings decode to a space at PostgREST, which parses its query string with http-types' plus-replacing decoder ("It decodes '+' characters to ' '" - `HTTP.parseQueryReplacePlus True`, [`QueryParams.hs:157`](https://github.com/PostgREST/postgrest/blob/426e15bbb4b03ff041fb6b4bc16694b3fa094fcd/src/library/PostgREST/ApiRequest/QueryParams.hs#L157)), so the choice is about which encoding family the renderer belongs to, not correctness against this server.
`%20` is the uniform unsafe-octet rule's own output, where `+` would need a dedicated special-case branch importing the one convention that belongs to form-encoding (the WHATWG form-urlencoded serializer: "0x20 (SP), then append U+002B (+)", [URL Standard](https://url.spec.whatwg.org/#concept-urlencoded-serializer)) into an RFC 3986 renderer.
`%20` is also the only decoder-invariant spelling: it means a space under both RFC 3986 percent-decoding and form-decoding, while `+` means a space only under form-decoding and is a literal plus under RFC 3986 ([section 2.2](https://www.rfc-editor.org/rfc/rfc3986#section-2.2)).
Precedent agrees where a query encoding must be unambiguous: supabase-swift pins the same quoted-identifier case as `%22first%20name%22` ([`PostgrestTransformBuilderTests.swift:34`](https://github.com/supabase/supabase-swift/blob/ebef170a4a6820d064e5909dd4f54e4341f12eb5/Tests/PostgRESTTests/PostgrestTransformBuilderTests.swift#L34)) and AWS SigV4's canonical request rules state "The space character is a reserved character and must be encoded as \"%20\" (and not as \"+\")" ([Create a signed request](https://docs.aws.amazon.com/IAM/latest/UserGuide/create-signed-request.html)).
The `+` spelling was rejected for the extra branch it demands, the decoder-dependence it retains for one character and the divergence from the sibling norm this rendering already follows - its only return would have been one fewer flipped pin in the tests.

## Builder phases are distinct concrete types (typestate); embedding only narrows, interfaces only at the terminals

**What**:  
`From` returns a concrete `QueryBuilder`; `Select` returns a concrete `FilterBuilder`; every builder method returns a concrete type, never an interface.
`QueryBuilder` and the ordering refinement wrappers (`OrderedFilterBuilder`, `OrderedDescendingFilterBuilder`) embed `FilterBuilder` to extend its method set, satisfying `Query` through promotion.
The read functions accept the sealed `Query` interface, whose one unexported method returns a `queryState` token binding the row type; builder states satisfy it and nothing outside the package can.
These wrapper structs have identical definitions on purpose: a builder type's identity is its method set - which chain steps are legal from here - not its field set.

**Why**:  
The types encode the phase of the chain, so illegal chains are compile errors: `Select` twice is unrepresentable, because `Select` consumes the `QueryBuilder` and `FilterBuilder` has no `Select`.
Every sibling SDK accepts the double call and resolves it silently, last write wins (postgrest-js `searchParams.set('select', ...)`, supabase-swift `appendOrUpdate`, postgrest-dart `overrideSearchParams`, supabase-py via inheritance).
They re-expose select after a verb because mutations need a "return these columns" variant, which this SDK spells as the terminal mutation builder's `Returning`, with replace semantics.

Interface-typed returns would hide the fluent surface from godoc and autocomplete without buying substitutability we need, so chain methods return concrete types and the read functions instead accept the sealed `Query` interface - interfaces in, concrete types out, the posture Google's Go style guidance names outright, with the interface living in the package that consumes query values.
A type parameter no method signature mentions is inert - `Query[Instrument]` and `Query[Section]` would define identical type sets and so be the same type, letting a wrong-row `Collect[Section](instrumentsQuery)` compile while `Row` inference fails at every call site - so the row type is bound where the read path already has a genuine method, `state`'s return type, keeping the public interface free of never-called members (a dedicated phantom anchor method was drafted and rejected for exactly that deadness).
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
What is not said is not sent - the ordering entry's principle - so the absent parameter relies on the server's documented `*` default rather than restating it, and `Select("")` keeps its documented `select=*` meaning because argument values, unlike chain steps, cannot be policed by the type system anyway (`Select("*")` proves as much).
Sibling behavior marks the safe boundary: postgrest-dart's dispatcher can be awaited through inheritance while its HTTP method is still null, forcing a runtime `ArgumentError`, whereas supabase-swift bakes `.get` into the request at `from()` and its inherited `execute()` serves the select-less GET successfully - opening the boundary is sound exactly when the method is fixed at `From`, as this SDK's request model does.
The write verbs sit where their inputs demand: `Insert` on `QueryBuilder`, where any promoted method closes the verb window the same way it closes the projection window, and `Update` on `FilterBuilder`, after the row-choosing filters.

## Builder state serializes immediately into the request model

**What**:  
Every builder method serializes its effect into the internal request model at call time - `Select` writes the `select` parameter immediately.
Builders hold no structured intermediate state (no columns, filters or limit fields), and the read functions' shared `execute` path performs no assembly beyond handing the model a base URL.

**Why**:  
The wire format is the canonical state in every sibling SDK - postgrest-js mutates `URLSearchParams` inside each method, supabase-swift appends `URLQueryItem`s, postgrest-dart rewrites the `Uri`, supabase-py updates its `URLQuery` - so behavior parity with the reference implementation is auditable call by call: our `Select` does what theirs does, at the same moment.
A structured representation assembled at execution time would be a second source of truth whose serializer must track the reference forever, for no validation gain: ordering rules ("X not before/after Y") are enforced earlier and stronger by the typestate split, and value-level conflict rules, when a concrete one arrives, can read the model through a narrow predicate (a `HasParameter`-style query added then) - normalization loses no state a known rule needs.
The only information call-time serialization erases is which method wrote a pair; no PostgREST rule branches on that provenance, and the siblings validate almost nothing themselves, delegating conflicts to PostgREST's own errors, which this SDK surfaces as `*Error`.

## Ordering is refined by postfix typestate builders

**What**:  
`FilterBuilder.Order(column)` writes the bare column as a new order term and returns `OrderedFilterBuilder`, which embeds `FilterBuilder` and adds `Descending` and `NullsFirst`. `Descending` returns `OrderedDescendingFilterBuilder`, which adds only `NullsLast`, and the null-placement methods return the plain `FilterBuilder`. Each state offers only departures from what the term already implies, direction strictly before nulls, and every refinement appends its token to the newest order term. A refinement restating a server default (`NullsLast` while ascending, `NullsFirst` once descending) has no method, so each direction and null-placement combination has exactly one incantation. `Order` itself takes no direction or null-placement parameter.

**Why**:  
An order term's programmable space is two independent binary axes - direction and null placement - and encoding the remaining choices in the returned type makes every meaningless sequence a compile error instead of a runtime ruling: a duplicated `Descending`, contradictory null placements and nulls-before-direction do not build, and a default-restating spelling does not exist, so no combination has two spellings - the same discipline the phase types already apply to a second `Select`. Direction-before-nulls is what keeps every refinement a plain string append onto already-serialized state; a commutative surface would need the request model to parse and reorder the term it wrote, against the serialize-immediately decision. The alternatives each answered the two axes worse: a four-value enum (`Order("name", Descending)`) was the runner-up with zero new types and untouched terminals, but it spells the ordering as an argument rather than chain steps and its un-suffixed names silently carry SQL's coupled null defaults where the postfix methods surface them as explicit, documented steps; bare boolean pairs are unreadable at call sites; a two-boolean struct's nulls zero value silently diverges from SQL's descending default; variadic tokens re-admit the contradictions the typestate forbids. What is not said is not sent: an unrefined axis relies on the server's documented defaults, and each refinement method documents the placement it produces.

## The `Range` modifier is inclusive at both ends and compiles onto PostgREST's `limit` and `offset`

**What**:  
`FilterBuilder.Range(from, to)` narrows the result to the rows at zero-based positions `from` through `to` inclusive, serialized immediately as `offset=from` and `limit=to-from+1` through the request model's replace semantics on both keys. A later Range replaces both pairs, Range and Limit replace each other's row cap in either order and a start offset outlives a later Limit. Bounds are computed verbatim with no validation: `Range(2, 1)` sends `limit=0` and requests zero rows, anything smaller sends a negative cap for the server to reject, and a negative `from` is forwarded untouched. There is no half-open spelling, no standalone Offset method and no referenced-table parameter.

**Why**:  
The zero-based inclusive contract is family-wide - every sibling SDK computes `offset=from` and `limit=to-from+1` with set-semantics on both keys, and the Supabase documentation teaches `range(0, 9)` returns ten rows - so a half-open Go spelling in the slice tradition would silently return one fewer row to anyone porting a documented example, an invisible off-by-one this SDK refuses to create; the doc comment carries the inclusivity and the arithmetic instead. PostgREST's Range-header mechanism carries the same information but no sibling uses it, it cannot address embedded resources and it would open a second serialization surface beside the query-string model. The Limit interplay is not bespoke code: both methods write the singleton `limit` key through `WithParameterReplacing`, so last-cap-wins falls out of the multimap decision, exactly the observable contract the sibling SDKs pin in their tests. A typestate exclusion of a second cap writer was considered and passed over: caps have no grammar to enforce (unlike direction-before-nulls), and a capped state would have to re-expose the whole filter surface for one unrepresentable-repeat guarantee the family universally spells as last-write-wins. A standalone Offset method exists only in supabase-py, has no capability id in the canonical matrix and adds nothing Range does not express. The referenced-table variant is deferred to the block that introduces relationship embedding, where Order, Limit and Range need one uniformly spelled referenced-table story rather than three ad-hoc ones.

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
The accepted costs - `Row` is a phantom threading through builders whose state never depends on it, and a finished query cannot fork into differently-typed decodes - stay shallow: an in-package `Retype[U](query)` is purely additive ([partial type argument lists](https://go.dev/ref/spec#Instantiations)) and `From[json.RawMessage]` covers raw rows, which is also why no `any`-typed `Execute` front door exists.
Execution is a package-level function - methods cannot declare type parameters below go1.27, the module floor - context-first per the standard's context mandate, following `slices.Collect` and [`iter.Pull`](https://pkg.go.dev/iter#Pull) as free generic functions over values (stripe-go's range-over-`Seq2` lists and openai-go's auto-paging extend the shape to paging), and array-ness as the return contract makes destination-pointer questions (nil-ness, preallocation) unrepresentable.
`Response` stays a plain exported-field record because it is returned by value and holds only scalars, so consumers hold independent copies and no aliasing exists to defend against, while unexported fields would stop consumers fabricating a `Response` in their own test doubles.
This argument is scalar-dependent: a reference-typed field (headers, raw body) must not be added to `Response` without revisiting it.

## `postgrest.Error` carries the parsed PostgREST body plus HTTP status

**What**:  
Non-2xx PostgREST responses become `*postgrest.Error` with exported `HTTPStatus`, `Code`, `Message`, `Details`, `Hint` fields, matched via `errors.As`, plus an `Unwrap` returning an (currently usually nil) underlying cause.
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
Integration tests are build-tagged `integration`, env-gated and run under `-race`.
They live in test-only `integrationtest` packages beside the code they exercise, consuming only the public API, and selection is by the tag alone: the script passes no `-run` name filter, so the hermetic unit tests compiled under the tag simply run again in the integration job.
The CI job runs the same `["1.25", "stable"]` matrix as build-and-test; `go vet -tags integration` in the unit script additionally keeps the tagged file compiling for fast local signal.

**Why**:  
The CLI cannot be installed with `go install` at v2 for two independent reasons: its module (`github.com/supabase/cli`) now lives in `apps/cli-go/` while the repo root carries no `go.mod`, so the module proxy resolves that path only to the stale v1 root-module history rather than the v2 code, and its `go.mod` carries local `replace` directives, which `go install pkg@version` refuses outright.
It is fetched instead as the pinned release binary, verified against a committed SHA-256 and installed into Go's own bin directory (GOBIN, else GOPATH/bin) - a writable, on-PATH location outside the checkout, so a read-only working tree is fine - the same first-party curl-and-checksum pattern as the local Go toolchain install.
npm was rejected as the channel even though it pins equally well, because bundling the CLI into `tools/node` conflated it with the unrelated cspell tool - every `npm ci` pulling both - and forced a node_modules write into the checkout, whereas cspell stays on npm as a genuine JS tool whose deep dependency tree is what a lockfile exists for.
The auth service stays enabled despite no test calling it, because `supabase status -o env` emits the stack's API keys (PUBLISHABLE_KEY included) only while auth is enabled - the harness reads its credentials from that output, consuming PUBLISHABLE_KEY exactly as the CLI repository's own e2e harness and the Swift SDK's integration tests do (ANON_KEY is deprecated upstream).
Schema lives in `migrations/` and only data in `seed.sql` because the CLI applies the seed as a single batch whose statements are prepared before earlier ones execute, so DDL cannot ride with inserts that depend on it (SQLSTATE 42P01 on a fresh stack) - the same layout as the CLI repository's own e2e project and the Swift SDK's.
The script runs the CLI against a disposable `mktemp -d` copy of `integration/` because the CLI writes scratch state (`supabase/.branches`, `supabase/.temp`) into whatever project directory it runs: the copy keeps committed trees pristine by construction (no scratch to gitignore, unlike upstream projects that gitignore it inside a writable tree) and lets the harness run from a read-only checkout, while `stop` still finds the stack because the CLI identifies it by `config.toml`'s `project_id`, not by path.
Disabling every other unused service attacks the block's stated risk head-on: this harness's startup time and flakiness set the floor for all future CI.
A name-anchored `-run` filter (`^TestIntegration`) would spare the unit re-run, but its failure mode is silence: a tagged test named outside the anchor compiles cleanly, never runs and lets the suite pass vacuously, whereas the re-run it prevents is hermetic and costs seconds.
The dedicated test-only package makes the consumer stance structural - every test package is external, so unexported access never exists to lose - and keeps the integration namespace decoupled from the unit test files, so suite selection never depends on function names and names never collide across suites.
The floor leg exists because the published `go 1.25` directive is a compatibility promise to consumers, and only a live-stack run proves that promise end to end on the floor toolchain; the legs run in parallel so wall-clock cost is unchanged.

## `X-Client-Info` resolution is proven by an out-of-tree consumer program

**What**:  
The `telemetrytest/` module is a stand-in consumer: it requires the SDK modules at fabricated, self-labeled versions (`v0.999.1-fabricated` supabase, `v0.999.2-fabricated` postgrest), `replace`s them to the local working tree and its main program asserts the exact `X-Client-Info` value each entry point sends to a local HTTP server.
`scripts/telemetry-test.sh` runs it with `GOWORK=off` and the module is not listed in `go.work`.
A second leg rebuilds the same program in GOPATH mode (`GO111MODULE=off`), where binaries carry build information without module records, and asserts the version-unknowable `0.0.0` fallback in every header.
The `TELEMETRY_TEST_MODE` environment variable tells the program which expectations to hold.
The check is part of the fast tier (`check-fast.sh`) and runs in CI as a step of the build-and-test job, on its `["1.25", "stable"]` matrix.
The probe is a plain program, not a `go test` suite.

**Why**:  
Every binary the in-repo suites produce has one of this repository's modules as its main module, so header resolution takes the in-tree branch and reports `(devel)`.
The branch every published-module consumer exercises - reading client versions from build-information dependency records - is reachable only from a main module outside the SDK's module tree.
It must be a plain program because `go build` and `go run` stamp dependency records into binaries while `go test` binaries record the main module and no dependencies (observed on go1.26), which rules out expressing the probe as a test suite.
Workspace membership would defeat the vantage from the other side - a workspace build supplies the SDK modules as local source with no resolvable versions - so the module stays out of `go.work` and the script forces `GOWORK=off`.
The fabricated versions are distinct from every sentinel the header can otherwise carry (`(devel)` in-tree, `0.0.0` without build information), so a pass is unambiguous provenance, and their `-fabricated` prerelease label keeps the header values in check output from reading as release claims.
Each must outrank every other require of the same module path in this build so minimal version selection keeps it as the selected, recorded version: `0.999.x` outranks the entire real `v0` series and deliberately loses to the first real `v1` require, so the fixture fails loudly at GA instead of surviving it silently.
The floor leg exists because the header is consumer-facing behavior and `go 1.25` is the consumer contract.
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
Every floor-carrying artefact - the published `go.mod` directives, [`go.work`](go.work), the [`telemetrytest`](telemetrytest/) stand-in consumer and [CI](.github/workflows/ci.yml)'s floor matrix legs - carries the oldest Go major release still supported by the Go project, currently `1.25`.
This floor should be raised opportunistically after each Go release rather than on release day.
The policy is stated consumer-facing in [our root `README.md`](README.md) ("Supported Go versions").

**Why**:  
The standard library is statically linked into every consumer binary and only the two newest majors receive security fixes, so a floor inside Go's support window never claims compatibility with toolchains whose binaries cannot be patched.
A lower floor buys no reach: every Go line below `1.25` is end of life, so no supported-toolchain consumer distinguishes `1.25` from lower floors.
The ecosystem this SDK composes with already sits at the same point - `golang.org/x` applies the two-release policy to itself and [pgx](https://github.com/jackc/pgx), [grpc-go](https://github.com/grpc/grpc-go), [google-cloud-go](https://github.com/googleapis/google-cloud-go) and [the original Supabase community postgrest-go](https://github.com/supabase-community/postgrest-go) all require 1.25 - so the floor is aligned rather than pioneering.

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
`Execute` accepts a sealed `Mutation[Row]` interface embedding `Query[Row]` behind a second unexported marker, so only write builders reach it and a read passed to `Execute` is a compile error.

**Why**:  
A write request is identical whether or not its rows come back - only the `Prefer` header differs - so the choice belongs beside the choice of decoding (executing functions), not in the builder's state.
Minimal by default matches the PostgREST server default, and never transfers rows a caller does not read - the representation is opt-in through choosing `Collect` over `Execute`.
This is also why the row-level-security interaction is a pure function of the executing function: an insert whose role may not `SELECT` succeeds through `Execute` yet fails through `Collect` with PostgreSQL's `42501`, because only that representation path emits the `RETURNING` that needs the `SELECT` right.

## Update changes are an opaque `any` payload marshaled to one JSON object

**What**:  
`Update(changes any)` takes the column assignments as an opaque value, marshaled with `encoding/json` to one JSON object and sent as the PATCH body.
A `map[string]any` is the recommended shape, where a key carrying nil clears its column to SQL null and an absent key leaves the column untouched, and a struct is accepted with the documented caution that every marshaled field is assigned, its zero value included.
There is no typed-changes type and no client-side pruning of which fields to send.

**Why**:  
An insert names whole rows, so its payload is the query's row type `T`, but an update assigns an arbitrary subset of columns that no single Go type expresses without a per-table partial-update wrapper or pervasive pointer fields.
Taking `any` and marshaling it straight to JSON matches how every sibling SDK accepts update values and lets the caller pick the shape that fits: a `map[string]any` to send exactly the named columns with explicit nulls, or a tagged struct when a fixed shape is more convenient.
The map's null-versus-absent distinction is the one PostgREST acts on, so the SDK carries it faithfully rather than inventing a sentinel for "clear this column", and the struct caution is documented rather than hidden because Go's zero values are indistinguishable from unset without field tags.

## `Update` is a terminal verb on `FilterBuilder`, so writes reuse the read filter surface

**What**:  
`Update(changes)` is a method on `FilterBuilder[T]`: the row-choosing filters chain first, exactly as they do on a read, and the verb ends the chain by returning the terminal `MutationBuilder`, which offers no filter methods.
Reachable through promotion, the verb called directly on `From`'s builder - no filters - addresses every row of the table.
`Insert` stays on `QueryBuilder` alone, so a filtered chain reaching `Insert` does not compile.
`Order`, `Limit`, `Range` and `Select` remain reachable before `Update`, traveling for the server to rule on, and there is no mutation-typed mirror of any filter method.

**Why**:  
An update scopes its rows with the same filters a read scopes its result, and placing the verb after the filters lets the one `FilterBuilder` surface serve both sides - one representation of every operator, with every future filter extending reads and writes at once, and a stored filtered scope reusable as a read through `Collect` and as a write through the verb.
The sibling SDKs' verb-first ordering (`update(...).eq(...)`) would require a mutation-typed duplicate of the entire filter surface, because the sealed `Mutation` typestate must survive the filter chain and a chained Go method cannot return its receiver's concrete type generically: a mirror of every filter method plus a compliance registration per method, a cost out of all proportion to the ordering familiarity it buys.
A porter reordering a sibling chain is guided by compile errors, since the terminal builder offers no filter methods.
The read-shaped modifiers are deliberately not fenced off the write path: PostgREST 13 dropped limited update/delete ("The feature was complicated and largely unused", [PostgREST changelog](https://github.com/PostgREST/postgrest/blob/main/CHANGELOG.md)), so `order` or `limit` riding a mutation is the server's to rule on - the same posture this SDK takes for a negative limit or an empty list - and a `Select` written before `Update` genuinely projects the returned representation, exactly as `Returning` does, with `Returning` replacing any projection `Select` wrote.
