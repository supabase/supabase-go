package pagination_test

import (
	"testing"

	"github.com/supabase/supabase-go/core/pagination"
)

func TestResolveWithoutOptionsHoldsNothing(t *testing.T) {
	parameters := pagination.Resolve()
	if number, ok := parameters.Page(); ok || number != 0 {
		t.Errorf("Page() = (%d, %t), want (0, false)", number, ok)
	}
	if items, ok := parameters.Size(); ok || items != 0 {
		t.Errorf("Size() = (%d, %t), want (0, false)", items, ok)
	}
}

func TestResolveSetsExactlyWhatOptionsChose(t *testing.T) {
	t.Run("WithPage alone leaves the size absent", func(t *testing.T) {
		parameters := pagination.Resolve(pagination.WithPage(2))
		if number, ok := parameters.Page(); !ok || number != 2 {
			t.Errorf("Page() = (%d, %t), want (2, true)", number, ok)
		}
		if items, ok := parameters.Size(); ok || items != 0 {
			t.Errorf("Size() = (%d, %t), want (0, false)", items, ok)
		}
	})

	t.Run("WithSize alone leaves the page absent", func(t *testing.T) {
		parameters := pagination.Resolve(pagination.WithSize(50))
		if items, ok := parameters.Size(); !ok || items != 50 {
			t.Errorf("Size() = (%d, %t), want (50, true)", items, ok)
		}
		if number, ok := parameters.Page(); ok || number != 0 {
			t.Errorf("Page() = (%d, %t), want (0, false)", number, ok)
		}
	})

	t.Run("together each parameter carries its own value", func(t *testing.T) {
		parameters := pagination.Resolve(pagination.WithPage(3), pagination.WithSize(25))
		if number, ok := parameters.Page(); !ok || number != 3 {
			t.Errorf("Page() = (%d, %t), want (3, true)", number, ok)
		}
		if items, ok := parameters.Size(); !ok || items != 25 {
			t.Errorf("Size() = (%d, %t), want (25, true)", items, ok)
		}
	})

	t.Run("the last repeated option wins", func(t *testing.T) {
		parameters := pagination.Resolve(pagination.WithPage(1), pagination.WithPage(4))
		if number, ok := parameters.Page(); !ok || number != 4 {
			t.Errorf("Page() = (%d, %t), want (4, true)", number, ok)
		}
	})
}
