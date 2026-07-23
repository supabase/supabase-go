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
package postgrest
