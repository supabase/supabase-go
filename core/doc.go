// Package core provides the shared HTTP pipeline, configuration and functional
// options that every Supabase domain module (postgrest, auth, storage and so on)
// is built on. It is the single module every other module in the SDK depends on,
// and it itself depends on nothing outside the Go standard library.
//
// core is part of the SDK's v1 compatibility surface. Most users never import it
// directly: they construct a client through the root supabase package, which
// composes core for them. It is exported because the scoped domain modules, when
// imported on their own, need a shared way to carry configuration and the request
// pipeline without depending on one another.
package core
