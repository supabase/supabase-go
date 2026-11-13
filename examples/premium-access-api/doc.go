// Command premium-access-api proves the code in the Premium Access
// walkthrough, docs/Premium Access Walkthrough.md. The walkthrough shows this
// program's main.go in full, and scripts/integration-test.sh fails unless the
// two match line for line. The script then runs the program against a local
// Supabase stack and makes the walkthrough's calls. Each answer must match
// the one the walkthrough documents.
//
// The program serves GET /backing-tracks on localhost:8080 to users on the
// premium plan. It asks Auth for each request's user as they are now, so a
// plan change or a sign-out takes effect on the next request. It reads
// SUPABASE_PROJECT_URL and SUPABASE_PUBLISHABLE_KEY from the environment.
package main
