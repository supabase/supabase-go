# Changelog

All notable changes to this module will be documented in this file.

This module is one of a number of modules defined in a multi-module repository.
Refer to [GitHub `../CHANGELOG.md`](https://github.com/supabase/supabase-go/blob/main/CHANGELOG.md) for further details on structure, format and sibling modules.

## Unreleased

Administration surface for the project's users, behind a secret API key: `Client.Admin` reaches `CreateUser`, `GetUser` and `DeleteUser` (soft rather than hard with the `WithSoftDelete` option), with users described by `UserAttributes` and returned as the same `User` the session calls return. A malformed user id is rejected before the wire with `ErrInvalidUserID`.

## `v0.1.0-alpha.1` (2026-10-01)

Auth client for server-side verification of end-user access tokens. `Client.GetClaims` returns a token's verified claims and `Client.GetUser` fetches the user's current profile from the Auth server. See the [package documentation](https://pkg.go.dev/github.com/supabase/supabase-go/auth).
