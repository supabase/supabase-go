// Command auth-session-service is a long-running service signed in as one
// user: it signs in once with the credentials the environment holds, keeps
// the session fresh with AutoRefresh for as long as it runs, logs every
// session event and signs out when it stops.
//
// It runs against the local Supabase stack started by
// scripts/integration-test.sh, reading SUPABASE_URL,
// SUPABASE_PUBLISHABLE_KEY, SERVICE_EMAIL and SERVICE_PASSWORD from the
// environment. It stops on SIGINT or SIGTERM, or once the duration its
// optional -run-for flag gives has elapsed.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/auth/signout"
	"golang.org/x/sync/errgroup"
)

func main() {
	runFor := flag.Duration("run-for", 0, "how long to run for, or until interrupted when zero")
	flag.Parse()

	projectURL := os.Getenv("SUPABASE_URL")
	publishableKey := os.Getenv("SUPABASE_PUBLISHABLE_KEY")
	email := os.Getenv("SERVICE_EMAIL")
	password := os.Getenv("SERVICE_PASSWORD")
	if projectURL == "" || publishableKey == "" || email == "" || password == "" {
		fmt.Fprintln(os.Stderr, "SUPABASE_URL, SUPABASE_PUBLISHABLE_KEY, SERVICE_EMAIL and SERVICE_PASSWORD must be set - run via scripts/integration-test.sh")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *runFor > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *runFor)
		defer cancel()
	}

	client, err := auth.New(projectURL, publishableKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, "auth.New:", err)
		os.Exit(1)
	}
	if err := run(ctx, client, email, password); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run signs in, keeps the session fresh and verifies its token periodically
// until ctx is done, then signs out. It returns an error when the session
// ends on its own or any step fails.
func run(ctx context.Context, client *auth.Client, email, password string) error {
	result, err := client.SignInWithPassword(ctx, auth.PasswordCredentials{Email: email, Password: password})
	if err != nil {
		return fmt.Errorf("signing in: %w", err)
	}
	session := result.Session()
	log.Printf("signed in as %s", session.User().Email())

	remove := session.AddEventListener(func(event auth.SessionEvent) {
		if event.Name() == auth.SessionEventSignedOut {
			log.Printf("session event: %s (%s)", event.Name(), event.SignOutReason())
			return
		}
		log.Printf("session event: %s", event.Name())
	})
	defer remove()

	// groupContext is cancelled when ctx is, or when either goroutine fails.
	group, groupContext := errgroup.WithContext(ctx)
	group.Go(func() error { return session.AutoRefresh(groupContext) })
	group.Go(func() error { return verifyPeriodically(groupContext, client, session) })
	err = group.Wait()

	// The loop has returned, so no refresh can land after the sign-out, which
	// runs on a context of its own because the service's has ended.
	signOutContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, signOutErr := session.SignOut(signOutContext, signout.WithLocalScope()); signOutErr != nil {
		log.Printf("signing out: %v", signOutErr)
	}
	return err
}

// verifyPeriodically reads the session's access token at once and then every
// five seconds, verifying each with GetClaims, until ctx is done. It returns
// nil once ctx is done and the error of a read or verification that failed.
func verifyPeriodically(ctx context.Context, client *auth.Client, session *auth.Session) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		accessToken, err := session.AccessToken(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return fmt.Errorf("reading the access token: %w", err)
		}
		claims, err := client.GetClaims(ctx, accessToken)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return fmt.Errorf("verifying the access token: %w", err)
		}
		log.Printf("token verified for %s, expiring %s", claims.Claims().Subject(), claims.Claims().ExpiresAt().Format(time.RFC3339))

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
