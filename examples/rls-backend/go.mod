// The backend the RLS walkthrough builds: verify an inbound end-user access
// token, then serve that user's own rows under Row Level Security, through one
// configured root client. A non-published module outside the go.work
// workspace; the replace directives resolve the SDK modules from the local
// tree.
module github.com/supabase/supabase-go/examples/rls-backend

go 1.26

require (
	github.com/supabase/supabase-go/postgrest v0.1.0-alpha.2
	github.com/supabase/supabase-go/supabase v0.1.0-alpha.4
)

require (
	github.com/supabase/supabase-go/auth v0.1.0-alpha.4 // indirect
	github.com/supabase/supabase-go/core v0.1.0-alpha.3 // indirect
)

replace (
	github.com/supabase/supabase-go/auth => ../../auth
	github.com/supabase/supabase-go/core => ../../core
	github.com/supabase/supabase-go/postgrest => ../../postgrest
	github.com/supabase/supabase-go/supabase => ../../supabase
)
