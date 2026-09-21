// Package auth is the Supabase Auth client for server-side [JWT] verification.
// A [Client] verifies the end-user access tokens a backend receives - on the
// [Authorization header] of an inbound request, say - and fetches the profile of
// the user a token authenticates.
//
// [Client.GetClaims] is the primary entry point. It returns a token's verified
// [Claims] - who the user is and how they authenticated - together with the
// token's decoded [JWTHeader] and signature bytes, having checked that
// signature against the project's signing keys:
//
//	claims, _, _, err := client.GetClaims(ctx, token)
//	if err != nil {
//	    // reject the request
//	}
//	// claims.Subject() is the verified user ID
//
// A token signed with one of the Supabase project's asymmetric [signing keys]
// (ES256, RS256 or EdDSA) is verified against a locally cached copy of the key
// set published at the project's remote /.well-known/jwks.json endpoint, which
// means that the most common path needs no further per-request round trip to
// the Auth server. A token the key set cannot verify locally (that is, one
// signed with the legacy shared secret, or carrying an unrecognized key id) is
// verified by the remote Supabase Auth server (the same round trip
// [Client.GetUser] makes).
//
// Local verification works because Supabase signs tokens with an asymmetric
// key pair. The remote Supabase Auth server holds the private half and signs
// each access token it mints, and the project publishes only the public,
// not-secret halves at its /.well-known/jwks.json endpoint (a JWK Set as
// defined by RFC 7517).
//
// [Client.GetUser] fetches the authenticated user's current profile from the
// Auth server, which verifies the token as part of serving the request. It is
// a round trip every time and returns server-fresh data, so prefer
// [Client.GetClaims] on request paths that only need verified claims.
//
// The two methods carry different trust models by design. GetClaims establishes
// identity from the token itself, a snapshot taken when the token was minted.
// GetUser reads the authoritative profile as it stands now. Reach for GetUser
// when a decision turns on data that may have changed since sign-in - a fresh
// email-confirmation state, updated metadata - and for GetClaims otherwise.
//
// To act on the database as the verified user afterwards - so Row Level
// Security policies apply - attach the same token to a Database client through
// its access-token provider. Verify at the edge with this package, then let the
// verified token authorize the query.
//
// A Client verifies with the project API key alone and never holds signing-key
// secrets: local verification uses only the public keys the project publishes.
// Construct one Client per project and share it, since it caches those keys
// internally. A Client is safe for concurrent use by multiple goroutines.
//
// Every call takes a [context.Context] and honors its deadline and
// cancellation end to end. Transport-level timeouts belong to the caller's
// [net/http.Client], supplied through [configuration.WithHTTPClient]. Auth
// requests are made once per call: the automatic-retry option
// ([configuration.WithRetry]) has no effect on this module today, because
// verification sits on request-handling hot paths where invisible backoff
// multiplies caller latency.
//
// Note: The term "minted" refers to the process that the Supabase Auth server
// performs to create a JSON Web Token - that is, generating the token,
// cryptographically signing it and then issuing it to the user.
//
// [JWT]: https://supabase.com/docs/guides/auth/jwts
// [Authorization header]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Authorization
// [signing keys]: https://supabase.com/docs/guides/auth/signing-keys
package auth
