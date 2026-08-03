// Package postgrest is the Supabase Database client: a chained query builder
// over PostgREST. A context-taking free generic function such as [Collect]
// executes the finished query through a [Client].
//
// A query starts at [From], chains through an immutable builder and executes
// with the row type it decodes into:
//
//	rows, response, err := postgrest.Collect[Instrument](
//	    ctx,
//	    client,
//	    postgrest.
//	        From("instruments").
//	        Select("id, name"))
//
// Builders are pure immutable values carrying no client reference: every step
// returns a new independent builder, so queries may be declared at package
// level before any client exists, stored, forked, and shared across
// goroutines. The client - and with it the HTTP connection pool - enters only
// at the executing read function.
package postgrest
