package unexportedreturn_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/supabase/supabase-go/tools/go/sdkchecker/unexportedreturn"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), unexportedreturn.Analyzer, "returns", "command")
}
