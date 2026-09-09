package auth

import (
	"testing"
	"time"
)

func TestParseUserFields(t *testing.T) {
	body := []byte(`{
		"id":"user-id",
		"aud":"authenticated",
		"role":"authenticated",
		"email":"player@example.com",
		"email_confirmed_at":"2026-01-02T03:04:05Z",
		"phone":"+15552368",
		"created_at":"2026-01-01T00:00:00Z",
		"updated_at":"2026-01-03T00:00:00Z",
		"is_anonymous":false,
		"app_metadata":{"provider":"email"},
		"user_metadata":{"display_name":"First Chair"},
		"identities":[
			{"id":"prov-id","identity_id":"idy-id","user_id":"user-id","provider":"email","identity_data":{"email":"player@example.com"},"created_at":"2026-01-01T00:00:00Z"}
		]
	}`)

	user, err := parseUser(body)
	if err != nil {
		t.Fatalf("parseUser: %v", err)
	}

	if user.ID() != "user-id" {
		t.Errorf("ID = %q", user.ID())
	}
	if user.Audience() != "authenticated" {
		t.Errorf("Audience = %q", user.Audience())
	}
	if user.Email() != "player@example.com" {
		t.Errorf("Email = %q", user.Email())
	}
	if !user.EmailConfirmedAt().Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("EmailConfirmedAt = %v", user.EmailConfirmedAt())
	}
	if user.UserMetadata()["display_name"] != "First Chair" {
		t.Errorf("UserMetadata = %v", user.UserMetadata())
	}
	identities := user.Identities()
	if len(identities) != 1 {
		t.Fatalf("Identities len = %d, want 1", len(identities))
	}
	if identities[0].Provider() != "email" {
		t.Errorf("identity Provider = %q", identities[0].Provider())
	}
	if identities[0].UserID() != "user-id" {
		t.Errorf("identity UserID = %q", identities[0].UserID())
	}
}

func TestParseUserAbsentTimestampsAreZero(t *testing.T) {
	user, err := parseUser([]byte(`{"id":"user-id","created_at":"2026-01-01T00:00:00Z"}`))
	if err != nil {
		t.Fatalf("parseUser: %v", err)
	}
	if !user.LastSignInAt().IsZero() {
		t.Errorf("LastSignInAt = %v, want zero", user.LastSignInAt())
	}
	if !user.BannedUntil().IsZero() {
		t.Errorf("BannedUntil = %v, want zero", user.BannedUntil())
	}
	if !user.DeletedAt().IsZero() {
		t.Errorf("DeletedAt = %v, want zero", user.DeletedAt())
	}
}

func TestUserMetadataCopied(t *testing.T) {
	user, err := parseUser([]byte(`{"id":"user-id","app_metadata":{"provider":"email"}}`))
	if err != nil {
		t.Fatalf("parseUser: %v", err)
	}
	user.AppMetadata()["provider"] = "mutated"
	if user.AppMetadata()["provider"] != "email" {
		t.Error("mutating the returned app metadata changed the user")
	}
}
