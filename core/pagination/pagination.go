// Package pagination carries the page-numbered pagination vocabulary the
// SDK's listing calls take: a page number and a page size, expressed as
// functional options. A call made with no options sends no pagination
// parameters at all, leaving the server's documented defaults in charge.
//
// The vocabulary is scoped to page-numbered listings. Windowing by item
// position is a different dialect with its own words.
package pagination

// Parameters is the resolved set of pagination parameters for one listing
// call. The zero value holds none. Each parameter is read with its comma-ok
// accessor, so a parameter no option set is absent rather than zero, and a
// module sends only what is present.
type Parameters struct {
	page    int
	size    int
	hasPage bool
	hasSize bool
}

// Option sets one pagination parameter on a listing call. Options are
// applied in the order they are supplied, the last setting of a parameter
// winning.
type Option func(*Parameters)

// Resolve applies options to an empty Parameters and returns the result.
func Resolve(options ...Option) Parameters {
	var parameters Parameters
	for _, option := range options {
		option(&parameters)
	}
	return parameters
}

// WithPage selects which page to fetch, numbered from 1.
func WithPage(number int) Option {
	return func(parameters *Parameters) {
		parameters.page = number
		parameters.hasPage = true
	}
}

// WithSize sets how many items each page carries.
func WithSize(items int) Option {
	return func(parameters *Parameters) {
		parameters.size = items
		parameters.hasSize = true
	}
}

// Page returns the selected page number, reporting false when no option
// chose one.
func (p Parameters) Page() (int, bool) { return p.page, p.hasPage }

// Size returns the selected page size, reporting false when no option chose
// one.
func (p Parameters) Size() (int, bool) { return p.size, p.hasSize }
