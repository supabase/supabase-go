// Fixtures shared by the adjacent integrationtest modules: the local stack's
// credentials and end-user signup. Never published and outside the go.work
// workspace; its consumers resolve it with a replace directive.
module github.com/supabase/supabase-go/integrationsupport

go 1.25
