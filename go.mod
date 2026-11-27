module github.com/supabase/supabase-go

// This module's consumer compatibility floor - the minimum Go version required
// to use it. It is a minimum, not the toolchain we build with, and it tracks
// the oldest Go major release still supported by the Go project (see
// "Supported Go versions" in the repository README).
go 1.26

// This module's directory is the repository root, so its package patterns
// (./...) reach every directory that is not a nested module. These two hold
// Go files that are not ours to build or publish: npm tooling under
// tools/node ships third-party Go sources (cspell's flatted dependency) and
// user.transient is developer scratch space, per .gitignore.
ignore (
	node_modules
	user.transient
)

require (
	github.com/supabase/supabase-go/auth v0.1.0-alpha.6
	github.com/supabase/supabase-go/core v0.1.0-alpha.5
	github.com/supabase/supabase-go/postgrest v0.1.0-alpha.4
)
