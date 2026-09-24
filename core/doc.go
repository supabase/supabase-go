// Package core provides common functionality that is shared between modules
// in the Supabase Go SDK.
//
// Consumers rarely import this package directly. The
// [github.com/supabase/supabase-go/core/configuration] package carries the
// client configuration and functional options shared by every domain module,
// and the [github.com/supabase/supabase-go/core/responses] package carries
// the HTTP error type that domain error types embed. This root package holds
// only the [ModulePath] identity constants that
// [github.com/supabase/supabase-go/core/configuration.New] takes.
package core
