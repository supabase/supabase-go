// Package supabase is the convenience entry point to the Supabase Go SDK. It
// composes the SDK's domain modules behind a single configured client. The
// [Database] client is reached through [Client.Database] and the [Auth] client
// through [Client.Auth].
//
// There are two ways to use the SDK. Importing this package wires the shared
// transport, API key, and options through one place. Callers who need only one
// domain may instead import that module directly (for example
// [github.com/supabase/supabase-go/postgrest]) and pull in only it and the
// core module's [github.com/supabase/supabase-go/core/configuration] package.
//
// The SDK repository's [example programs] include an HTTP backend that
// verifies each request's token, then queries the database as that user.
//
// [Database]: https://supabase.com/docs/guides/database/overview
// [Auth]: https://supabase.com/docs/guides/auth
// [example programs]: https://github.com/supabase/supabase-go/tree/main/examples
package supabase
