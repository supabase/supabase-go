package clash

import "text/template"

// Text escapes HTML for text/template, but [template.New] names neither template package. // want `renders as plain text: package clash imports more than one package named template`
var Text = template.HTMLEscapeString
