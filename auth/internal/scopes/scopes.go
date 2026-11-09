// Package scopes holds the sign-out scope vocabulary: the values the Auth
// server's logout endpoint takes as its scope query parameter.
package scopes

const (
	// Global revokes every session the user holds.
	Global = "global"

	// Local revokes only the session the presented token belongs to.
	Local = "local"

	// Others revokes every session except the one the presented token
	// belongs to.
	Others = "others"
)
