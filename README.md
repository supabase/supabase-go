# Supabase Go SDK

The official Supabase SDK for Go. This is a multi-module monorepo, currently in early pre-release development.

> [!CAUTION]
> Status: pre-Alpha. The public surface is being established and is not yet tagged for general use.

## Modules

| `github.com/supabase/supabase-go` module | Purpose |
| ------ | ------- |
| [Root](./) | Convenience root client composing the domains |
| [`core`](./core/) | Shared HTTP pipeline, configuration and options |
| [`postgrest`](./postgrest/) | Database (PostgREST) client |

## Contributing and development

This SDK is in pre-release and is not yet accepting external code contributions (see [`DEVELOPMENT.md`](./DEVELOPMENT.md)).
