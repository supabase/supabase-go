# Changelog

All notable changes to this module will be documented in this file.

This module is one of a number of modules defined in a multi-module repository.
Refer to [GitHub `../CHANGELOG.md`](https://github.com/supabase/supabase-go/blob/main/CHANGELOG.md) for further details on structure, format and sibling modules.

## Unreleased

### Changed

Call-scoped functional options moved from the package root into per-family packages, each owning its `Option` type and constructors:

- `auth/userinvite` serves `Admin.InviteUser` (was `InviteUserOption` with `WithUserMetadata` and `WithRedirectTo`)
- `auth/signout` serves `Admin.SignOut` (was `SignOutOption` with `WithLocalScope` and `WithOthersScope`)
- `auth/userdelete` serves `Admin.DeleteUser` (was `DeleteUserOption` with `WithSoftDelete`)
- `auth/mfafactor` serves `Admin.UpdateMFAFactor` (was `MFAFactorOption` with `WithFriendlyName` and `WithPhone`)
- `auth/customprovider` serves the custom provider calls: update options as `Option` (was `CustomProviderOption` with its eighteen constructors), listing filters as `ListOption` (was `ListCustomProvidersOption` with `WithOIDCOnly` and `WithOAuth2Only`) and the creation builder as `New`, `Builder`, `OIDCDefinition`, `OAuth2Definition` and `Definition` (were `NewCustomProvider`, `CustomProviderBuilder`, `OIDCProviderDefinition`, `OAuth2ProviderDefinition` and `CustomProviderDefinition`)

Every operation now returns exactly one result value plus `error`, the result type named `<Operation>Result`, so future response data arrives as new accessors rather than as changed signatures:

- `GetClaims` folds its four-value tuple into `*GetClaimsResult`, carrying `Claims()`, `Header()` and `Signature()`
- `Client.GetUser` and `Admin.GetUser` answer `*GetUserResult` (were `*User`)
- `CreateUser`, `UpdateUser` and `InviteUser` answer `*CreateUserResult`, `*UpdateUserResult` and `*InviteUserResult` (were `*User`)
- `UserPage` is renamed `ListUsersResult` and `GeneratedLink` is renamed `GenerateLinkResult`, their accessors unchanged
- `ListMFAFactors` answers `*ListMFAFactorsResult` (was `[]Factor`) and `UpdateMFAFactor` answers `*UpdateMFAFactorResult` (was `*Factor`)
- the custom provider operations answer `*CreateCustomProviderResult`, `*GetCustomProviderResult`, `*ListCustomProvidersResult` and `*UpdateCustomProviderResult` (were `*CustomProvider` and `[]CustomProvider`)
- the bodiless operations `DeleteUser`, `SignOut`, `DeleteMFAFactor` and `DeleteCustomProvider` answer empty results that carry nothing, allowing for future population if the protocol changes to provide result detail (were bare `error`)

## `v0.1.0-alpha.2` (2026-10-15)

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
