// A stand-in consumer of the SDK. This module lies outside the repository
// workspace and the SDK's module tree, so a binary built from it resolves SDK
// client versions from build-information dependency records the way every
// consumer build does.
module telemetrytest

go 1.26

// Fabricated, self-labeled versions that are never published - the replace
// block resolves them to the local working tree. Each must outrank every other
// require of the same module path in this build, or the higher one becomes the
// selected version that build information records and main.go asserts.
require (
	github.com/supabase/supabase-go/postgrest v1.999.2-fabricated
	github.com/supabase/supabase-go/supabase v1.999.1-fabricated
)

require (
	github.com/supabase/supabase-go/auth v0.0.0-00010101000000-000000000000 // indirect
	github.com/supabase/supabase-go/core v0.1.0-alpha.1 // indirect
)

replace (
	github.com/supabase/supabase-go/auth => ../auth
	github.com/supabase/supabase-go/core => ../core
	github.com/supabase/supabase-go/postgrest => ../postgrest
	github.com/supabase/supabase-go/supabase => ../supabase
)
