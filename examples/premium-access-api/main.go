package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/supabase/supabase-go/auth"
)

// backingTrack describes one of the licensed tracks the API serves.
type backingTrack struct {
	Piece string `json:"piece"`
	Tempo int    `json:"tempo"`
}

// backingTracks stands in for the API's own storage, which the Database never sees.
var backingTracks = []backingTrack{
	{Piece: "Prelude in C", Tempo: 72},
	{Piece: "Moonlight Sonata", Tempo: 54},
}

func main() {
	projectURL := os.Getenv("SUPABASE_PROJECT_URL")
	publishableKey := os.Getenv("SUPABASE_PUBLISHABLE_KEY")
	client, err := auth.New(projectURL, publishableKey)
	if err != nil {
		slog.Error("creating the client", "error", err)
		os.Exit(1)
	}

	http.HandleFunc("GET /backing-tracks", func(writer http.ResponseWriter, request *http.Request) {
		token, found := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
		if !found {
			slog.Warn("rejecting a request without a bearer token")
			http.Error(writer, "missing bearer token", http.StatusUnauthorized)
			return
		}

		// Ask Auth for the user as they are now, not as the token describes them.
		result, err := client.GetUser(request.Context(), token)
		var authError *auth.Error
		switch {
		case errors.As(err, &authError) && authError.HTTPStatus < http.StatusInternalServerError:
			slog.Warn("rejecting token", "error", err)
			http.Error(writer, "invalid token", http.StatusUnauthorized)
			return
		case err != nil:
			slog.Error("fetching the user", "error", err)
			http.Error(writer, "auth unavailable", http.StatusBadGateway)
			return
		}

		user := result.User()
		if user.AppMetadata()["plan"] != "premium" {
			slog.Info("refusing a user without the premium plan", "user", user.ID())
			http.Error(writer, "premium plan required", http.StatusForbidden)
			return
		}

		slog.Info("serving backing tracks", "user", user.ID())
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(backingTracks); err != nil {
			slog.Warn("writing the response", "error", err)
		}
	})

	server := &http.Server{Addr: "localhost:8080", ReadHeaderTimeout: 10 * time.Second}
	slog.Info("listening on http://localhost:8080")
	if err := server.ListenAndServe(); err != nil {
		slog.Error("serving", "error", err)
		os.Exit(1)
	}
}
