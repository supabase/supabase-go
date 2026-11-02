// Command sdkchecker analyzes the Go packages its arguments name with this
// repository's own analyzers, reporting each finding on standard error. It
// exits non-zero when it reports a finding or cannot analyze a package.
//
// The repository entry point is ./scripts/lint.sh, which runs it over the
// non-test files of each published module:
//
//	sdkchecker -test=false ./...
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/supabase/supabase-go/tools/go/sdkchecker/unexportedreturn"
)

func main() {
	singlechecker.Main(unexportedreturn.Analyzer)
}
