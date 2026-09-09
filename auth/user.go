package auth

import (
	"encoding/json"
	"maps"
	"slices"
	"time"
)

// User is the authenticated user's profile as the Auth server holds it now. A
// User is immutable and safe for concurrent use by multiple goroutines.
// Instances come only from [Client.GetUser]. Timestamp accessors return the
// zero [time.Time] when the server supplied no value.
type User struct {
	id               string
	audience         string
	role             string
	email            string
	emailConfirmedAt time.Time
	phone            string
	phoneConfirmedAt time.Time
	confirmedAt      time.Time
	lastSignInAt     time.Time
	createdAt        time.Time
	updatedAt        time.Time
	bannedUntil      time.Time
	deletedAt        time.Time
	isAnonymous      bool
	appMetadata      map[string]any
	userMetadata     map[string]any
	identities       []Identity
}

// ID returns the user's unique ID, the value a token's sub claim carries.
func (u *User) ID() string { return u.id }

// Audience returns the user's aud value.
func (u *User) Audience() string { return u.audience }

// Role returns the user's Postgres role, usually "authenticated".
func (u *User) Role() string { return u.role }

// Email returns the user's email address, or the empty string when none is set.
func (u *User) Email() string { return u.email }

// EmailConfirmedAt returns when the user's email was confirmed.
func (u *User) EmailConfirmedAt() time.Time { return u.emailConfirmedAt }

// Phone returns the user's phone number, or the empty string when none is set.
func (u *User) Phone() string { return u.phone }

// PhoneConfirmedAt returns when the user's phone number was confirmed.
func (u *User) PhoneConfirmedAt() time.Time { return u.phoneConfirmedAt }

// ConfirmedAt returns when the user was first confirmed by any method.
func (u *User) ConfirmedAt() time.Time { return u.confirmedAt }

// LastSignInAt returns when the user most recently signed in.
func (u *User) LastSignInAt() time.Time { return u.lastSignInAt }

// CreatedAt returns when the user was created.
func (u *User) CreatedAt() time.Time { return u.createdAt }

// UpdatedAt returns when the user was last updated.
func (u *User) UpdatedAt() time.Time { return u.updatedAt }

// BannedUntil returns the instant a ban on the user lifts, or the zero time
// when the user is not banned.
func (u *User) BannedUntil() time.Time { return u.bannedUntil }

// DeletedAt returns when the user was soft-deleted, or the zero time when the
// user is not deleted.
func (u *User) DeletedAt() time.Time { return u.deletedAt }

// IsAnonymous reports whether the user is anonymous rather than signed in.
func (u *User) IsAnonymous() bool { return u.isAnonymous }

// AppMetadata returns the application-controlled metadata, such as the sign-in
// providers. The result is a copy the caller may retain and modify freely.
func (u *User) AppMetadata() map[string]any { return maps.Clone(u.appMetadata) }

// UserMetadata returns the user-controlled metadata set at sign-up or update.
// The result is a copy the caller may retain and modify freely.
func (u *User) UserMetadata() map[string]any { return maps.Clone(u.userMetadata) }

// Identities returns the user's linked sign-in identities. The result is a copy
// the caller may retain and modify freely.
func (u *User) Identities() []Identity { return slices.Clone(u.identities) }

// Identity is one linked sign-in identity - a provider account - on a user.
type Identity struct {
	id           string
	identityID   string
	userID       string
	provider     string
	identityData map[string]any
	createdAt    time.Time
	lastSignInAt time.Time
	updatedAt    time.Time
}

// ID returns the identity's provider-scoped ID.
func (i Identity) ID() string { return i.id }

// IdentityID returns the identity's own unique ID within the project.
func (i Identity) IdentityID() string { return i.identityID }

// UserID returns the ID of the user this identity belongs to.
func (i Identity) UserID() string { return i.userID }

// Provider returns the identity's provider, such as "email" or "google".
func (i Identity) Provider() string { return i.provider }

