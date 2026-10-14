// Command admin-provisioning is a back-office operator tool built on the
// composed Supabase client. It does one stage of a user account's lifecycle per
// invocation, so each stage can be run and examined at the operator's own
// pace - inspecting the project between stages, for example - rather than as
// one scripted sweep:
//
//	go run . onboard ada@example.com     provision the account and mint its onboarding link
//	go run . reconcile                   walk every user, as a nightly audit would
//	go run . offboard ada@example.com    ban sign-ins, then delete the account
//
// It authenticates with the project's secret API key, which bypasses Row
// Level Security and so belongs to server-controlled environments alone -
// read from the environment, never from source. This is the other half of
// the trust boundary the rls-backend example shows: there, a backend acts as
// one signed-in user with that user's token; here, an operator tool acts on
// the project itself - the Auth admin surface managing accounts and the
// Database client reading the profiles mirror as the service role - and no
// end-user token appears anywhere.
//
// It runs against the local Supabase stack started by
// scripts/integration-test.sh (which walks all three verbs in order), reading
// SUPABASE_URL and SUPABASE_SECRET_KEY from the environment. The minted link
// prints to stdout, standing in for the application's own mailer: a real job
// hands the link to its delivery system and logs it nowhere, because the
// link signs in whoever follows it.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/supabase/supabase-go/auth"
	"github.com/supabase/supabase-go/core/pagination"
	"github.com/supabase/supabase-go/postgrest"
	"github.com/supabase/supabase-go/supabase"
)

// profile maps a row of public.profiles, the mirror this project maintains of
// auth.users by trigger - the Managing User Data pattern from the Supabase
// docs. Application code resolves accounts through the mirror, never through
// the auth schema, which PostgREST does not expose.
type profile struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func main() {
	projectURL := os.Getenv("SUPABASE_URL")
	secretKey := os.Getenv("SUPABASE_SECRET_KEY")
	if projectURL == "" || secretKey == "" {
		fmt.Fprintln(os.Stderr, "SUPABASE_URL and SUPABASE_SECRET_KEY must be set - run via scripts/integration-test.sh or against your own project")
		os.Exit(1)
	}

	client, err := supabase.New(projectURL, secretKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, "supabase.New:", err)
		os.Exit(1)
	}
	admin := client.Auth().Admin()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	switch {
	case len(os.Args) == 3 && os.Args[1] == "onboard":
		err = onboard(ctx, admin, os.Args[2])
	case len(os.Args) == 2 && os.Args[1] == "reconcile":
		err = reconcile(ctx, admin)
	case len(os.Args) == 3 && os.Args[1] == "offboard":
		err = offboard(ctx, admin, client.Database(), os.Args[2])
	default:
		fmt.Fprintln(os.Stderr, "usage: go run . {onboard <email> | reconcile | offboard <email>}")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// onboard provisions the user's account and mints the link their onboarding
// email carries. Admin creation sends no email: EmailConfirm marks
// the address trusted on arrival, and the account's first password arrives
// through the recovery link, so none is set here.
func onboard(ctx context.Context, admin *auth.Admin, email string) error {
	user, err := admin.CreateUser(ctx, auth.UserAttributes{
		Email:        email,
		EmailConfirm: true,
		AppMetadata:  map[string]any{"role": "support"},
	})
	if err != nil {
		return fmt.Errorf("creating user: %w", err)
	}
	fmt.Println("provisioned", user.Email(), "as", user.AppMetadata()["role"])

	// Following the link proves the address and has the user choose
	// their password. Printing stands in for the application's mailer.
	link, err := admin.GenerateLink(ctx, auth.NewLinkRequest(email).Recovery())
	if err != nil {
		return fmt.Errorf("minting recovery link: %w", err)
	}
	fmt.Println("deliver to", email+":", link.ActionLink())
	return nil
}

// reconcile walks every page of the project's users and prints one line per
// account, the shape of a nightly audit of the project. Each follow-up
// call restates the page size, because the server computes page boundaries
// from the size each request states.
func reconcile(ctx context.Context, admin *auth.Admin) error {
	const pageSize = 50
	total := 0
	page := 1
	for {
		users, err := admin.ListUsers(ctx, pagination.WithPage(page), pagination.WithSize(pageSize))
		if err != nil {
			return fmt.Errorf("listing users: %w", err)
		}
		for _, user := range users.Users() {
			role := "none"
			if value, ok := user.AppMetadata()["role"].(string); ok {
				role = value
			}
			fmt.Printf("%s role=%s created=%s\n", user.Email(), role, user.CreatedAt().Format(time.RFC3339))
			total++
		}
		next, ok := users.NextPage()
		if !ok {
			break
		}
		page = next
	}
	fmt.Println(total, "users reconciled")
	return nil
}

// offboard resolves the user's id from the profiles mirror - one
// PostgREST query as the service role - then bans sign-ins so access ends the
// moment the decision is made, and deletes the account. Deletion also removes
// the account's identities, sessions and factors, and the mirror row goes
// with it through its foreign key; rows other services hold against the id
// are governed by those services' own schemas.
func offboard(ctx context.Context, admin *auth.Admin, database *postgrest.Client, email string) error {
	user, found, _, err := postgrest.CollectSingleMaybe(ctx, database,
		postgrest.From[profile]("profiles").Select("id").Eq("email", email))
	if err != nil {
		return fmt.Errorf("resolving %s: %w", email, err)
	}
	if !found {
		return fmt.Errorf("no user holds %s", email)
	}

	if _, err := admin.UpdateUser(ctx, user.ID, auth.UserAttributes{BanDuration: "24h"}); err != nil {
		return fmt.Errorf("banning user: %w", err)
	}
	if err := admin.DeleteUser(ctx, user.ID); err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}
	fmt.Println("offboarded", email)
	return nil
}
