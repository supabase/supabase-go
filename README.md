# Supabase Go SDK

The official Supabase SDK for Go. This is a multi-module monorepo, currently in early pre-release development.

> [!CAUTION]
> Status: pre-Alpha. The public surface is being established and is not yet tagged for general use.

## Modules

| `github.com/supabase/supabase-go` module | Purpose |
| ------ | ------- |
| [`core`](core/) | Shared configuration, functional options and the HTTP pipeline |
| [`auth`](auth/) | Auth client for server-side JWT verification |
| [`postgrest`](postgrest/) | Database (PostgREST) client |
| [`supabase`](supabase/) | Convenience root client composing the domains |

## Quick start

Add the SDK to your module:

```bash
go get github.com/supabase/supabase-go/supabase
```

Construct a client with your project URL and API key:

```go
client, err := supabase.New("https://PROJECT_ID.supabase.co", "sb_publishable_...")
```

Two walkthroughs build complete Go APIs against a local Supabase stack:

- The [RLS Walkthrough](docs/RLS%20Walkthrough.md) verifies each user's access token, then serves only that user's rows under Row Level Security.
- The [Premium Access Walkthrough](docs/Premium%20Access%20Walkthrough.md) checks each user with Auth on every request, so a plan change or a sign-out takes effect straight away. It uses the `auth` module on its own.

The package documentation on [pkg.go.dev](https://pkg.go.dev/github.com/supabase/supabase-go/supabase) covers the rest of the API.

## Supported Go versions

We support the two most recent major versions of Go, aligning with the Go project's [Release Policy](https://go.dev/doc/devel/release#policy).

The oldest Go version we support is referred to as our "consumer floor" and can be found in each module's `go` directive (see `go.mod` files, for example [`supabase/go.mod`](supabase/go.mod)), raised opportunistically per-module after each new Go release.

## Security

This SDK is built for backend processes that hold privileged credentials, with its security posture enforced by design:

- **Zero external runtime dependencies**: the published modules require only each other, so consumers inherit no third-party code from us.
- **Vulnerability scanning is continuous and scheduled**: `govulncheck` runs on every push and pull request and daily on a schedule.
- **Credentials never enter logs**: silent by default, and an injected logger gets one debug record per request round trip - bodies, query strings and header values are never logged.
- **Keys live only in memory**: API keys and end-user tokens are held in client configuration for the client's lifetime and never persisted.
- **No Cgo**: pure Go throughout, with no memory-unsafe boundary of our own.
- **Token verification is local-first**: the `auth` module verifies end-user JWTs against the project's published signing keys, deferring to the Auth server only for tokens the key set cannot verify.

To report a vulnerability, see our organization-wide [security policy](https://github.com/supabase/.github/blob/main/SECURITY.md).

## Contributing and development

This SDK is in pre-release and is not yet accepting external code contributions (see [`DEVELOPMENT.md`](DEVELOPMENT.md)).

Also see [`standard.md`](standard.md) for our definition of "what good looks like" for a Go SDK.

## License

This SDK is licensed under the MIT License - see [`LICENSE`](LICENSE).
