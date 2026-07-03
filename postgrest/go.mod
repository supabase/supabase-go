module github.com/supabase/supabase-go/postgrest

// 1.22 is this module's consumer compatibility floor - the minimum Go version
// required to use it. It is a minimum, not the toolchain we build with, held
// conservatively so adopting our SDK doesn't force a user to upgrade Go unless
// their Go predates this baseline.
go 1.22

require github.com/supabase/supabase-go/configuration v0.0.0-00010101000000-000000000000
