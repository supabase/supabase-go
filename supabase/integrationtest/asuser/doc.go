// Package asuser exercises the supabase module against the live local Supabase
// stack that scripts/integration-test.sh starts, composing it with the domain
// modules the way a consumer backend does, as a signed-in user: every client
// here holds the publishable key, and the secret key is absent from the
// package's environment.
package asuser
