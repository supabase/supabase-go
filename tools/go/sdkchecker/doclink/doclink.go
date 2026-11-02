// Package doclink defines an analyzer for doc links that pkg.go.dev renders
// as plain text or that point at nothing.
package doclink

import (
	"fmt"
	"go/ast"
	"go/doc"
	"go/doc/comment"
	"go/parser"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer reports, in the non-test files of a package, each doc link that
// pkg.go.dev renders as plain text and each rendered doc link that points at
// nothing. A link renders as plain text when it names a symbol the package
// does not document, or a package name that no import binds or that more than
// one import binds. A rendered link points at nothing when it names a symbol
// missing from the analyzed package or from a package the analyzed package
// imports, and a link into any other package goes unchecked. Bracketed text
// that does not end in a capitalized identifier reads as prose and is never
// reported.
var Analyzer = &analysis.Analyzer{
	Name: "doclink",
	Doc:  "report doc links that render as plain text or point at nothing",
	Run:  run,
}

// unresolved prefixes the import path that the permissive parser gives a
// package name the package's own parser cannot resolve.
const unresolved = "\x00"

// run parses every comment group in the non-test .go files of pass's package
// twice: once with the package's own doc comment parser, and once with a
// permissive one that accepts every symbol and package name. A package with no
// such file, such as the main package the go command generates for a test
// binary, has nothing to check.
func run(pass *analysis.Pass) (any, error) {
	var files []*ast.File
	for _, file := range pass.Files {
		name := pass.Fset.File(file.Pos()).Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		return nil, nil
	}
	actual, err := packageParser(pass, files)
	if err != nil {
		return nil, err
	}
	permissive := &comment.Parser{
		LookupSym: func(string, string) bool { return true },
		LookupPackage: func(name string) (string, bool) {
			if path, ok := actual.LookupPackage(name); ok {
				return path, true
			}
			if path, ok := comment.DefaultLookupPackage(name); ok {
				return path, true
			}
			return unresolved + name, true
		},
	}
	for _, file := range files {
		for _, group := range file.Comments {
			checkGroup(pass, files, actual, permissive, group)
		}
	}
	return nil, nil
}

// packageParser returns the doc comment parser of the documentation computed
// from files. It leaves files untouched, computing the documentation from
// copies parsed afresh from source.
func packageParser(pass *analysis.Pass, files []*ast.File) (*comment.Parser, error) {
	fset := token.NewFileSet()
	copies := make([]*ast.File, 0, len(files))
	for _, file := range files {
		name := pass.Fset.File(file.Pos()).Name()
		src, err := pass.ReadFile(name)
		if err != nil {
			return nil, err
		}
		copied, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		copies = append(copies, copied)
	}
	pkg, err := doc.NewFromFiles(fset, copies, pass.Pkg.Path())
	if err != nil {
		return nil, err
	}
	return pkg.Parser(), nil
}

// checkGroup reports each doc link in group that the actual parser renders as
// plain text or that points at nothing.
func checkGroup(pass *analysis.Pass, files []*ast.File, actual, permissive *comment.Parser, group *ast.CommentGroup) {
	text := group.Text()
	rendered := map[string]bool{}
	for _, link := range docLinks(actual.Parse(text)) {
		rendered[linkText(link)] = true
	}
	occurrences := map[string]int{}
	for _, link := range docLinks(permissive.Parse(text)) {
		if link.Name == "" {
			continue
		}
		source := linkText(link)
		occurrence := occurrences[source]
		occurrences[source]++
		var problem string
		if rendered[source] {
			problem = pointsAtNothing(pass, actual, link)
		} else {
			problem = plainText(pass, files, link)
		}
		if problem != "" {
			pass.Reportf(position(group, source, occurrence), "[%s] %s", source, problem)
		}
	}
}

// pointsAtNothing returns why a rendered link points at nothing, or "" when
// it points at a symbol or into a package the analyzed package does not
// import.
func pointsAtNothing(pass *analysis.Pass, actual *comment.Parser, link *comment.DocLink) string {
	name, declared := pass.Pkg.Name(), false
	if link.ImportPath == "" {
		declared = actual.LookupSym(link.Recv, link.Name)
	} else {
		imported := importedPackage(pass.Pkg, link.ImportPath)
		if imported == nil {
			return ""
		}
		name, declared = imported.Name(), declares(imported, link.Recv, link.Name)
	}
	if declared {
		return ""
	}
	return fmt.Sprintf("links to nothing: package %s declares no %s", name, symbol(link))
}

// plainText returns why a link renders as plain text.
func plainText(pass *analysis.Pass, files []*ast.File, link *comment.DocLink) string {
	name, qualified := strings.CutPrefix(link.ImportPath, unresolved)
	switch {
	case !qualified:
		return fmt.Sprintf("renders as plain text: package %s declares no %s", pass.Pkg.Name(), symbol(link))
	case importCount(pass, files, name) > 1:
		return fmt.Sprintf("renders as plain text: package %s imports more than one package named %s", pass.Pkg.Name(), name)
	default:
		return fmt.Sprintf("renders as plain text: package %s imports no package named %s", pass.Pkg.Name(), name)
	}
}

// importedPackage returns the package pkg imports from path, or nil if it
// imports none.
func importedPackage(pkg *types.Package, path string) *types.Package {
	for _, imported := range pkg.Imports() {
		if imported.Path() == path {
			return imported
		}
	}
	return nil
}

// declares reports whether pkg declares name or, given recv, a field or
// method name of its type recv.
func declares(pkg *types.Package, recv, name string) bool {
	if recv == "" {
		return pkg.Scope().Lookup(name) != nil
	}
	typ, ok := pkg.Scope().Lookup(recv).(*types.TypeName)
	if !ok {
		return false
	}
	obj, _, _ := types.LookupFieldOrMethod(typ.Type(), true, pkg, name)
	return obj != nil
}

// importCount returns how many distinct packages files import under name.
func importCount(pass *analysis.Pass, files []*ast.File, name string) int {
	paths := map[string]bool{}
	for _, file := range files {
		for _, spec := range file.Imports {
			if imported := pass.TypesInfo.PkgNameOf(spec); imported != nil && imported.Name() == name {
				paths[imported.Imported().Path()] = true
			}
		}
	}
	return len(paths)
}

// docLinks returns the doc links in d, in order.
func docLinks(d *comment.Doc) []*comment.DocLink {
	var links []*comment.DocLink
	collect := func(texts []comment.Text) {
		for _, text := range texts {
			if link, ok := text.(*comment.DocLink); ok {
				links = append(links, link)
			}
		}
	}
	for _, block := range d.Content {
		switch b := block.(type) {
		case *comment.Paragraph:
			collect(b.Text)
		case *comment.List:
			for _, item := range b.Items {
				for _, content := range item.Content {
					if paragraph, ok := content.(*comment.Paragraph); ok {
						collect(paragraph.Text)
					}
				}
			}
		}
	}
	return links
}

// linkText returns the text between a doc link's brackets.
func linkText(link *comment.DocLink) string {
	var text strings.Builder
	for _, t := range link.Text {
		if plain, ok := t.(comment.Plain); ok {
			text.WriteString(string(plain))
		}
	}
	return text.String()
}

// symbol returns the symbol a doc link names, without its package.
func symbol(link *comment.DocLink) string {
	if link.Recv == "" {
		return link.Name
	}
	return link.Recv + "." + link.Name
}

// position returns the position of an occurrence, counted from zero, of
// source within brackets in group's raw text, or the position of group when
// there is no such occurrence.
func position(group *ast.CommentGroup, source string, occurrence int) token.Pos {
	bracketed := "[" + source + "]"
	for _, c := range group.List {
		for offset := 0; ; {
			i := strings.Index(c.Text[offset:], bracketed)
			if i < 0 {
				break
			}
			if occurrence == 0 {
				return c.Slash + token.Pos(offset+i)
			}
			occurrence--
			offset += i + len(bracketed)
		}
	}
	return group.Pos()
}
