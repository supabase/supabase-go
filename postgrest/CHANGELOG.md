# Changelog

All notable changes to this module will be documented in this file.

This module is one of a number of modules defined in a multi-module repository.
Refer to [GitHub `../CHANGELOG.md`](https://github.com/supabase/supabase-go/blob/main/CHANGELOG.md) for further details on structure, format and sibling modules.

## `v0.1.0-alpha.2` (2026-11-09)

### Added

The sentinel a singular request's refusal stands for, wrapped by the `Error` that answers it:

- `ErrNoRows` and `ErrTooManyRows`, from `CollectSingle`
- `ErrTooManyRows`, from a write through `CollectSingleMaybe`

### Fixed

A response body that cannot be read in full or closed fails the call, where before a failed close was ignored and a body that could not be drained was retried regardless:

- `Collect`, `CollectSingle`, `CollectSingleMaybe`, `CollectRaw` and `Execute`

Aligns `CollectSingleMaybe` with the [cross-SDK at-most-one-row spec](https://github.com/supabase/sdk/blob/fe473e180d7d579a012e9da6be7c4b16d7a78428/packages/capability-matrix/specs/database/using_modifiers/maybe_single_row.md), where before it counted every result on the client, so a write that matched more than one row changed every one before failing:

- a write through `CollectSingleMaybe` is counted by the server inside its transaction, so one that would affect more than one row changes nothing, and one that affects no row returns a `Response` carrying the server's 406
- a read is counted from the `Content-Range` header

## `v0.1.0-alpha.1` (2026-10-01)

Database client with a chained query builder over PostgREST, for filtered, ordered and paginated reads, writes (insert, upsert, update and delete) and Postgres function calls. An access-token provider runs queries as a signed-in user, so that user's Row Level Security policies apply. See the [package documentation](https://pkg.go.dev/github.com/supabase/supabase-go/postgrest).
