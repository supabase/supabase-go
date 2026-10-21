package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/supabase/supabase-go/auth/internal/link"
	"github.com/supabase/supabase-go/auth/internal/profile"
)

// NewLinkRequest starts a link request for email, the one input every kind of
// link requires. Refine it with [LinkRequestBuilder.RedirectTo], then choose
// the kind of link with one of the mode methods - the result is what
// [Admin.GenerateLink] accepts.
func NewLinkRequest(email string) LinkRequestBuilder {
	return LinkRequestBuilder{request: link.New(email)}
}

// LinkRequestBuilder is a link request before its mode is chosen. It is an
// immutable value: each method returns a new value, so builders fork freely
// and never alias.
type LinkRequestBuilder struct {
	request link.Request
}

// RedirectTo names where the link lands after the server verifies it. A URL
// that is not on the project's redirect allow list is ignored in favor of the
// project's defaults rather than rejected.
func (b LinkRequestBuilder) RedirectTo(redirectURL string) LinkRequestBuilder {
	return LinkRequestBuilder{request: b.request.WithRedirectTo(redirectURL)}
}

// Signup describes a signup confirmation link for a new user with the given
// password. For an email already signed up and confirmed the server rejects
// the request, and for one signed up but unconfirmed it ignores the password
// and merges the metadata per key.
func (b LinkRequestBuilder) Signup(password string) SignupLinkRequest {
	return SignupLinkRequest{request: b.request.WithMode("signup").WithPassword(password)}
}

// Invite describes an invitation link. The server creates the user if the
// email is unknown and rejects one already confirmed. No invitation email is
// sent: sending is the caller's job, or [Admin.InviteUser]'s.
func (b LinkRequestBuilder) Invite() InviteLinkRequest {
	return InviteLinkRequest{request: b.request.WithMode("invite")}
}

// MagicLink describes a password-less sign-in link. An unknown email is signed
// up on the spot with a generated password, which is when the metadata
// applies; for an existing user it is ignored.
func (b LinkRequestBuilder) MagicLink() MagicLinkRequest {
	return MagicLinkRequest{request: b.request.WithMode("magiclink")}
}

// Recovery describes a password recovery link for an existing user. An
// unknown email is the server's rejection, an [*Error] whose HTTPStatus is
// 404.
func (b LinkRequestBuilder) Recovery() RecoveryLinkRequest {
	return RecoveryLinkRequest{request: b.request.WithMode("recovery")}
}

// EmailChangeToCurrent describes the link that confirms moving the user's
// email to newEmail, for delivery to the current address. The server answers
// it only on projects with secure email change enabled, and an unknown email
// is its rejection, an [*Error] whose HTTPStatus is 404.
func (b LinkRequestBuilder) EmailChangeToCurrent(newEmail string) EmailChangeLinkRequest {
	return EmailChangeLinkRequest{request: b.request.WithMode("email_change_current").WithNewEmail(newEmail)}
}

// EmailChangeToNew describes the link that confirms moving the user's email
// to newEmail, for delivery to that new address. An unknown email is the
// server's rejection, an [*Error] whose HTTPStatus is 404.
func (b LinkRequestBuilder) EmailChangeToNew(newEmail string) EmailChangeLinkRequest {
	return EmailChangeLinkRequest{request: b.request.WithMode("email_change_new").WithNewEmail(newEmail)}
}

// SignupLinkRequest is a signup link request, ready for [Admin.GenerateLink].
type SignupLinkRequest struct {
	request link.Request
}

// UserMetadata stores metadata on the user the link signs up, merged per key
// for an existing unconfirmed user.
func (r SignupLinkRequest) UserMetadata(metadata map[string]any) SignupLinkRequest {
	return SignupLinkRequest{request: r.request.WithData(metadata)}
}

func (r SignupLinkRequest) linkRequest() link.Request { return r.request }

// InviteLinkRequest is an invitation link request, ready for
// [Admin.GenerateLink].
type InviteLinkRequest struct {
	request link.Request
}

// UserMetadata stores metadata on the user the link invites.
func (r InviteLinkRequest) UserMetadata(metadata map[string]any) InviteLinkRequest {
	return InviteLinkRequest{request: r.request.WithData(metadata)}
}

