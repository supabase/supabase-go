// Integration tests for the supabase module, composing it with the domain
// modules the way a consumer backend does. A non-published module outside the
// go.work workspace, so the published supabase go.mod stays free of test-only
// requirements; scripts run it with GOWORK=off and the replace directives
// below resolve every requirement from the local tree.
module github.com/supabase/supabase-go/supabase/integrationtest

go 1.26

require (
	github.com/supabase/supabase-go/integration-testing/testkit v0.0.0-00010101000000-000000000000
	github.com/supabase/supabase-go/postgrest v0.1.0-alpha.1
	github.com/supabase/supabase-go/supabase v0.0.0-00010101000000-000000000000
)

require (
	github.com/supabase/supabase-go/auth v0.1.0-alpha.1 // indirect
	github.com/supabase/supabase-go/core v0.1.0-alpha.1 // indirect
)

replace (
	github.com/supabase/supabase-go/auth => ../../auth
	github.com/supabase/supabase-go/core => ../../core
	github.com/supabase/supabase-go/integration-testing/testkit => ../../integration-testing/testkit
	github.com/supabase/supabase-go/postgrest => ../../postgrest
	github.com/supabase/supabase-go/supabase => ../../supabase
)
