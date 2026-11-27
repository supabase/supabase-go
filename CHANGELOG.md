# Changelog

All notable changes to this module will be documented in this file.

This module is the root of a multi-module repository.
The other published modules, which [`go.work`](go.work) lists, each keep a changelog of their own: [`auth`](auth/CHANGELOG.md), [`core`](core/CHANGELOG.md) and [`postgrest`](postgrest/CHANGELOG.md).
Refer to [`DEVELOPMENT.md`](DEVELOPMENT.md#changelogs) for the structure and format every module's changelog follows.

## Unreleased

### Changed

- Upgrades `auth` from `v0.1.0-alpha.5` to `v0.1.0-alpha.6`.
- Upgrades `core` from `v0.1.0-alpha.4` to `v0.1.0-alpha.5`.
- Upgrades `postgrest` from `v0.1.0-alpha.3` to `v0.1.0-alpha.4`.

## `v0.1.0-alpha.6` (2026-11-24)

### Changed

- Upgrades `postgrest` from `v0.1.0-alpha.2` to `v0.1.0-alpha.3`.

## `v0.1.0-alpha.5` (2026-11-16)

### Changed

Relocates the module to the repository root: it is imported as `github.com/supabase/supabase-go`, no longer as `github.com/supabase/supabase-go/supabase`, and its release tags carry the bare version. The releases below, headed by their `supabase/` tags, were made at the old import path and remain published there.

## `supabase/v0.1.0-alpha.5` (2026-11-17)

### Deprecated

Deprecates this module in favor of `github.com/supabase/supabase-go`, the `supabase` root package's new path, where its releases will continue.

The code is unchanged from the previous release, but `go get` and pkg.go.dev now warn that the module is deprecated.

To move, change your import from `github.com/supabase/supabase-go/supabase` to `github.com/supabase/supabase-go` and run `go mod tidy`. The package is still named `supabase`, so the rest of your code stays the same.

## `supabase/v0.1.0-alpha.4` (2026-11-09)

### Changed

- Upgrades `auth` from `v0.1.0-alpha.3` to `v0.1.0-alpha.4`.
- Upgrades `core` from `v0.1.0-alpha.2` to `v0.1.0-alpha.3`.
- Upgrades `postgrest` from `v0.1.0-alpha.1` to `v0.1.0-alpha.2`.

## `supabase/v0.1.0-alpha.3` (2026-11-05)

### Changed

- Upgrades `auth` from `v0.1.0-alpha.2` to `v0.1.0-alpha.3`.

## `supabase/v0.1.0-alpha.2` (2026-10-15)

### Changed

- Upgrades `auth` from `v0.1.0-alpha.1` to `v0.1.0-alpha.2`.
- Upgrades `core` from `v0.1.0-alpha.1` to `v0.1.0-alpha.2`.

## `supabase/v0.1.0-alpha.1` (2026-10-01)

Convenience root client that composes the SDK's domain clients behind one configuration, with the Database client at `Client.Database` and the Auth client at `Client.Auth`. See the [package documentation](https://pkg.go.dev/github.com/supabase/supabase-go/supabase).
