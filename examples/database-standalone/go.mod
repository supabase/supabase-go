// The scoped door into the SDK: query the Database through the postgrest
// module alone, with no root client import, so the consumer's module graph
// carries only postgrest and core. A non-published module outside the go.work
// workspace; the replace directives resolve the SDK modules from the local
// tree.
module github.com/supabase/supabase-go/examples/database-standalone

go 1.25

require github.com/supabase/supabase-go/postgrest v0.0.0-00010101000000-000000000000

require github.com/supabase/supabase-go/core v0.0.0-00010101000000-000000000000 // indirect

replace (
	github.com/supabase/supabase-go/core => ../../core
	github.com/supabase/supabase-go/postgrest => ../../postgrest
)
