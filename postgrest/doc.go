// Package postgrest is the Supabase Database client: a chained query builder
// over PostgREST that terminates in a context-taking generic terminal such as
// [Collect].
//
// A query starts at [Client.From], chains through an immutable builder and
// executes with the row type it decodes into:
//
//		rows, response, err := postgrest.Collect[Instrument](
//	     ctx,
//	     client.
//	         From("instruments").
//	         Select("id, name"))
//
// Builders are immutable values: every step returns a new independent builder,
// so partially-built queries may be stored, forked, and shared across
// goroutines.
//
// Collect with a named struct is the recommended default. Consumers that
// cannot name row types at compile time - schema-driven admin tooling,
// proxies, migration utilities - instantiate the same terminal with a dynamic
// container instead: Collect[map[string]any] decodes rows into generic maps,
// and Collect[json.RawMessage] defers per-row decoding entirely.
package postgrest
