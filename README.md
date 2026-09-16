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

## Supported Go versions

We support the two most recent major versions of Go, aligning with the Go project's [Release Policy](https://go.dev/doc/devel/release#policy).

The oldest Go version we support is referred to as our "consumer floor" and can be found in each module's `go` directive (see `go.mod` files, for example [`supabase/go.mod`](supabase/go.mod)), raised opportunistically per-module after each new Go release.

## Contributing and development

This SDK is in pre-release and is not yet accepting external code contributions (see [`DEVELOPMENT.md`](DEVELOPMENT.md)).

Also see [`standard.md`](standard.md) for our definition of "what good looks like" for a Go SDK.

## License

This SDK is licensed under the MIT License - see [`LICENSE`](LICENSE).
