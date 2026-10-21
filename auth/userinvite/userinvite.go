// Package userinvite carries the vocabulary the SDK's user-invitation call
// takes: the metadata stored on the invited user and where the accepted
// invitation lands, expressed as functional options. A call made with no
// options invites with the server's defaults alone.
package userinvite

// Parameters is the resolved set of invitation parameters for one call. The
// zero value holds none.
type Parameters struct {
	userMetadata map[string]any
	redirectTo   string
}

// Option sets one invitation parameter. Options are applied in the order
// they are supplied, the last setting of a parameter winning.
type Option func(*Parameters)

// Resolve applies options to an empty Parameters and returns the result.
func Resolve(options ...Option) Parameters {
	var parameters Parameters
	for _, option := range options {
		option(&parameters)
	}
	return parameters
}

// WithUserMetadata stores metadata on the invited user, where the
// invitation's accept page can read it.
func WithUserMetadata(metadata map[string]any) Option {
	return func(parameters *Parameters) {
		parameters.userMetadata = metadata
	}
}

// WithRedirectTo names where the accepted invitation lands. A URL that is
// not on the project's redirect allow list is ignored in favor of the
// project's defaults rather than rejected.
func WithRedirectTo(redirectURL string) Option {
	return func(parameters *Parameters) {
		parameters.redirectTo = redirectURL
	}
}

// UserMetadata returns the metadata chosen for the invited user, nil when
// no option chose any.
func (p Parameters) UserMetadata() map[string]any { return p.userMetadata }

// RedirectTo returns the chosen landing URL, empty when no option chose
// one.
func (p Parameters) RedirectTo() string { return p.redirectTo }