// IdentityData returns the profile data the provider supplied for this identity.
// The result is a copy the caller may retain and modify freely.
func (i Identity) IdentityData() map[string]any { return maps.Clone(i.identityData) }

// CreatedAt returns when the identity was linked.
func (i Identity) CreatedAt() time.Time { return i.createdAt }

// LastSignInAt returns when the user most recently signed in through this
// identity.
func (i Identity) LastSignInAt() time.Time { return i.lastSignInAt }

// UpdatedAt returns when the identity was last updated.
func (i Identity) UpdatedAt() time.Time { return i.updatedAt }

// userWire is the JSON shape of the Auth server's user resource. Optional
// timestamps are pointers so an absent field stays the zero time rather than
// the Unix epoch.
type userWire struct {
	ID               string         `json:"id"`
	Audience         string         `json:"aud"`
	Role             string         `json:"role"`
	Email            string         `json:"email"`
	EmailConfirmedAt *time.Time     `json:"email_confirmed_at"`
	Phone            string         `json:"phone"`
	PhoneConfirmedAt *time.Time     `json:"phone_confirmed_at"`
	ConfirmedAt      *time.Time     `json:"confirmed_at"`
	LastSignInAt     *time.Time     `json:"last_sign_in_at"`
	CreatedAt        *time.Time     `json:"created_at"`
	UpdatedAt        *time.Time     `json:"updated_at"`
	BannedUntil      *time.Time     `json:"banned_until"`
	DeletedAt        *time.Time     `json:"deleted_at"`
	IsAnonymous      bool           `json:"is_anonymous"`
	AppMetadata      map[string]any `json:"app_metadata"`
	UserMetadata     map[string]any `json:"user_metadata"`
	Identities       []identityWire `json:"identities"`
}

// identityWire is the JSON shape of one entry in a user's identities array.
type identityWire struct {
	ID           string         `json:"id"`
	IdentityID   string         `json:"identity_id"`
	UserID       string         `json:"user_id"`
	Provider     string         `json:"provider"`
	IdentityData map[string]any `json:"identity_data"`
	CreatedAt    *time.Time     `json:"created_at"`
	LastSignInAt *time.Time     `json:"last_sign_in_at"`
	UpdatedAt    *time.Time     `json:"updated_at"`
}

// parseUser decodes the Auth server's user JSON into a User, returning the
// decode error when the body is not a user object.
func parseUser(body []byte) (*User, error) {
	var wire userWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}

	user := &User{
		id:               wire.ID,
		audience:         wire.Audience,
		role:             wire.Role,
		email:            wire.Email,
		emailConfirmedAt: timeOrZero(wire.EmailConfirmedAt),
		phone:            wire.Phone,
		phoneConfirmedAt: timeOrZero(wire.PhoneConfirmedAt),
		confirmedAt:      timeOrZero(wire.ConfirmedAt),
		lastSignInAt:     timeOrZero(wire.LastSignInAt),
		createdAt:        timeOrZero(wire.CreatedAt),
		updatedAt:        timeOrZero(wire.UpdatedAt),
		bannedUntil:      timeOrZero(wire.BannedUntil),
		deletedAt:        timeOrZero(wire.DeletedAt),
		isAnonymous:      wire.IsAnonymous,
		appMetadata:      wire.AppMetadata,
		userMetadata:     wire.UserMetadata,
	}
	for _, identity := range wire.Identities {
		user.identities = append(user.identities, Identity{
			id:           identity.ID,
			identityID:   identity.IdentityID,
			userID:       identity.UserID,
			provider:     identity.Provider,
			identityData: identity.IdentityData,
			createdAt:    timeOrZero(identity.CreatedAt),
			lastSignInAt: timeOrZero(identity.LastSignInAt),
			updatedAt:    timeOrZero(identity.UpdatedAt),
		})
	}
	return user, nil
}

// timeOrZero dereferences an optional wire timestamp, yielding the zero time
// when the field was absent.
func timeOrZero(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}
