# Supabase Go SDK

The official Supabase SDK for Go. This is a multi-module monorepo, currently in early pre-release development.

> [!CAUTION]
> Status: pre-Alpha. The public surface is being established and is not yet tagged for general use.

## Modules

| `github.com/supabase/supabase-go` module | Purpose |
| ------ | ------- |
| [Root](./) | Convenience root client composing the domains |
| [`core`](core/) | Shared configuration, functional options and the HTTP pipeline |
| [`postgrest`](postgrest/) | Database (PostgREST) client |

## Supported Go versions

This SDK supports the Go releases that the Go project itself supports: the two most recent major versions. Each module's `go` directive - the minimum Go version required to consume the SDK - tracks the older of those two majors and is raised opportunistically after each new Go release.

## Contributing and development

This SDK is in pre-release and is not yet accepting external code contributions (see [`DEVELOPMENT.md`](DEVELOPMENT.md)).

Also see [`standard.md`](standard.md) for our definition of "what good looks like" for a Go SDK.
