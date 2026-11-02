// Package unexportedreturn defines an analyzer for exported functions and
// methods whose results have types their package does not export.
package unexportedreturn

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer reports an exported function, or an exported method of an exported
// type, whose results include a type its package does not export. It skips
// test files and package main, and reports a function once, at the first such
// result.
var Analyzer = &analysis.Analyzer{
	Name: "unexportedreturn",
	Doc:  "report exported functions and methods that return unexported types",
	Run:  run,
}

// run checks the functions declared in the non-test files of pass's package,
// unless it is package main.
func run(pass *analysis.Pass) (any, error) {
	if pass.Pkg.Name() == "main" {
		return nil, nil
	}
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				checkFunc(pass, fn)
			}
		}
	}
	return nil, nil
}

// checkFunc reports fn's first result whose type is not exported, if fn is an
// exported function or an exported method of a type whose name is exported.
func checkFunc(pass *analysis.Pass, fn *ast.FuncDecl) {
	if !fn.Name.IsExported() || fn.Type.Results == nil {
		return
	}
	kind, name := "func", fn.Name.Name
	if fn.Recv != nil {
		receiver := receiverName(fn.Recv.List[0].Type)
		if !ast.IsExported(receiver) {
			return
		}
		kind, name = "method", receiver+"."+name
	}
	for _, result := range fn.Type.Results.List {
		typ := pass.TypesInfo.TypeOf(result.Type)
		if !exported(typ) {
			pass.Reportf(result.Type.Pos(), "exported %s %s returns unexported type %s, which can be annoying to use",
				kind, name, types.TypeString(typ, types.RelativeTo(pass.Pkg)))
			return
		}
	}
}

// receiverName returns the name of the type a receiver is declared with,
// without its pointer or type parameters.
func receiverName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiverName(e.X)
	case *ast.ParenExpr:
		return receiverName(e.X)
	case *ast.IndexExpr:
		return receiverName(e.X)
	case *ast.IndexListExpr:
		return receiverName(e.X)
	case *ast.Ident:
		return e.Name
	}
	return ""
}

// exported reports whether typ is exported. A named type or alias is exported
// when its name is exported or it is built in, and a map when its key and
// value types are. A pointer, slice, array or channel is exported when its
// element type is, and any other type always is.
func exported(typ types.Type) bool {
	switch t := typ.(type) {
	case *types.Alias:
		return exportedName(t.Obj())
	case *types.Named:
		return exportedName(t.Obj())
	case *types.Map:
		return exported(t.Key()) && exported(t.Elem())
	case interface{ Elem() types.Type }:
		return exported(t.Elem())
	}
	return true
}

// exportedName reports whether obj's name is exported or obj is built in.
func exportedName(obj *types.TypeName) bool {
	return obj.Pkg() == nil || obj.Exported()
}
