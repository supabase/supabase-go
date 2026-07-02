// This directory holds npm-managed tooling (cspell), not Go source. This go.mod
// exists only so the repository-root module's `./...` stops here and never
// descends into node_modules: cspell's transitive dependency "flatted" ships a
// Go port (flatted/golang/pkg/flatted/flatted.go) that would otherwise be built,
// tested and linted as if it were ours. It mirrors how tools/go is its own
// module. No Go code lives here and nothing builds this module.
module github.com/supabase/supabase-go/tools/node

go 1.22.0
