package filesnapshot_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestUnsupportedAtAdapterSourceFailsClosedWithoutPathnameReopen(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("read_at_unsupported.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(source), "//go:build !darwin && !linux && !freebsd && !netbsd && !openbsd && !windows\n") {
		t.Fatal("read_at_unsupported.go must compile only when supported Unix At-adapters are absent")
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), "read_at_unsupported.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range parsed.Imports {
		path := strings.Trim(spec.Path.Value, `"`)
		switch path {
		case "context", "fmt", "os":
		default:
			t.Fatalf("unsupported At-adapter imports %q, want no pathname or I/O packages", path)
		}
	}

	function := unsupportedAtAdapterFunction(t, parsed)
	var returnedUnsupported bool
	ast.Inspect(function, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.CallExpr:
			if !allowedUnsupportedAtAdapterCall(typed) {
				t.Fatalf("unsupported At-adapter calls %s, want validation only then ErrUnsupported", callName(typed))
			}
		case *ast.ReturnStmt:
			if returnsBareUnsupported(typed) {
				returnedUnsupported = true
			}
		}
		return true
	})
	if !returnedUnsupported {
		t.Fatal("unsupported At-adapter must return bare ErrUnsupported")
	}
}

func unsupportedAtAdapterFunction(t *testing.T, file *ast.File) *ast.FuncDecl {
	t.Helper()
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "readRegularFileAtCounted" || function.Recv != nil {
			continue
		}
		return function
	}
	t.Fatal("read_at_unsupported.go must define readRegularFileAtCounted")
	return nil
}

func allowedUnsupportedAtAdapterCall(call *ast.CallExpr) bool {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name == "validDirentName"
	case *ast.SelectorExpr:
		return fun.Sel.Name == "Err" || fun.Sel.Name == "Errorf"
	default:
		return false
	}
}

func returnsBareUnsupported(stmt *ast.ReturnStmt) bool {
	if len(stmt.Results) != 2 {
		return false
	}
	ident, ok := stmt.Results[1].(*ast.Ident)
	return ok && ident.Name == "ErrUnsupported"
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
