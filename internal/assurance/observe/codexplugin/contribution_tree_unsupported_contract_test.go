package codexplugin

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestUnsupportedTreeAdapterSourceFailsClosedWithoutPathnameReopen(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("contribution_tree_unsupported.go")
	if err != nil {
		t.Fatal(err)
	}
	normalizedSource := strings.ReplaceAll(string(source), "\r\n", "\n")
	if !strings.HasPrefix(normalizedSource, "//go:build !darwin && !linux && !windows\n") {
		t.Fatal("contribution_tree_unsupported.go must compile only when Darwin/Linux/Windows tree adapters are absent")
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), "contribution_tree_unsupported.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range parsed.Imports {
		path := strings.Trim(spec.Path.Value, `"`)
		switch path {
		case "errors", "os":
		default:
			t.Fatalf("unsupported tree adapter imports %q, want no pathname packages", path)
		}
	}

	required := map[string]bool{
		"openChildDirectoryNoFollow": false,
		"classifyChild":              false,
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil {
			continue
		}
		if _, known := required[function.Name.Name]; !known {
			t.Fatalf("unsupported tree adapter defines extra function %s", function.Name.Name)
		}
		required[function.Name.Name] = true
		assertUnsupportedTreeAdapterFunction(t, function)
	}
	for name, seen := range required {
		if !seen {
			t.Fatalf("contribution_tree_unsupported.go must define %s", name)
		}
	}
}

func assertUnsupportedTreeAdapterFunction(t *testing.T, function *ast.FuncDecl) {
	t.Helper()
	var returnedUnsupported bool
	ast.Inspect(function, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.CallExpr:
			if !allowedUnsupportedTreeAdapterCall(typed) {
				t.Fatalf("%s calls %s, want validation only then errDescriptorRelativeTreeUnsupported", function.Name.Name, callName(typed))
			}
		case *ast.SelectorExpr:
			if typed.Sel.Name == "Name" || typed.Sel.Name == "Join" || typed.Sel.Name == "Open" || typed.Sel.Name == "Lstat" {
				t.Fatalf("%s uses pathname %s", function.Name.Name, typed.Sel.Name)
			}
		case *ast.ReturnStmt:
			if returnsTreeUnsupported(typed) {
				returnedUnsupported = true
			}
		}
		return true
	})
	if !returnedUnsupported {
		t.Fatalf("%s must return errDescriptorRelativeTreeUnsupported", function.Name.Name)
	}
}

func allowedUnsupportedTreeAdapterCall(call *ast.CallExpr) bool {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name == "validDirentComponent"
	case *ast.SelectorExpr:
		return fun.Sel.Name == "New"
	default:
		return false
	}
}

func returnsTreeUnsupported(stmt *ast.ReturnStmt) bool {
	if len(stmt.Results) != 2 {
		return false
	}
	ident, ok := stmt.Results[1].(*ast.Ident)
	return ok && ident.Name == "errDescriptorRelativeTreeUnsupported"
}

func callName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		if ident, ok := fun.X.(*ast.Ident); ok {
			return ident.Name + "." + fun.Sel.Name
		}
		return fun.Sel.Name
	default:
		return "unknown"
	}
}
