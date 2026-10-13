// Package factor owns the Auth server's MFA factor resource: [Parse] and
// [ParseList] decode the server's factor JSON and the opaque [Factor] holds
// the result, readable only through accessors so no code outside this package
// can express a mutation of its contents.
package factor

import (
	"encoding/json"
	"maps"
	"time"
)

// Factor is the opaque state of one enrolled MFA factor.
// Timestamp accessors return the zero [time.Time] when the server supplied no
// value.
type Factor struct {
	id                        string
	factorType                string
	status                    string
	friendlyName              string
	phone                     string
	webAuthnAAGUID            string
	createdAt                 time.Time
	updatedAt                 time.Time
	lastChallengedAt          time.Time
	lastWebAuthnChallengeData map[string]any
}

// ID returns the factor's unique ID.
func (f Factor) ID() string { return f.id }

// Type returns the factor's type.
func (f Factor) Type() string { return f.factorType }

// Status returns the factor's verification status.
func (f Factor) Status() string { return f.status }

// FriendlyName returns the factor's label.
func (f Factor) FriendlyName() string { return f.friendlyName }

// Phone returns the number a phone factor delivers codes to, or the empty
// string on any other factor type.
func (f Factor) Phone() string { return f.phone }

// WebAuthnAAGUID returns the authenticator model's AAGUID on a WebAuthn
// factor, or the empty string on any other factor type.
func (f Factor) WebAuthnAAGUID() string { return f.webAuthnAAGUID }

// CreatedAt returns when the factor was enrolled.
func (f Factor) CreatedAt() time.Time { return f.createdAt }

// UpdatedAt returns when the factor was last changed.
func (f Factor) UpdatedAt() time.Time { return f.updatedAt }

// LastChallengedAt returns when the factor was last challenged.
func (f Factor) LastChallengedAt() time.Time { return f.lastChallengedAt }

// LastWebAuthnChallengeData returns a copy of the server's record of a
// WebAuthn factor's latest challenge, or nil when there is none. The clone is
// shallow: nested maps and slices are shared references, a deliberate
// shallowness the public surface documents.
func (f Factor) LastWebAuthnChallengeData() map[string]any {
	return maps.Clone(f.lastWebAuthnChallengeData)
}

// factorWire is the JSON shape of the Auth server's factor resource. Optional
// timestamps are pointers so an absent field stays the zero time rather than
// the Unix epoch.
type factorWire struct {
	ID                        string         `json:"id"`
	FactorType                string         `json:"factor_type"`
	Status                    string         `json:"status"`
	FriendlyName              string         `json:"friendly_name"`
	Phone                     string         `json:"phone"`
	WebAuthnAAGUID            string         `json:"web_authn_aaguid"`
	CreatedAt                 *time.Time     `json:"created_at"`
	UpdatedAt                 *time.Time     `json:"updated_at"`
	LastChallengedAt          *time.Time     `json:"last_challenged_at"`
	LastWebAuthnChallengeData map[string]any `json:"last_webauthn_challenge_data"`
}

// Parse decodes the Auth server's factor JSON into a [Factor], returning the
// decode error verbatim when the body is not a factor object; the caller owns
// the consumer-facing wrapping.
func Parse(body []byte) (Factor, error) {
	var wire factorWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return Factor{}, err
	}
	return fromWire(wire), nil
}

// ParseList decodes the Auth server's factor array, as [Parse] decodes one
// factor.
func ParseList(body []byte) ([]Factor, error) {
	var wires []factorWire
	if err := json.Unmarshal(body, &wires); err != nil {
		return nil, err
	}
	factors := make([]Factor, len(wires))
	for i, wire := range wires {
		factors[i] = fromWire(wire)
	}
	return factors, nil
}

// fromWire builds one [Factor] from its decoded wire shape.
func fromWire(wire factorWire) Factor {
	return Factor{
		id:                        wire.ID,
		factorType:                wire.FactorType,
		status:                    wire.Status,
		friendlyName:              wire.FriendlyName,
		phone:                     wire.Phone,
		webAuthnAAGUID:            wire.WebAuthnAAGUID,
		createdAt:                 timeOrZero(wire.CreatedAt),
		updatedAt:                 timeOrZero(wire.UpdatedAt),
		lastChallengedAt:          timeOrZero(wire.LastChallengedAt),
		lastWebAuthnChallengeData: wire.LastWebAuthnChallengeData,
	}
}

// timeOrZero dereferences an optional wire timestamp, yielding the zero time
// when the field was absent.
func timeOrZero(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}
