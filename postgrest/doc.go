// Package postgrest is the Supabase Database client: a chained query builder
// over PostgREST. A context-taking free generic function such as [Collect]
// executes the finished query through a [Client].
//
// A query names the row type it decodes into at [From], chains through an
// immutable builder and executes. [From] alone is a complete query for
// every column; [QueryBuilder.Select] narrows the projection:
//
//	rows, response, err := postgrest.Collect(
//	    ctx,
//	    client,
//	    postgrest.
//	        From[Instrument]("instruments").
//	        Select("id, name"))
//
// Between [From] and execution the chain narrows and shapes the read: filter
// methods named for PostgREST's operators - [FilterBuilder.Eq],
// [FilterBuilder.In], [FilterBuilder.TextSearch] and their kin - choose the
// rows, combining with AND, while [FilterBuilder.Order], [FilterBuilder.Limit]
// and [FilterBuilder.Range] shape the result.
//
// Writes mirror reads. [QueryBuilder.Insert] creates rows and
// [QueryBuilder.Upsert] creates them or merges them on conflict, while
// [FilterBuilder.Update] and [FilterBuilder.Delete] end a filtered chain by
// changing or removing the rows the preceding filters chose - the same filters
// that scope a read. [Execute] applies a write without reading anything back:
//
//	response, err := postgrest.Execute(
//	    ctx,
//	    client,
//	    postgrest.From[Instrument]("instruments").Insert(Instrument{Name: "viola"}))
//
// To read the affected rows back in the same call, pass the write to
// [Collect] instead of [Execute]: the server returns the rows, and the write
// builder's Returning method narrows which columns they carry.
//
// Builders are pure immutable values carrying no client reference: every step
// returns a new independent builder, so queries may be declared at package
// level before any client exists, stored, forked, and shared across
// goroutines. The client - and with it the HTTP connection pool - enters only
// at the executing read function.
//
// Collect with a named struct is the recommended default. Consumers that
// cannot name row types at compile time - schema-driven admin tooling,
// proxies, migration utilities - instantiate Collect with a dynamic
// container instead: Collect[map[string]any] decodes rows into generic
// maps, and Collect[json.RawMessage] defers per-row decoding entirely.
package postgrest
