// Package postgrest is the Supabase Database client: a chained, generic query
// builder over PostgREST that terminates in a context-taking Execute.
//
// A query starts at [Client.From], chains through an immutable builder and
// executes with a destination to decode into:
//
//	var rows []Instrument
//	response, err := client.From("instruments").Select("id, name").Execute(ctx, &rows)
//
// Builders are immutable values: every step returns a new independent builder,
// so partially-built queries may be stored, forked and shared across
// goroutines.
package postgrest
