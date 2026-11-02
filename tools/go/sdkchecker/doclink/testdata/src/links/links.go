// Package links holds doc links that render and doc links that do not.
package links

import "strings"

// Exported is a function.
func Exported() {}

// Type is a struct type.
type Type struct {
	// Field is a field.
	Field int
}

// Method is a method.
func (Type) Method() {}

// Interface is an interface type.
type Interface interface {
	// Method is an interface method.
	Method()
}

// Resolved links to [Exported], [*Type], [Type.Method], [Type.Field],
// [Interface.Method], [links.Exported], [strings.ToUpper] and the [Go]
// website.
//
// [Go]: https://go.dev
func Resolved() string { return strings.ToUpper("") }

// Prose holds brackets that are not doc links, a[i], [1], [optional] and
// []byte, and a code block that is not parsed for doc links:
//
//	value := [Absent]{}
func Prose() {}

// Symbol links to [Absent]. // want `renders as plain text: package links declares no Absent`
func Symbol() {}

// Member links to [Type.Absent]. // want `renders as plain text: package links declares no Type.Absent`
func Member() {}

// Elsewhere links to [elsewhere.Thing]. // want `renders as plain text: package links imports no package named elsewhere`
func Elsewhere() {}

// Imported links to [strings.Absent]. // want `links to nothing: package strings declares no Absent`
func Imported() {}

// Qualified links to [links.Absent]. // want `links to nothing: package links declares no Absent`
func Qualified() {}
