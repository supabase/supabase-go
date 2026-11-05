package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/supabase/supabase-go/auth/internal/session"
)

func TestPanickingCommitLeavesSessionUsable(t *testing.T) {
	state, err := session.NewState("access-token", "refresh-token", time.Now().Add(-time.Minute), []byte(`{"id":"33333333-3333-3333-3333-333333333333"}`))
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	refused := errors.New("refused")
	verdicts := 0
	live := session.New(state, session.Options{
		Refresh: func(context.Context, string) (session.State, error) {
			return session.State{}, refused
		},
		Definitive: func(error) bool {
			verdicts++
			if verdicts == 1 {
				panic("verdict failure")
			}
			return false
		},
		ExpiryMargin: 30 * time.Second,
		Now:          time.Now,
	})

	func() {
		defer func() {
			if recovered := recover(); recovered != "verdict failure" {
				t.Errorf("recovered %v, want the verdict's panic passed on", recovered)
			}
		}()
		clearErr, err := live.Refresh(t.Context())
		t.Fatalf("Refresh = (%v, %v), want the verdict's panic passed on", clearErr, err)
	}()

	type outcome struct{ clearErr, err error }
	refreshed := make(chan outcome, 1)
	go func() {
		clearErr, err := live.Refresh(t.Context())
		refreshed <- outcome{clearErr, err}
	}()
	select {
	case got := <-refreshed:
		if got.clearErr != nil || !errors.Is(got.err, refused) {
			t.Errorf("Refresh after the panic = (%v, %v), want no failed clear and the refresh failure", got.clearErr, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Refresh after the panic blocked on a lock or flight the panic left held")
	}
	if verdicts != 2 {
		t.Errorf("the verdict was asked %d times, want once per refresh", verdicts)
	}
	if refreshToken := live.State().RefreshToken(); refreshToken != "refresh-token" {
		t.Errorf("State().RefreshToken() = %q, want the session as it was", refreshToken)
	}
}
