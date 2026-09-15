// Command rls-backend is an HTTP backend built on the composed Supabase
// client: each request's bearer token is verified with the Auth client, then
// the Database client queries as that user so Row Level Security confines the
// response to the user's own rows.
//
// It runs against the local Supabase stack started by
// scripts/integration-test.sh, reading SUPABASE_URL and
// SUPABASE_PUBLISHABLE_KEY from the environment. Standing in for an
// application's own sign-in flow, it registers a throwaway user over the Auth
// REST API, inserts a practice-log row as that user, then calls its own
// handler with the user's token and prints the rows the handler returns.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/postgrest"
	"github.com/supabase/supabase-go/supabase"
)

// practiceLog maps a row of the public.practice_logs table, whose Row Level
// Security policies admit a user to only their own rows.
type practiceLog struct {
	ID      string `json:"id,omitzero"`
	UserID  string `json:"user_id"`
	Piece   string `json:"piece"`
	Minutes int    `json:"minutes"`
}

// practiceLogsHandler serves GET /practice-logs: it verifies the request's
// bearer token, then returns the practice-log rows the verified user may see.
func practiceLogsHandler(client *supabase.Client) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		token, found := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
		if !found || token == "" {
			http.Error(writer, "missing bearer token", http.StatusUnauthorized)
			return
		}

		if _, _, _, err := client.Auth().GetClaims(request.Context(), token); err != nil {
			status := http.StatusUnauthorized
			var rejection *auth.Error
			if !errors.Is(err, auth.ErrExpiredJWT) &&
				!errors.Is(err, auth.ErrMalformedJWT) &&
				!errors.Is(err, auth.ErrInvalidSignature) &&
				!errors.As(err, &rejection) {
				status = http.StatusBadGateway // verification infrastructure failed, not the token
			}
			http.Error(writer, "token rejected", status)
			return
		}

		// The verified user's own token queries the Database, so Row Level
		// Security evaluates as that user and returns only their rows.
		userDatabase := client.Database().WithAccessTokenProvider(
			func(context.Context) (string, error) { return token, nil },
		)
		rows, _, err := postgrest.Collect(request.Context(), userDatabase,
			postgrest.From[practiceLog]("practice_logs"))
		if err != nil {
			http.Error(writer, "query failed", http.StatusBadGateway)
			return
		}

		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(rows); err != nil {
			http.Error(writer, "encoding failed", http.StatusInternalServerError)
		}
	})
}

func main() {
	projectURL := os.Getenv("SUPABASE_URL")
	apiKey := os.Getenv("SUPABASE_PUBLISHABLE_KEY")
	if projectURL == "" || apiKey == "" {
		fmt.Fprintln(os.Stderr, "SUPABASE_URL and SUPABASE_PUBLISHABLE_KEY must be set - run via scripts/integration-test.sh")
		os.Exit(1)
	}

	client, err := supabase.New(projectURL, apiKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, "supabase.New:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	userID, token, err := signUpDemoUser(ctx, projectURL, apiKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, "signing up demo user:", err)
		os.Exit(1)
	}

	// Seed one row as the demo user - the insert policy requires the row's
	// user_id to match the token's subject.
	userDatabase := client.Database().WithAccessTokenProvider(
		func(context.Context) (string, error) { return token, nil },
	)
	if _, err := postgrest.Execute(
		ctx, userDatabase,
		postgrest.From[practiceLog]("practice_logs").Insert(
			practiceLog{UserID: userID, Piece: "Prelude in C", Minutes: 30},
		),
	); err != nil {
		fmt.Fprintln(os.Stderr, "inserting practice log:", err)
		os.Exit(1)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "listening:", err)
		os.Exit(1)
	}
	server := &http.Server{Handler: practiceLogsHandler(client)}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Shutdown(ctx) }()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+listener.Addr().String()+"/practice-logs", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "building request:", err)
		os.Exit(1)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		fmt.Fprintln(os.Stderr, "calling backend:", err)
		os.Exit(1)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "backend answered %d: %s (%v)\n", response.StatusCode, body, err)
		os.Exit(1)
	}
	fmt.Printf("backend served the verified user's rows: %s", body)
}

// signUpDemoUser registers a throwaway user over the Auth REST API and returns
// its id and access token. The local stack auto-confirms email sign-ups, so
// the response carries a usable session immediately. An application would run
// its own sign-in flow instead; the SDK's session capabilities are the part of
// the Auth surface this backend does not need.
func signUpDemoUser(ctx context.Context, projectURL, apiKey string) (userID, accessToken string, err error) {
	email := fmt.Sprintf("rls-backend-%d@example.com", time.Now().UnixNano())
	payload, err := json.Marshal(map[string]string{"email": email, "password": "example-password"})
	if err != nil {
		return "", "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, projectURL+"/auth/v1/signup", bytes.NewReader(payload))
	if err != nil {
		return "", "", err
	}
	request.Header.Set("apikey", apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return "", "", fmt.Errorf("signup answered %d: %s", response.StatusCode, body)
	}
	var session struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		return "", "", err
	}
	return session.User.ID, session.AccessToken, nil
}
