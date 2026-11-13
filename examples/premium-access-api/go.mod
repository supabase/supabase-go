// The API the Premium Access walkthrough builds: check each request's user
// with Auth, then serve backing tracks only to users on the premium plan. A
// non-published module outside the go.work workspace, whose replace
// directives resolve the SDK modules from the local tree.
module github.com/supabase/supabase-go/examples/premium-access-api

go 1.26

require github.com/supabase/supabase-go/auth v0.1.0-alpha.4

require github.com/supabase/supabase-go/core v0.1.0-alpha.3 // indirect

replace (
	github.com/supabase/supabase-go/auth => ../../auth
	github.com/supabase/supabase-go/core => ../../core
)
