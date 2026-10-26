package auth

import (
	"time"

	"github.com/supabase/supabase-go/auth/internal/profile"
)

// User is the authenticated user's profile as the Auth server holds it now. A
// User is immutable and safe for concurrent use by multiple goroutines.
// Timestamp accessors return the zero [time.Time] when the server supplied no
// value.
type User struct {
	inner profile.User
}

// ID returns the user's unique ID, the value a token's sub claim carries
// (see [Claims.Subject]).
func (u *User) ID() string { return u.inner.ID() }

// Audience returns the user's aud value.
func (u *User) Audience() string { return u.inner.Audience() }

// Role returns the user's Postgres role, usually "authenticated". Database
// access runs under a token's role claim rather than this value, and a
// [Custom Access Token hook] can make the two differ (see [Claims.Role]).
//
// [Custom Access Token hook]: https://supabase.com/docs/guides/auth/auth-hooks/custom-access-token-hook
func (u *User) Role() string { return u.inner.Role() }

// Email returns the user's email address, or the empty string when none is set.
func (u *User) Email() string { return u.inner.Email() }

// EmailConfirmedAt returns when the user's email was confirmed.
func (u *User) EmailConfirmedAt() time.Time { return u.inner.EmailConfirmedAt() }

// Phone returns the user's phone number, or the empty string when none is set.
func (u *User) Phone() string { return u.inner.Phone() }

// PhoneConfirmedAt returns when the user's phone number was confirmed.
func (u *User) PhoneConfirmedAt() time.Time { return u.inner.PhoneConfirmedAt() }

// ConfirmedAt returns when the user was first confirmed by any method.
func (u *User) ConfirmedAt() time.Time { return u.inner.ConfirmedAt() }

// InvitedAt returns when the user was last sent an invitation, or the zero
// time when they never were.
func (u *User) InvitedAt() time.Time { return u.inner.InvitedAt() }

// ConfirmationSentAt returns when the last signup or invitation confirmation
// email was sent, or the zero time when none ever was.
func (u *User) ConfirmationSentAt() time.Time { return u.inner.ConfirmationSentAt() }

// RecoverySentAt returns when the last password recovery or magic link email
// was sent, or the zero time when none ever was.
func (u *User) RecoverySentAt() time.Time { return u.inner.RecoverySentAt() }

// NewEmail returns the address a pending email change moves the user to, or
// the empty string when no change is pending.
func (u *User) NewEmail() string { return u.inner.NewEmail() }

// EmailChangeSentAt returns when the last email change confirmation was sent,
// or the zero time when none ever was.
func (u *User) EmailChangeSentAt() time.Time { return u.inner.EmailChangeSentAt() }

// NewPhone returns the number a pending phone change moves the user to, or
// the empty string when no change is pending.
func (u *User) NewPhone() string { return u.inner.NewPhone() }

// PhoneChangeSentAt returns when the last phone change confirmation message
// was sent, or the zero time when none ever was.
func (u *User) PhoneChangeSentAt() time.Time { return u.inner.PhoneChangeSentAt() }

// ReauthenticationSentAt returns when the last reauthentication one-time
// password was sent, or the zero time when none ever was.
func (u *User) ReauthenticationSentAt() time.Time { return u.inner.ReauthenticationSentAt() }

// LastSignInAt returns when the user most recently signed in.
func (u *User) LastSignInAt() time.Time { return u.inner.LastSignInAt() }

// CreatedAt returns when the user was created.
func (u *User) CreatedAt() time.Time { return u.inner.CreatedAt() }

// UpdatedAt returns when the user was last updated.
func (u *User) UpdatedAt() time.Time { return u.inner.UpdatedAt() }

// BannedUntil returns the instant a ban on the user lifts, or the zero time
// when the user is not banned.
func (u *User) BannedUntil() time.Time { return u.inner.BannedUntil() }

// DeletedAt returns when the user was soft-deleted, or the zero time when the
// user is not deleted.
func (u *User) DeletedAt() time.Time { return u.inner.DeletedAt() }

// IsAnonymous reports whether the user is anonymous rather than signed in.
func (u *User) IsAnonymous() bool { return u.inner.IsAnonymous() }

// AppMetadata returns the application-controlled metadata, such as the sign-in
// providers. The result is a copy the caller may retain and modify freely.
func (u *User) AppMetadata() map[string]any { return u.inner.AppMetadata() }

// UserMetadata returns the user-controlled metadata set at sign-up or update.
// The result is a copy the caller may retain and modify freely.
func (u *User) UserMetadata() map[string]any { return u.inner.UserMetadata() }

// Identities returns the user's linked sign-in identities. The result is a copy
// the caller may retain and modify freely.
func (u *User) Identities() []Identity {
	inner := u.inner.Identities()
	identities := make([]Identity, len(inner))
	for i, identity := range inner {
		identities[i] = Identity{inner: identity}
	}
	return identities
}

// Factors returns the user's enrolled multi-factor authentication factors.
// The result is a copy the caller may retain and modify freely.
func (u *User) Factors() []Factor {
	inner := u.inner.Factors()
	factors := make([]Factor, len(inner))
	for i, enrolled := range inner {
		factors[i] = Factor{inner: enrolled}
	}
	return factors
}

// Identity is one linked sign-in identity - a provider account - on a user.
type Identity struct {
	inner profile.Identity
}

// ID returns the identity's provider-scoped ID.
func (i Identity) ID() string { return i.inner.ID() }

// IdentityID returns the identity's own unique ID within the project.
func (i Identity) IdentityID() string { return i.inner.IdentityID() }

// Email returns the email address carried in the identity's provider data,
// or the empty string when the provider reported none.
func (i Identity) Email() string { return i.inner.Email() }

// UserID returns the ID of the user this identity belongs to.
func (i Identity) UserID() string { return i.inner.UserID() }

// Provider returns the identity's provider, such as "email" or "google".
func (i Identity) Provider() string { return i.inner.Provider() }

// IdentityData returns the profile data the provider supplied for this identity.
// The result is a copy the caller may retain and modify freely.
func (i Identity) IdentityData() map[string]any { return i.inner.IdentityData() }

// CreatedAt returns when the identity was linked.
func (i Identity) CreatedAt() time.Time { return i.inner.CreatedAt() }

// LastSignInAt returns when the user most recently signed in through this
// identity.
func (i Identity) LastSignInAt() time.Time { return i.inner.LastSignInAt() }

// UpdatedAt returns when the identity was last updated.
func (i Identity) UpdatedAt() time.Time { return i.inner.UpdatedAt() }

// GetUserResult is the server's answer to [Client.GetUser] and
// [Admin.GetUser]: one type serves both, the two operations sharing their
// name and their shape.
type GetUserResult struct {
	user *User
}

// User returns the fetched user's profile.
func (r *GetUserResult) User() *User { return r.user }
