// Command admin-provisioning: a back-office operator tool on the composed
// Supabase client, provisioning, reconciling and offboarding user
// accounts with the project's secret API key. A non-published module outside
// the go.work workspace; the replace directives resolve the SDK modules from
// the local tree.
module github.com/supabase/supabase-go/examples/admin-provisioning

go 1.26

require (
	github.com/supabase/supabase-go/auth v0.1.0-alpha.2
	github.com/supabase/supabase-go/core v0.1.0-alpha.2
	github.com/supabase/supabase-go/postgrest v0.1.0-alpha.1
	github.com/supabase/supabase-go/supabase v0.0.0-00010101000000-000000000000
)

replace (
	github.com/supabase/supabase-go/auth => ../../auth
	github.com/supabase/supabase-go/core => ../../core
	github.com/supabase/supabase-go/postgrest => ../../postgrest
	github.com/supabase/supabase-go/supabase => ../../supabase
)
