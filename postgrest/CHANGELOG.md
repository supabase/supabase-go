# Changelog

All notable changes to this module will be documented in this file.

This module is one of a number of modules defined in a multi-module repository.
Refer to [GitHub `../DEVELOPMENT.md`](https://github.com/supabase/supabase-go/blob/main/DEVELOPMENT.md#changelogs) for further details on structure and format, and to [GitHub `../CHANGELOG.md`](https://github.com/supabase/supabase-go/blob/main/CHANGELOG.md) for sibling modules.

## `v0.1.0-alpha.4` (2026-11-27)

### Fixed

Aligns our implementation of `CollectSingleMaybe` with the [cross-SDK at-most-one-row spec](https://github.com/supabase/sdk/blob/16c8713d21a14625dfeac6c7afa558399a646be4/packages/capability-matrix/specs/database/using_modifiers/maybe_single_row.md) for the `database.using_modifiers.maybe_single_row` capability:

- the `Response` of a write that affects no row now reports HTTP status 200, where before it carried the server's 406
- a function's single array value, which the server counts as one row, now decodes whole into a slice `Row` such as `[]int`, where before the call failed with a decoding error

Automatic retries of a read:  
A retry now waits its 1s, 2s or 4s backoff when the response's `Retry-After` header asks for zero seconds, where before it went out at once.
PostgREST sends that header with every 503, so such a read spent its three retries with no wait between them.

## `v0.1.0-alpha.3` (2026-11-24)

### Fixed

`RPCRowsCall.ReadOnly` and `RPCValueCall.ReadOnly`:  
An array argument of a read-only function call now reaches the function with the elements it was given.
Before this fix an element holding a comma, brace, double quote or backslash, or one that was empty, padded with whitespace or spelled `NULL`, was split, altered or rejected.

`FilterBuilder` methods `ContainsAll`, `ContainedBy`, `OverlapsAny`, `LikeAll`, `LikeAny`, `ILikeAll` and `ILikeAny`:  
An element spelling `NULL` in any letter case now arrives as that text.
Before this fix Postgres read it as a null, which matches nothing.

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
