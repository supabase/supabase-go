# Go SDK Development: What Good Looks Like

This document is the authoritative form of "the standard" for this repository, stating what must hold for the SDK we ship.

How to build and work here is documented in [`DEVELOPMENT.md`](DEVELOPMENT.md), and the reasons why things are the way they are is documented in [`decisions.md`](decisions.md).

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [BCP 14](https://www.rfc-editor.org/info/bcp14) ([RFC 2119](https://www.rfc-editor.org/rfc/rfc2119), [RFC 8174](https://www.rfc-editor.org/rfc/rfc8174)) when, and only when, they appear in all capitals, as shown here.

A deviation from a SHOULD in this document are recorded in [`decisions.md`](decisions.md) with its justification.

## Introduction

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
- Dependencies MUST be permissively licensed (MIT, Apache 2.0, BSD) to protect our SDK users. "Viral" copyleft licenses (the GPL family) MUST NOT be taken on. Test-only dependencies and build-time generators do not ship to consumers but SHOULD follow the same hygiene.

## The Good

These are things within the Go ecosystem that we should embrace closely:

- **gofmt**: Opinionated Go source code formatting. Code MUST be gofmt-clean.
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
- **Zero-Config Tracing** (Observability): An open question. Supporting [OpenTelemetry](https://pkg.go.dev/go.opentelemetry.io/otel) by default means instrumenting complex SDK behaviors (retries, fan-outs) with custom spans, exported through the consumer's OTel provider (the global one, or one injected via options), at the cost of the OTel API becoming a hard dependency for every consumer. The alternative is pluggable tracing. Either way, `context.Context` MUST flow through all I/O paths so trace context can propagate.
- **Documentation** "as the Product Surface": Every exported identifier must have a Go-idiomatic doc comment. We provide runnable [`Example` functions](https://pkg.go.dev/testing#hdr-Examples) in our test files. Because `go test` compiles and verifies these examples, our documentation snippets can never go stale or lie to the user, with the added free benefit that they render automatically as interactive snippets on [pkg.go.dev](https://pkg.go.dev/).

Plus, there's the behind-the-scenes hygiene in terms of quality assurance:

- **Unit Testing**: Comprehensive unit tests MUST accompany our code. Assertion and mocking libraries (for example [`testify`](https://pkg.go.dev/github.com/stretchr/testify) and [`mockery`](https://github.com/vektra/mockery)) MAY be adopted later if deemed necessary, but the default preference should be to keep tests standard-library-only.
- **Integration Testing**: Integration tests SHOULD exercise the SDK against real services, either local-stack or remote-stack, ideally both.
- **`go vet` as a baseline**: `go vet` is the Go toolchain's correctness checker, catching bugs that formatters and style linters do not. `go test` runs a high-confidence subset of its checks automatically, and the full suite MUST run in CI.
- **Deeper Formatting, Linting and Static Analysis**: Static analysis beyond `go vet` MUST run in CI, with commands reproducible at a developer's workstation. Baseline SHOULD include [gofumpt](https://github.com/mvdan/gofumpt) plus:
    - `staticcheck`: Detects unused unexported declarations and struct fields, suspicious constructs, incorrect `context` usage and inefficient allocations.
    - `errcheck`: Enforces that errors aren't silently ignored, instead that they are explicitly handled or intentionally suppressed.
    - `revive`: Idiomatic naming conventions, exported comment structures and coding standards.
- **Generate for a typed REST surface** (do not reflect!): Where the API is described by an OpenAPI document we SHOULD prefer generating the typed client and models at build time via `go generate` over runtime reflection.
- **Automated API Compatibility Checking**: We MUST NOT rely on humans alone to catch breaking changes to our SDK APIs. Once there is a previous release to compare against, [`gorelease`](https://pkg.go.dev/golang.org/x/exp/cmd/gorelease) (or [`apidiff`](https://pkg.go.dev/golang.org/x/exp/cmd/apidiff)) MUST gate CI, mechanically verifying the API surface against it and strictly enforcing SIV.
- **Race Detection**: The test suite MUST run under the race detector (`go test -race`) in CI. For an SDK that does anything concurrent this is essential and costs only CI minutes.
- **Vulnerability Auditing**: [`govulncheck`](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) MUST run in CI.

And there are other things that we may or may not choose to embrace and use from the outset:

- A defined approach to including critical upstream security patches (`go get -u`).
- **Future-Proofing for `encoding/json/v2`**: Go is currently testing a massive, stricter revamp of the JSON standard library (available via `GOEXPERIMENT=jsonv2` since Go 1.25 and still experimental as of Go 1.26, so stabilisation will be Go 1.27 at the earliest). Because JSON I/O will be a backbone of our SDK, we must design our data models defensively today. This means making deliberate choices around `struct` tags (explicitly using `omitempty` or the newer `omitzero`) and strictly defining our stance on unknown JSON fields, ensuring our eventual migration to v2 is as friction-free as we can possibly make it for our SDK users.

## The Bad

These are things within the Go ecosystem that we should avoid, including classically anticipated design faults and anti-patterns that come with the territory, in particular for the less-experienced Gopher:

- **Cgo**: Allows Go packages to call C code. Introduces memory safety risks and can break cross-compilation. We should stick to the Go proverb that "Cgo is not Go".
- Letting a `panic` cross our API boundary: "Don't panic" is a Go proverb. Expected failures must be returned as `error` values and any goroutine we spawn internally must `recover` so that a background panic can never take down the consumer's process.
- Forcing SDK users to use `reflect.DeepEqual`.
- We MUST NOT use global loggers or `stdout` within the SDK implementation (see "Silent by Default" in [The Good](#the-good)).
- Leaning on Runtime Reflection and "Magic": Reaching for the `reflect` package (for example, dynamic type inspection or `reflect.MakeFunc`) to build overly clever abstractions in our own code. "Reflection is never clear" is a Go proverb due to how it sacrifices static type safety and readability, defeating compiler optimisations and introducing unpredictable runtime panics. It's worth noting that the standard library does use reflection responsibly under the hood (for example, `encoding/json`) so relying on that is fine. If we need dynamic behaviour we should prefer implicit interfaces, and if we need to reduce boilerplate we should prefer build-time code generation (`go generate`) - for example generating a typed client and models from an OpenAPI document.
