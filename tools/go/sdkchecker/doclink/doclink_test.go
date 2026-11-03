package doclink_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/supabase/supabase-go/tools/go/sdkchecker/doclink"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), doclink.Analyzer, "links", "clash")
}
