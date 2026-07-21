// Package supabase is the convenience entry point to the Supabase Go SDK. It
// composes the SDK's domain modules behind a single configured client. The
// Database client is reached through [Client.From].
//
// There are two ways to use the SDK. Importing this package wires the shared
// transport, API key, and options through one place. Callers who need only one
// domain may instead import that module directly (for example
// [github.com/supabase/supabase-go/postgrest]) and pull in only it and the
// core module's [github.com/supabase/supabase-go/core/configuration] package.
package supabase
