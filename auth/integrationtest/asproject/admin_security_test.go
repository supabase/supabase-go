package asproject

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/mfafactor"
	"github.com/supabase/supabase-go/auth/signout"
	"github.com/supabase/supabase-go/integration-testing/testkit"
)

// enrollTOTPFactor enrolls a TOTP factor for the user accessToken belongs to
// and returns the new factor's id. It calls the user-side enrolment endpoint
// over raw HTTP deliberately: the MFA admin capabilities act on enrolled
// factors, and enrolment itself is a different, user-side capability this SDK
// does not offer yet.
func enrollTOTPFactor(t *testing.T, projectURL, apiKey, accessToken, friendlyName string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"factor_type": "totp", "friendly_name": friendlyName})
	if err != nil {
		t.Fatalf("marshaling enroll payload: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, projectURL+"/auth/v1/factors", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("building enroll request: %v", err)
	}
	request.Header.Set("apikey", apiKey)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("enrolling TOTP factor: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("enrolling TOTP factor: HTTP %d: %s - is auth.mfa.totp enabled in config.toml?", response.StatusCode, body)
	}
	var factor struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&factor); err != nil {
		t.Fatalf("decoding enroll response: %v", err)
	}
	if factor.ID == "" {
		t.Fatal("enroll response carries no factor id")
	}
	return factor.ID
}

// TestAdminSignOut proves revocation against the session surface: a token
// works before, survives a sign-out that spares its session and fails with
// the server's session_not_found once its session is revoked.
func TestAdminSignOut(t *testing.T) {
	projectURL, apiKey := testkit.Credentials(t)
	user := testkit.SignUpUser(t, projectURL, apiKey)
	client := newAuthClient(t)
	admin := newAdmin(t)

	if _, err := client.GetUser(t.Context(), user.AccessToken); err != nil {
		t.Fatalf("GetUser before sign-out: %v", err)
	}

	if err := admin.SignOut(t.Context(), user.AccessToken, signout.WithOthersScope()); err != nil {
		t.Fatalf("SignOut others: %v", err)
	}
	if _, err := client.GetUser(t.Context(), user.AccessToken); err != nil {
		t.Errorf("GetUser after WithOthersScope = %v, want the presented session spared", err)
	}

	// The bare call is the server's default: global revocation.
	if err := admin.SignOut(t.Context(), user.AccessToken); err != nil {
		t.Fatalf("SignOut: %v", err)
	}
	_, err := client.GetUser(t.Context(), user.AccessToken)
	var serverError *auth.Error
	if !errors.As(err, &serverError) {
		t.Fatalf("GetUser after the global sign-out = %v, want *auth.Error", err)
	}
	if serverError.HTTPStatus != http.StatusForbidden || serverError.Code != "session_not_found" {
		t.Errorf("rejection = HTTP %d code %q, want 403 session_not_found", serverError.HTTPStatus, serverError.Code)
	}

	// The local scope ends the presented session itself.
	second := testkit.SignUpUser(t, projectURL, apiKey)
	if err := admin.SignOut(t.Context(), second.AccessToken, signout.WithLocalScope()); err != nil {
		t.Fatalf("SignOut local: %v", err)
	}
	if _, err := client.GetUser(t.Context(), second.AccessToken); err == nil {
		t.Error("GetUser after WithLocalScope succeeded, want the presented session revoked")
	}

	for _, id := range []string{user.ID, second.ID} {
		if err := admin.DeleteUser(t.Context(), id); err != nil {
			t.Errorf("DeleteUser(%s): %v", id, err)
		}
	}
}

