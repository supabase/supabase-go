# Changelog

All notable changes to this module will be documented in this file.

This module is one of a number of modules defined in a multi-module repository.
Refer to [GitHub `../CHANGELOG.md`](https://github.com/supabase/supabase-go/blob/main/CHANGELOG.md) for further details on structure, format and sibling modules.

## Unreleased

### Added

Administration surface for the project's users and custom sign-in providers, `Client.Admin`:

- `CreateCustomProvider` (registering a custom OAuth 2.0 or OIDC provider, built with `NewCustomProvider`)
- `CreateUser`
- `DeleteCustomProvider`
- `DeleteMFAFactor`
- `DeleteUser` (including `WithSoftDelete` option)
- `GenerateLink` (minting email action links, built with `NewLinkRequest`)
- `GetCustomProvider`
- `GetUser`
- `InviteUser`
- `ListCustomProviders` (including `WithOIDCOnly` and `WithOAuth2Only` options)
- `ListMFAFactors`
- `ListUsers` (paginated)
- `SignOut` (revoking a user's sessions, including `WithLocalScope` and `WithOthersScope` options)
- `UpdateCustomProvider` (one functional option per changeable field)
- `UpdateMFAFactor` (including `WithFriendlyName` and `WithPhone` options)
- `UpdateUser`

`User` now presents every field the server serves, adding `ConfirmationSentAt`, `EmailChangeSentAt`, `InvitedAt`, `NewEmail`, `NewPhone`, `PhoneChangeSentAt`, `ReauthenticationSentAt` and `RecoverySentAt`, and `Identity` likewise adds `Email`. `User` also carries `Factors`, the enrolled MFA factors, each a wire-complete `Factor`.

## `v0.1.0-alpha.1` (2026-10-01)

Auth client for server-side verification of end-user access tokens. `Client.GetClaims` returns a token's verified claims and `Client.GetUser` fetches the user's current profile from the Auth server. See the [package documentation](https://pkg.go.dev/github.com/supabase/supabase-go/auth).
