// The composed Supabase Go SDK backend: verify an inbound end-user access
// token, then serve that user's own rows under Row Level Security, through one
// configured root client. A non-published module outside the go.work
// workspace; the replace directives resolve the SDK modules from the local
// tree.
module github.com/supabase/supabase-go/examples/rls-backend

go 1.25

require (
	github.com/supabase/supabase-go/auth v0.0.0-00010101000000-000000000000
	github.com/supabase/supabase-go/postgrest v0.0.0-00010101000000-000000000000
	github.com/supabase/supabase-go/supabase v0.0.0-00010101000000-000000000000
)

require github.com/supabase/supabase-go/core v0.0.0-00010101000000-000000000000 // indirect

replace (
	github.com/supabase/supabase-go/auth => ../../auth
	github.com/supabase/supabase-go/core => ../../core
	github.com/supabase/supabase-go/postgrest => ../../postgrest
	github.com/supabase/supabase-go/supabase => ../../supabase
)
