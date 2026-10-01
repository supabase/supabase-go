module github.com/supabase/supabase-go/supabase

// This module's consumer compatibility floor - the minimum Go version required
// to use it. It is a minimum, not the toolchain we build with, and it tracks
// the oldest Go major release still supported by the Go project (see
// "Supported Go versions" in the repository README).
go 1.26

require (
	github.com/supabase/supabase-go/auth v0.1.0-alpha.1
	github.com/supabase/supabase-go/core v0.1.0-alpha.1
	github.com/supabase/supabase-go/postgrest v0.1.0-alpha.1
)
