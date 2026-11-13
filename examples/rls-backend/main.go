package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/supabase/supabase-go/postgrest"
	"github.com/supabase/supabase-go/supabase"
)

// practiceLog is a row of the practice_logs table.
type practiceLog struct {
	Piece   string `json:"piece"`
	Minutes int    `json:"minutes"`
}

func main() {
	projectURL := os.Getenv("SUPABASE_PROJECT_URL")
	publishableKey := os.Getenv("SUPABASE_PUBLISHABLE_KEY")
	client, err := supabase.New(projectURL, publishableKey)
	if err != nil {
		slog.Error("creating the client", "error", err)
		os.Exit(1)
	}

	http.HandleFunc("GET /practice-logs", func(writer http.ResponseWriter, request *http.Request) {
		token, found := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
		if !found {
			http.Error(writer, "missing bearer token", http.StatusUnauthorized)
			return
		}

		result, err := client.Auth().GetClaims(request.Context(), token)
		if err != nil {
			slog.Warn("rejecting token", "error", err)
			http.Error(writer, "invalid token", http.StatusUnauthorized)
			return
		}
		slog.Info("serving practice logs", "user", result.Claims().Subject())

		// Query as the verified user, so Row Level Security returns only their rows.
		practiceLogs, _, err := postgrest.Collect(
			request.Context(),
			client.Database(),
			postgrest.From[practiceLog]("practice_logs").Select("piece, minutes"),
			postgrest.WithAccessTokenProvider(
				func(context.Context) (string, error) { return token, nil },
			),
		)
		if err != nil {
			slog.Error("querying practice logs", "error", err)
			http.Error(writer, "query failed", http.StatusBadGateway)
			return
		}

		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(practiceLogs); err != nil {
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