// TestAdminMFAFactors walks one factor through its whole admin lifecycle -
// list after enrolment, the user resource mirroring it, rename, the ignored
// phone number, the bare update, delete - asserting every property the served
// factor carries at each step.
func TestAdminMFAFactors(t *testing.T) {
	projectURL, apiKey := testkit.Credentials(t)
	user := testkit.SignUpUser(t, projectURL, apiKey)
	admin := newAdmin(t)

	factorID := enrollTOTPFactor(t, projectURL, apiKey, user.AccessToken, "Original authenticator")

	factors, err := admin.ListMFAFactors(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("ListMFAFactors: %v", err)
	}
	if len(factors) != 1 {
		t.Fatalf("ListMFAFactors = %d factors, want the one enrolled", len(factors))
	}
	enrolled := factors[0]
	if enrolled.ID() != factorID {
		t.Errorf("ID = %q, want the enrolled factor %q", enrolled.ID(), factorID)
	}
	if enrolled.Type() != "totp" {
		t.Errorf("Type = %q, want totp", enrolled.Type())
	}
	if enrolled.Status() != "unverified" {
		t.Errorf("Status = %q, want unverified - the factor was enrolled but never proven", enrolled.Status())
	}
	if enrolled.FriendlyName() != "Original authenticator" {
		t.Errorf("FriendlyName = %q, want the enrolment's label", enrolled.FriendlyName())
	}
	if enrolled.Phone() != "" {
		t.Errorf("Phone = %q, want empty on a TOTP factor", enrolled.Phone())
	}
	if enrolled.CreatedAt().IsZero() || enrolled.UpdatedAt().IsZero() {
		t.Errorf("timestamps = (%v, %v), want both set", enrolled.CreatedAt(), enrolled.UpdatedAt())
	}
	if !enrolled.LastChallengedAt().IsZero() {
		t.Errorf("LastChallengedAt = %v, want zero - the factor was never challenged", enrolled.LastChallengedAt())
	}
	if enrolled.WebAuthnAAGUID() != "" || enrolled.LastWebAuthnChallengeData() != nil {
		t.Errorf("WebAuthn fields = (%q, %v), want both empty on a TOTP factor", enrolled.WebAuthnAAGUID(), enrolled.LastWebAuthnChallengeData())
	}

	// The user resource mirrors the enrolled factor.
	fetched, err := admin.GetUser(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	mirrored := fetched.Factors()
	if len(mirrored) != 1 || mirrored[0].ID() != factorID || mirrored[0].Type() != "totp" {
		t.Errorf("User.Factors = %d entries, want the enrolled factor mirrored on the user resource", len(mirrored))
	}

	renamed, err := admin.UpdateMFAFactor(t.Context(), user.ID, factorID, mfafactor.WithFriendlyName("Recognized device"))
	if err != nil {
		t.Fatalf("UpdateMFAFactor: %v", err)
	}
	if renamed.FriendlyName() != "Recognized device" {
		t.Errorf("FriendlyName = %q, want the new label", renamed.FriendlyName())
	}
	if renamed.ID() != factorID || renamed.Type() != "totp" || renamed.Status() != "unverified" {
		t.Errorf("renamed = (%q, %q, %q), want everything but the label unchanged", renamed.ID(), renamed.Type(), renamed.Status())
	}
	if !renamed.CreatedAt().Equal(enrolled.CreatedAt()) {
		t.Errorf("CreatedAt = %v, want the enrolment instant %v unchanged", renamed.CreatedAt(), enrolled.CreatedAt())
	}

	// A phone number on a TOTP factor is ignored, success and all, as the
	// option documents.
	unchanged, err := admin.UpdateMFAFactor(t.Context(), user.ID, factorID, mfafactor.WithPhone("+15555550123"))
	if err != nil {
		t.Fatalf("UpdateMFAFactor with WithPhone: %v", err)
	}
	if unchanged.Phone() != "" {
		t.Errorf("Phone = %q, want the number ignored on a TOTP factor", unchanged.Phone())
	}

	// A bare update changes nothing and still succeeds.
	untouched, err := admin.UpdateMFAFactor(t.Context(), user.ID, factorID)
	if err != nil {
		t.Fatalf("bare UpdateMFAFactor: %v", err)
	}
	if untouched.FriendlyName() != "Recognized device" {
		t.Errorf("FriendlyName = %q, want the rename to have held", untouched.FriendlyName())
	}

	if err := admin.DeleteMFAFactor(t.Context(), user.ID, factorID); err != nil {
		t.Fatalf("DeleteMFAFactor: %v", err)
	}
	factors, err = admin.ListMFAFactors(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("ListMFAFactors after delete: %v", err)
	}
	if len(factors) != 0 {
		t.Errorf("factors = %d, want none after the delete", len(factors))
	}

	// A second delete is the server's 404 with its MFA code.
	err = admin.DeleteMFAFactor(t.Context(), user.ID, factorID)
	var serverError *auth.Error
	if !errors.As(err, &serverError) {
		t.Fatalf("second DeleteMFAFactor = %v, want *auth.Error", err)
	}
	if serverError.HTTPStatus != http.StatusNotFound || serverError.Code != "mfa_factor_not_found" {
		t.Errorf("rejection = HTTP %d code %q, want 404 mfa_factor_not_found", serverError.HTTPStatus, serverError.Code)
	}

	if err := admin.DeleteUser(t.Context(), user.ID); err != nil {
		t.Errorf("DeleteUser: %v", err)
	}
}
