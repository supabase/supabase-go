package session

import (
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/supabase/supabase-go/auth/internal/profile"
)

// State is one session's wire document: the access token, the refresh token,
// the instant the access token expires and the user's profile as the server
// served it. A State is immutable, and the zero State holds nothing.
type State struct {
	accessToken  string
	refreshToken string
	expiresAt    time.Time
	user         []byte
}

// wire is the JSON shape of a State: the token endpoint's own field names, so
// a token response parses as a State and an encoded State reads as one.
type wire struct {
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	ExpiresAt    int64           `json:"expires_at"`
	User         json.RawMessage `json:"user"`
}

// errIncomplete is reported when a document lacks a field a State requires.
var errIncomplete = errors.New("session document is missing a field")

// NewState assembles a State from parts obtained separately: an access token
// with the expiry its exp claim states, its refresh token and the user
// document the server served for it. It returns the decode error verbatim when
// user is not a user object, and errIncomplete when any part is empty.
func NewState(accessToken, refreshToken string, expiresAt time.Time, user []byte) (State, error) {
	if accessToken == "" || refreshToken == "" || expiresAt.IsZero() || len(user) == 0 {
		return State{}, errIncomplete
	}
	if _, err := profile.Parse(user); err != nil {
		return State{}, err
	}
	return State{
		accessToken:  accessToken,
		refreshToken: refreshToken,
		expiresAt:    expiresAt,
		user:         slices.Clone(user),
	}, nil
}

// Parse decodes a token response, or a document a [Session] wrote to its
// [Store], into a State. The user object is validated as part of decoding, so a State's user
// always decodes. It returns the decode error verbatim when document is not
// JSON of that shape, and errIncomplete when a required field is absent.
func Parse(document []byte) (State, error) {
	var decoded wire
	if err := json.Unmarshal(document, &decoded); err != nil {
		return State{}, err
	}
	var expiresAt time.Time
	if decoded.ExpiresAt != 0 {
		expiresAt = time.Unix(decoded.ExpiresAt, 0)
	}
	return NewState(decoded.AccessToken, decoded.RefreshToken, expiresAt, decoded.User)
}

// encode renders the state as the JSON document [Parse] reads: the token
// response's field names with nothing beyond the four fields a State holds.
func (s State) encode() []byte {
	document, err := json.Marshal(wire{
		AccessToken:  s.accessToken,
		RefreshToken: s.refreshToken,
		ExpiresAt:    s.expiresAt.Unix(),
		User:         s.user,
	})
	if err != nil {
		// The fields are strings, an integer and JSON that already decoded, so
		// marshaling cannot fail.
		panic("session: encoding state: " + err.Error())
	}
	return document
}

// AccessToken returns the access token.
func (s State) AccessToken() string { return s.accessToken }

// RefreshToken returns the refresh token.
func (s State) RefreshToken() string { return s.refreshToken }

// ExpiresAt returns when the access token expires, as the server stated it.
func (s State) ExpiresAt() time.Time { return s.expiresAt }

// User returns the user document as the server served it, as a copy the caller
// may retain and modify freely. It is nil for the zero State.
func (s State) User() []byte { return slices.Clone(s.user) }
