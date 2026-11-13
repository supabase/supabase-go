// Command rls-backend proves the code in the RLS walkthrough,
// docs/RLS Walkthrough.md. The walkthrough shows this program's main.go in
// full, and scripts/integration-test.sh fails unless the two match line for
// line. The script then runs the program against a local Supabase stack and
// makes the walkthrough's calls. Each answer must match the one the
// walkthrough documents.
//
// The program serves GET /practice-logs on localhost:8080. It verifies each
// request's bearer token with Auth, then reads the practice_logs table as
// that user, so Row Level Security returns only the user's own rows. It reads
// SUPABASE_PROJECT_URL and SUPABASE_PUBLISHABLE_KEY from the environment.
package main