func (r InviteLinkRequest) linkRequest() link.Request { return r.request }

// MagicLinkRequest is a password-less sign-in link request, ready for
// [Admin.GenerateLink].
type MagicLinkRequest struct {
	request link.Request
}

// UserMetadata stores metadata on the user the link signs up, read only when
// the email is unknown and a user is created for it.
func (r MagicLinkRequest) UserMetadata(metadata map[string]any) MagicLinkRequest {
	return MagicLinkRequest{request: r.request.WithData(metadata)}
}

func (r MagicLinkRequest) linkRequest() link.Request { return r.request }

// RecoveryLinkRequest is a password recovery link request, ready for
// [Admin.GenerateLink].
type RecoveryLinkRequest struct {
	request link.Request
}

func (r RecoveryLinkRequest) linkRequest() link.Request { return r.request }

// EmailChangeLinkRequest is an email change confirmation link request, ready
// for [Admin.GenerateLink].
type EmailChangeLinkRequest struct {
	request link.Request
}

func (r EmailChangeLinkRequest) linkRequest() link.Request { return r.request }

// LinkRequest is a link request with its mode chosen, built from
// [NewLinkRequest]. The five mode types satisfy it and nothing outside this
// package can.
type LinkRequest interface {
	linkRequest() link.Request
}

// GenerateLink mints the link without sending any email: the calling
// application delivers it through its own mailer instead of the Auth
// server's built-in one. The returned link is a credential for the account
// it targets: hand it to the mailer and log it nowhere.
func (a *Admin) GenerateLink(ctx context.Context, request LinkRequest) (*GenerateLinkResult, error) {
	body, err := a.send(ctx, http.MethodPost, a.adminBaseURL.JoinPath("generate_link"), request.linkRequest().Payload())
	if err != nil {
		return nil, err
	}
	return parseGenerateLinkResult(body)
}

// GenerateLinkResult is the server's answer to [Admin.GenerateLink]: the
// link, its verification artifacts and the user it targets.
type GenerateLinkResult struct {
	actionLink       string
	emailOTP         string
	hashedToken      string
	redirectTo       string
	verificationType string
	user             User
}

// ActionLink returns the full verification URL, the link to deliver.
func (l *GenerateLinkResult) ActionLink() string { return l.actionLink }

// EmailOTP returns the one-time password the link carries, for templates
// that have the user type a code instead of clicking.
func (l *GenerateLinkResult) EmailOTP() string { return l.emailOTP }

// HashedToken returns the hash of the link's token, for building a custom
// verification URL against the project's verify endpoint.
func (l *GenerateLinkResult) HashedToken() string { return l.hashedToken }

// RedirectTo returns where the link lands after the server verifies it.
func (l *GenerateLinkResult) RedirectTo() string { return l.redirectTo }

// VerificationType returns the kind of link minted, as the wire names it:
// "signup", "invite", "magiclink", "recovery", "email_change_current" or
// "email_change_new".
func (l *GenerateLinkResult) VerificationType() string { return l.verificationType }

// User returns the user the link targets, as stored when the link was
// minted. The result is a copy the caller may retain freely.
func (l *GenerateLinkResult) User() *User {
	user := l.user
	return &user
}

// parseGenerateLinkResult decodes the generate-link response, whose wire
// shape is one flat object: the link fields and the user's fields side by
// side. The body is therefore parsed twice, once for each half.
func parseGenerateLinkResult(body []byte) (*GenerateLinkResult, error) {
	var wire struct {
		ActionLink       string `json:"action_link"`
		EmailOTP         string `json:"email_otp"`
		HashedToken      string `json:"hashed_token"`
		RedirectTo       string `json:"redirect_to"`
		VerificationType string `json:"verification_type"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("auth: decoding generated link: %w", err)
	}
	user, err := profile.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("auth: decoding generated link: %w", err)
	}
	return &GenerateLinkResult{
		actionLink:       wire.ActionLink,
		emailOTP:         wire.EmailOTP,
		hashedToken:      wire.HashedToken,
		redirectTo:       wire.RedirectTo,
		verificationType: wire.VerificationType,
		user:             User{inner: user},
	}, nil
}
