// Integration tests for the auth module. A non-published module outside the
// go.work workspace, so the published auth go.mod stays free of test-only
// requirements; scripts run it with GOWORK=off and the replace directives
// below resolve every requirement from the local tree.
module github.com/supabase/supabase-go/auth/integrationtest

go 1.25

require (
	github.com/supabase/supabase-go/auth v0.0.0-00010101000000-000000000000
	github.com/supabase/supabase-go/integration-testing/testkit v0.0.0-00010101000000-000000000000
)

require github.com/supabase/supabase-go/core v0.0.0-00010101000000-000000000000 // indirect

replace (
	github.com/supabase/supabase-go/auth => ../../auth
	github.com/supabase/supabase-go/core => ../../core
	github.com/supabase/supabase-go/integration-testing/testkit => ../../integration-testing/testkit
)
