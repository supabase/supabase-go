---
name: designing-internal-boundaries
description: Guides the design of internal type contracts in respect of immutability and encapsulation. Enhances codebase maintainability by separating concerns in a manner that is compiler-enforced, both for internal state as well as immutable values returned to callers alike. Use when creating or reworking types, when adding machinery (e.g. parsing, caching, transport, crypto), when naming or splitting an internal package, when writing constructors or defensive copies and when deciding whether a test is external or in-package.
---

In Go the package is the only compiler-enforced encapsulation boundary. File boundaries are decorative, so any file in a package can assign any unexported field declared anywhere in that package. A contract enforced only by convention is not enforced, causing maintainability issues as the amount of source code in a package increases (whether by line count or file count), so should be considered a design smell.

Exemplars include: `postgrest/internal/http` and the `auth/internal` packages (`token`, `key`, `cache`, `profile`, plus `testkit` for shared fixtures).

Highlights of good design:

1. **One concern per package and let naming police that.** If no single plain word names the package then it probably covers multiple concerns, therefore should be split into multiple packages. A coined compound name is usually a design smell.
2. **A contract-carrying type is a one-field wrapper.** `auth.Claims` is `struct{ inner token.Claims }`: the inner type lives in an internal package and its fields are unexported, so only that package can touch them. Public accessors forward to `inner`, carrying the consumer-facing docs and no logic. Contract-free types stay ordinary structs (`auth.Error` keeps exported fields).
3. **Constructors are the only source of values.** The internal package exports them, and the public package never builds or mutates inner values. Reassigning `inner` still compiles - never do it.
4. **Defensive copies live with the fields they protect.** Clone maps and slices in the owning package's accessors, never in forwarders. Document shallow clones.
5. **Policy stays public, machinery goes internal.** Sentinels, error shaping, routing and fallbacks stay in the public package, while HTTP and credentials are injected as closures - the error model is defined once.
6. **Cross boundaries by value.** A shared pointer hands read-only code a mutable alias (the `*url.URL` lesson).
7. **Tests are external by default.** `*_test.go` files are `package x_test`. In-package tests live only in `*_internal_test.go` files, for behavior a boundary genuinely hides. Shared fixtures go in an internal test-support package, never into widened production API.
8. **Know when to stop.** Single-function wire carriers and contract-free types need no boundary. Enforce stated contracts, not ceremony.
