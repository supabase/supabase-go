# Go SDK Development: What Good Looks Like

## Introduction

This document has been authored by [@QuintinWillison](https://github.com/QuintinWillison) in order to snapshot the current state of the art in terms of best practice and "what good looks like" in 2026 when it comes to SDK development for users of the Go programming language.

AI (a mix of Google's Gemini and Anthropic's Claude) has been used to research what went into this document as well as review this document prior to sharing with the wider team. Otherwise this document is entirely written by a human for consumption by other humans. It should also be useful as input for AIs downstream as work embarks on Go SDK development at Supabase.

Go was designed at Google for:

- Simplicity, reducing "paradigm complexity".
- Rapid compilation.
- Static linking.
- Explicit flow control.
- First-class, lightweight concurrency (goroutines and channels).
- Maintainability at scale.

Goals we should aim for as Go SDK developers, all of which hold true for other SDK languages too, include:

- Designing for defensibility with a small, deliberate exported surface that we can keep stable and evolve without breaking consumers. We should favour opaque types and options over long positional parameter lists. We must avoid exposing third-party types (permanent coupling).
- Focussing on API stability.
- Go-idiomatic behaviours, including intuitive synchronisation boundaries.
- Encapsulation and immutability:
    - `/internal`
    - unexported (private) fields, constructors, getters (read-only access)
- Package-oriented design, keeping it simple (that is, domain packages reside at the repository root for immediate discoverability).
- Mitigating supply chain vulnerabilities, including by taking on minimal dependencies.
- Only using permissively licensed (MIT, Apache 2.0, BSD) dependencies to protect our SDK users. "Viral" copyleft licences like GPL cannot be allowed. Test-only dependencies and build-time generators do not ship to consumers, but should follow similar hygiene.
- Delivering a frictionless developer experience.

## The Good

These are things within the Go ecosystem that we should embrace closely:

- **gofmt**: Opinionated Go source code formatting, to which we must adhere.
- **stdlib**: Go's standard library is famously capable and robust, including built-in support for common primitives that most SDKs will need such as cryptography, HTTP transport and JSON parsing.
- **Minimal Version Selection (MVS)**: Guarantees deterministic builds for our users.
- **Semantic Import Versioning (SIV)**: Supports breaking changes when they are unavoidable. However, it's imperative that we aim to design our APIs carefully and with flexibility at their core from the outset to avoid the need to make breaking changes later on.
- **Decentralised Distribution**: Go deliberately avoids an `npm`style central registry. Modules are distributed directly from our GitHub (meaning our module path is our repo path) and the release process is pushing a [SemVer](https://semver.org/) git tag (`vX.Y.Z`). The ecosystem's automatic proxy, checksum database and documentation site ([pkg.go.dev](https://pkg.go.dev/)) give us global caching, cryptographic immutability and hosted documentation for free.
- **Goroutines and Channels**: Obvious but worth highlighting that concurrency is a little different to other languages - "don't communicate by sharing memory, share memory by communicating" (a Go proverb).
    - Accept **`context.Context`** into all functions that perform I/O or may otherwise block.
    - Listen for context's `Done()` channel in implementations - for example when background polling or batching asynchronously.
    - Use `defer cancel()` where appropriate - for example when creating SDK-internal contexts, ensure they get cleaned up properly.
- **Functional Options Pattern (FOP)** for optional overrides: Embracing Go's functional capabilities, similar-yet-distinct-from the builder pattern in other languages. Avoid [the dysfunctional anti-pattern](https://rednafi.com/go/dysfunctional-options-pattern/).
- **Owning the Lifecycle of Background Work**: We **must never** start a goroutine without a clear way to stop it. Any long-lived work we own (a Realtime subscription, a reconnect loop, a batch flusher) must be stoppable by the consumer via a `Close`/`Shutdown` method or context cancellation. We must release resources and never leak. We should document the concurrency-safety contract of our exported types (for example "a Client is safe for concurrent use by multiple goroutines"), aligned with how the standard library does it.

Alongside the above there are things we should ensure we do in order to be good citizens:

- **Conservative Minimum Required Go Version**: An SDK should lag the bleeding edge, typically supporting the current release and a couple before it, to avoid forcing a toolchain upgrade on consumers just to adopt us. We should also build in CI with the latest/newer toolchain versions to verify forward compatibility.
- **Safer Concurrency** (Go 1.22+ and bearing in mind the bullet point before this): If we set our `go.mod` minimum to at least version `1.22` then loop variables automatically receive per-iteration scope. This eliminates a classic, difficult-to-debug "footgun" where closures and goroutines accidentally capture a shared loop variable, freeing us from the historical `v := v` shadowing dance.
- Provide **`Equal`** methods on our immutable types, where appropriate.
- Prudent and informed pre-allocation of slices to anticipated capacity.
- Targeted Use of **Implicit Interfaces** for **Mockability** - "accept interfaces, return structs":
    - Outputs - **return `struct`s**: We export concrete structs. This empowers the consumer to define their own minimal, custom interfaces (duck typing) for frictionless unit testing without us forcing a massive SDK interface on them.
    - Inputs - **accept `interface`s**: We only define small, focused interfaces (like `io.Reader` does) when we need to accept flexible inputs, or to decouple our own internal dependencies for testing.
- Conform to the **HTTP Pipeline Design Pattern** (interceptor / middleware): `http.RoundTripper`.
- **Stringent Error Hygiene**: Enable frictionless use of `errors.As` and `errors.Is` for SDK users by defining clear sentinel errors and custom error types, layering in domain context when appropriate. Making deliberate, considered choices about when to wrap (`%w`) versus format (`%v`) when propagating internal errors.
- **Silent by Default**: Return errors to the caller for them to handle. If internal logging is unavoidable then let the consumer inject a logger via options. Idiomatically that means an `slog.Logger` or `slog.Handler` (Go 1.21+), defaulting to a no-op handler so we emit nothing unless asked.
- **Careful and Considered Use of Generics** (Go 1.18+): Use type parameters for type-generic building blocks but do not lean on them as a "hammer". Go generics deliberately lack variance (no covariance or contravariance) so patterns ported from C# or Java generic hierarchies will not translate. Therefore, we should design with that constraint rather than fight it.
- **Make the Zero Value Useful** where we reasonably can: Aligning with the designs of `sync.Mutex` and `bytes.Buffer`, the zero value of an exported type should ideally be safe to use with functional options supplying any non-trivial defaults.
- **Zero-Config Tracing** (Observability): Support [OpenTelemetry](https://pkg.go.dev/go.opentelemetry.io/otel) by default. Always pass `context.Context` so trace context propagates. Instrument complex SDK behaviours (retries, fan-outs) with custom spans using the OTel API, but rely on the consumer's OTel provider (the global one, or one injected via options) to actually export the telemetry. This would mean taking the OTel API as a hard dependency for every consumer, which might be an acceptable cost because it's lightweight (alternatively tracing could be made pluggable instead).
- **Documentation** "as the Product Surface": Every exported identifier must have a Go-idiomatic doc comment. We provide runnable [`Example` functions](https://pkg.go.dev/testing#hdr-Examples) in our test files. Because `go test` compiles and verifies these examples, our documentation snippets can never go stale or lie to the user, with the added free benefit that they render automatically as interactive snippets on [pkg.go.dev](https://pkg.go.dev/).

Plus, there's the behind-the-scenes hygiene in terms of quality assurance:

- **Unit Testing** - probably alongside [`testify`](https://pkg.go.dev/github.com/stretchr/testify) (assertions) and [`mockery`](https://github.com/vektra/mockery) (mock code generator).
- **Integration Testing** (local stack and remote stack, if possible).
- **`go vet` as a baseline**: `go vet`, run automatically by `go test`, is the standard library's correctness checker, catching bugs that formatters and style linters do not.
- **Deeper Formatting, Linting and Static Analysis**: Just adopt [gofumpt](https://github.com/mvdan/gofumpt) or go *all in* with [golangci-lint](https://github.com/golangci/golangci-lint), including:
    - `staticcheck`: Detects unused unexported declarations and struct fields, suspicious constructs, incorrect `context` usage and inefficient allocations.
    - `errcheck`: Enforces that errors aren't silently ignored, instead that they are explicitly handled or intentionally suppressed.
    - `cyclop`: Cyclomatic complexity limits.
    - `exhaustruct`: Enforces that all fields are set in `struct` literals, so a field added later is not silently left at its zero value.
    - `revive`: Idiomatic naming conventions, exported comment structures and coding standards.
- **Generate for a typed REST surface** (do not reflect!): Where the API is described by an OpenAPI document we should prefer generating the typed client and models at build time via `go generate` over runtime reflection. A spec-first language such as [TypeSpec](https://typespec.io/) can serve as the single source of truth that emits that OpenAPI document. To-be-confirmed as this depends on what is available elsewhere at Supabase or what might be created now to enable this.
- **Automated API Compatibility Checking**: We do not rely on humans to catch breaking changes to our SDK APIs. We use [`gorelease`](https://pkg.go.dev/golang.org/x/exp/cmd/gorelease) (or [`apidiff`](https://pkg.go.dev/golang.org/x/exp/cmd/apidiff)) in CI to mechanically verify our API surface against the previous release. This strictly enforces SIV.
- **Race Detection**: Ideally we should run the test suite under the race detector (`go test -race`) in CI. For an SDK that does anything concurrent this is considered essential and costs us only in CI minutes.

And there are other things that we may or may not choose to embrace and use from the outset:

- A defined approach to including critical upstream security patches (`go get -u`).
- Regular vulnerability auditing (`govulncheck`).
- **Future-Proofing for `encoding/json/v2`**: Go is currently testing a massive, stricter revamp of the JSON standard library (available via `GOEXPERIMENT=jsonv2` since Go 1.25 and still experimental as of Go 1.26, so stabilisation will be Go 1.27 at the earliest). Because JSON I/O will be a backbone of our SDK, we must design our data models defensively today. This means making deliberate choices around `struct` tags (explicitly using `omitempty` or the newer `omitzero`) and strictly defining our stance on unknown JSON fields, ensuring our eventual migration to v2 is as friction-free as we can possibly make it for our SDK users.

## The Bad

These are things within the Go ecosystem that we should avoid, including classically anticipated design faults and anti-patterns that come with the territory, in particular for the less-experienced Gopher:

- **Cgo**: Allows Go packages to call C code. Introduces memory safety risks and can break cross-compilation. We should stick to the Go proverb that "Cgo is not Go".
- Letting a `panic` cross our API boundary: "Don't panic" is a Go proverb. Expected failures must be returned as `error` values and any goroutine we spawn internally must `recover` so that a background panic can never take down the consumer's process.
- Forcing SDK users to use `reflect.DeepEqual`.
- Using global loggers or `stdout` within the SDK implementation (see "Silent by Default" in [The Good](https://app.notion.com/p/Go-SDK-Development-What-Good-Looks-Like-3825004b775f803ab444e83c7a5ebc4b?pvs=21)).
- Leaning on Runtime Reflection and "Magic": Reaching for the `reflect` package (for example, dynamic type inspection or `reflect.MakeFunc`) to build overly clever abstractions in our own code. "Reflection is never clear" is a Go proverb due to how it sacrifices static type safety and readability, defeating compiler optimisations and introducing unpredictable runtime panics. It's worth noting that the standard library does use reflection responsibly under the hood (for example, `encoding/json`) so relying on that is fine. If we need dynamic behaviour we should prefer implicit interfaces, and if we need to reduce boilerplate we should prefer build-time code generation (`go generate`) - for example generating a typed client and models from an OpenAPI document.

## Glossary

### Go

This is the canonically correct name of the programming language.
Unfortunately it's not a great word when it comes to indexing in search engines and LLMs, due to being so short and commonly used in everyday language. Hence [golang](https://app.notion.com/p/Go-SDK-Development-What-Good-Looks-Like-3825004b775f803ab444e83c7a5ebc4b?pvs=21) is often used instead.

### Golang

This is the term heavily adopted in the community, preferred over 'just' [go](https://app.notion.com/p/Go-SDK-Development-What-Good-Looks-Like-3825004b775f803ab444e83c7a5ebc4b?pvs=21) for disambiguation and SEO.

### Gopher

The official mascot of the Go programming language, appearing all over the place including official branding, merchandise and error pages.
Also used as a term for a person who writes code in Go - for example, "I am a gopher".