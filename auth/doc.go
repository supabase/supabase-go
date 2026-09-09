// Package auth is the Supabase Auth client for server-side JWT verification.
// A [Client] verifies the end-user access tokens a backend receives - on the
// Authorization header of an inbound request, say - and fetches the profile of
// the user a token authenticates.
//
// [Client.GetClaims] is the primary entry point. It returns a token's verified
// [Claims] - who the user is and how they authenticated - having checked the
// signature against the project's signing keys:
//
//	claims, err := client.GetClaims(ctx, token)
//	if err != nil {
//	    // reject the request
//	}
//	// claims.Subject() is the verified user ID
//
// A token signed with one of the project's asymmetric signing keys (ES256,
// RS256 or EdDSA) is verified locally against the key set published at the
// project's /.well-known/jwks.json endpoint, cached in the client, so the
// common path needs no per-request round trip to the Auth server. A token the
// key set cannot verify locally - one signed with the legacy shared secret, or
// carrying an unrecognized key id - is verified by the Auth server, the same
// round trip [Client.GetUser] makes.
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
package auth
