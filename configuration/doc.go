// Package configuration provides the shared project configuration, functional
// options and authenticating HTTP pipeline that every Supabase domain module
// (postgrest, auth, storage, and so on) is built on. It is the single module
// every other module in the SDK depends on, and it itself depends on nothing
// outside the Go standard library.
//
// configuration is part of the SDK's v1 compatibility surface. Most users never
// import it directly: they construct a client through the root supabase package,
// which composes this module for them. A domain module imported on its own, such
// as [github.com/supabase/supabase-go/postgrest], is constructed from a
// [Configuration] built directly with this package.
package configuration
