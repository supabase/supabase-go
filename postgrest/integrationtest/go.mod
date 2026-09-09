// Integration tests for the postgrest module. A non-published module outside
// the go.work workspace, so the published postgrest go.mod stays free of
// test-only requirements; scripts run it with GOWORK=off and the replace
// directives below resolve every requirement from the local tree.
module github.com/supabase/supabase-go/postgrest/integrationtest

go 1.25

require (
	github.com/supabase/supabase-go/core v0.0.0-00010101000000-000000000000
	github.com/supabase/supabase-go/integrationsupport v0.0.0-00010101000000-000000000000
	github.com/supabase/supabase-go/postgrest v0.0.0-00010101000000-000000000000
)

replace (
	github.com/supabase/supabase-go/core => ../../core
	github.com/supabase/supabase-go/integrationsupport => ../../integrationsupport
	github.com/supabase/supabase-go/postgrest => ../../postgrest
)
