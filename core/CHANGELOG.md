# Changelog

All notable changes to this module will be documented in this file.

This module is one of a number of modules defined in a multi-module repository.
Refer to [GitHub `../CHANGELOG.md`](https://github.com/supabase/supabase-go/blob/main/CHANGELOG.md) for further details on structure, format and sibling modules.

<!-- cSpell:ignore freebsd -->

## Unreleased

### Fixed

Aligns the `X-Client-Info` header every client sends with the [cross-SDK client info spec](https://github.com/supabase/sdk/blob/2d2e0ed57ff5a4846af84cd3400fd6f7b8109ba1/packages/capability-matrix/specs/client/observability/client_info.md):

- an `X-Client-Info` header set through `WithHeader` now replaces the SDK's value, where before the SDK's value replaced it
- a build in which the SDK's version is not recorded, such as one from a workspace, sends version `0.0.0`, where before it sent `(devel)`, which Supabase's usage reporting cannot attribute to the SDK

## `v0.1.0-alpha.3` (2026-11-09)

### Added

`Configuration.Logger`, returning the configured logger.

## `v0.1.0-alpha.2` (2026-10-15)

### Added

Pagination options for the SDK's listing calls, package `pagination`:

- `Option`s `WithPage` and `WithSize`
- `Parameters` via `Resolve`

## `v0.1.0-alpha.1` (2026-10-01)

Shared configuration for the SDK's domain clients, with options for a custom HTTP client, global headers, automatic retries and debug logging through `log/slog`. The module also defines the HTTP error type that domain errors embed. See the [`configuration`](https://pkg.go.dev/github.com/supabase/supabase-go/core/configuration) and [`responses`](https://pkg.go.dev/github.com/supabase/supabase-go/core/responses) package documentation.
