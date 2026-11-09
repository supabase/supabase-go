// Command auth-session-service: a long-running service signed in as one user,
// keeping its session fresh with AutoRefresh and logging its session events
// until it is stopped. A non-published module outside the go.work workspace;
// the replace directives resolve the SDK modules from the local tree.
module github.com/supabase/supabase-go/examples/auth-session-service

go 1.26.0

require (
	github.com/supabase/supabase-go/auth v0.1.0-alpha.4
	golang.org/x/sync v0.24.0
)

require github.com/supabase/supabase-go/core v0.1.0-alpha.3 // indirect

replace (
	github.com/supabase/supabase-go/auth => ../../auth
	github.com/supabase/supabase-go/core => ../../core
)
