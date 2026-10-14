package auth

import (
	"fmt"
	"time"

	"github.com/supabase/supabase-go/auth/internal/factor"
)

// Factor is one enrolled multi-factor authentication factor on a user, as the
// Auth server holds it now. A Factor is immutable and safe for concurrent use
// by multiple goroutines. Timestamp accessors return the zero [time.Time] when
// the server supplied no value.
type Factor struct {
	inner factor.Factor
}

// ID returns the factor's unique ID.
func (f Factor) ID() string { return f.inner.ID() }

// Type returns the factor's type: "totp", "phone" or "webauthn", with any
// type a newer server adds passing through verbatim.
func (f Factor) Type() string { return f.inner.Type() }

// Status returns the factor's verification status: "unverified" from
// enrollment until the user first proves possession, then "verified". Only
// verified factors raise the session's authenticator assurance level.
func (f Factor) Status() string { return f.inner.Status() }

// FriendlyName returns the label the factor was enrolled with, or last given
// by [Admin.UpdateMFAFactor], or the empty string when it has none.
func (f Factor) FriendlyName() string { return f.inner.FriendlyName() }

// Phone returns the number a phone factor delivers codes to, or the empty
// string on any other factor type.
func (f Factor) Phone() string { return f.inner.Phone() }

// WebAuthnAAGUID returns the authenticator model's AAGUID on a WebAuthn
// factor, or the empty string on any other factor type.
func (f Factor) WebAuthnAAGUID() string { return f.inner.WebAuthnAAGUID() }

// CreatedAt returns when the factor was enrolled.
func (f Factor) CreatedAt() time.Time { return f.inner.CreatedAt() }

// UpdatedAt returns when the factor was last changed.
func (f Factor) UpdatedAt() time.Time { return f.inner.UpdatedAt() }

// LastChallengedAt returns when the factor was last challenged to verify,
// or the zero time when it never was.
func (f Factor) LastChallengedAt() time.Time { return f.inner.LastChallengedAt() }

// LastWebAuthnChallengeData returns the server's record of a WebAuthn
// factor's latest challenge, or nil when there is none. The result is a copy
// the caller may retain and modify freely.
func (f Factor) LastWebAuthnChallengeData() map[string]any {
	return f.inner.LastWebAuthnChallengeData()
}

// parseFactor decodes a factor response.
func parseFactor(body []byte) (*Factor, error) {
	inner, err := factor.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("auth: decoding factor: %w", err)
	}
	return &Factor{inner: inner}, nil
}

// parseFactorList decodes a factor array response.
func parseFactorList(body []byte) ([]Factor, error) {
	inners, err := factor.ParseList(body)
	if err != nil {
		return nil, fmt.Errorf("auth: decoding factor list: %w", err)
	}
	factors := make([]Factor, len(inners))
	for i, inner := range inners {
		factors[i] = Factor{inner: inner}
	}
	return factors, nil
}
