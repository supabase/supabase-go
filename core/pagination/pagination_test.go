package pagination_test

import (
	"testing"

	"github.com/supabase/supabase-go/core/pagination"
)

func TestResolve(t *testing.T) {
	cases := []struct {
		name       string
		options    []pagination.Option
		wantPage   int
		wantPageOK bool
		wantSize   int
		wantSizeOK bool
	}{
		{name: "no options holds nothing"},
		{name: "WithPage alone leaves the size absent", options: []pagination.Option{pagination.WithPage(2)}, wantPage: 2, wantPageOK: true},
		{name: "WithSize alone leaves the page absent", options: []pagination.Option{pagination.WithSize(50)}, wantSize: 50, wantSizeOK: true},
		{name: "together each parameter carries its own value", options: []pagination.Option{pagination.WithPage(3), pagination.WithSize(25)}, wantPage: 3, wantPageOK: true, wantSize: 25, wantSizeOK: true},
		{name: "the last repeated option wins", options: []pagination.Option{pagination.WithPage(1), pagination.WithPage(4)}, wantPage: 4, wantPageOK: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			parameters := pagination.Resolve(testCase.options...)
			if number, ok := parameters.Page(); ok != testCase.wantPageOK || number != testCase.wantPage {
				t.Errorf("Page() = (%d, %t), want (%d, %t)", number, ok, testCase.wantPage, testCase.wantPageOK)
			}
			if items, ok := parameters.Size(); ok != testCase.wantSizeOK || items != testCase.wantSize {
				t.Errorf("Size() = (%d, %t), want (%d, %t)", items, ok, testCase.wantSize, testCase.wantSizeOK)
			}
		})
	}
}
