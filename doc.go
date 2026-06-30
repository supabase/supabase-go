// Package supabase is the convenience entry point to the Supabase Go SDK. It
// composes the domain modules (Database now; Auth, Storage and Edge Functions in
// later blocks) behind a single configured client.
//
// Two doors are supported. Importing this package is the convenience path: it
// wires the shared transport, API key and options through one place. Callers who
// need only one domain may instead import that module directly (for example
// github.com/supabase/supabase-go/postgrest) and pull in only it and core.
package supabase
