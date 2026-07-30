package clipresent

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandJSONSchemaVersionOwners(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  int
		want int
	}{
		{name: "version", got: versionJSONSchemaVersion, want: 1},
		{name: "init", got: initJSONSchemaVersion, want: 1},
		{name: "manifest authoring", got: manifestAuthoringJSONSchemaVersion, want: 2},
		{name: "lock comparison", got: lockJSONSchemaVersion, want: 3},
		{name: "resource inventory", got: listResourcesJSONSchemaVersion, want: 1},
		{name: "output inventory", got: listOutputsJSONSchemaVersion, want: 3},
		{name: "reconciliation plan", got: planJSONSchemaVersion, want: 10},
		{name: "apply result", got: applyResultJSONSchemaVersion, want: 15},
		{name: "recovery", got: recoveryJSONSchemaVersion, want: 3},
		{name: "doctor", got: doctorJSONSchemaVersion, want: 1},
		{name: "MCP probe", got: mcpProbeJSONSchemaVersion, want: 1},
		{name: "extension refresh", got: refreshJSONSchemaVersion, want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if test.got != test.want {
				t.Fatalf("schema version = %d, want %d", test.got, test.want)
			}
		})
	}
}

func TestCLIReferenceDocumentsApplyResultSchemaVersion(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "docs", "cli.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CLI reference: %v", err)
	}
	want := fmt.Sprintf(
		"| confirmed `apply` | Apply result | `%d` |",
		applyResultJSONSchemaVersion,
	)
	if !strings.Contains(string(content), want) {
		t.Fatalf("CLI reference is missing current apply schema row %q", want)
	}
}

func TestSchemaVersionAssignmentsUseEnvelopeOwners(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read presenter package: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		owners := schemaVersionOwners(file)
		usedOwners := make(map[string]struct{})
		ast.Inspect(file, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.KeyValueExpr:
				key, ok := value.Key.(*ast.Ident)
				if ok && key.Name == "SchemaVersion" {
					assertSchemaVersionOwner(t, entry.Name(), owners, usedOwners, value.Value)
				}
			case *ast.AssignStmt:
				for index, left := range value.Lhs {
					selector, ok := left.(*ast.SelectorExpr)
					if !ok || selector.Sel.Name != "SchemaVersion" {
						continue
					}
					if index >= len(value.Rhs) {
						t.Errorf("%s assigns SchemaVersion through an unsupported multi-value expression", entry.Name())
						continue
					}
					assertSchemaVersionOwner(t, entry.Name(), owners, usedOwners, value.Rhs[index])
				}
			}
			return true
		})
		for owner := range owners {
			if _, used := usedOwners[owner]; !used {
				t.Errorf("%s declares unused schema version owner %s", entry.Name(), owner)
			}
		}
	}
}

func schemaVersionOwners(file *ast.File) map[string]struct{} {
	owners := make(map[string]struct{})
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, specification := range general.Specs {
			value, ok := specification.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range value.Names {
				if strings.HasSuffix(name.Name, "JSONSchemaVersion") {
					owners[name.Name] = struct{}{}
				}
			}
		}
	}
	return owners
}

func assertSchemaVersionOwner(
	t *testing.T,
	filename string,
	owners map[string]struct{},
	usedOwners map[string]struct{},
	expression ast.Expr,
) {
	t.Helper()
	for {
		parenthesized, ok := expression.(*ast.ParenExpr)
		if !ok {
			break
		}
		expression = parenthesized.X
	}
	identifier, ok := expression.(*ast.Ident)
	if !ok {
		t.Errorf("%s assigns SchemaVersion without its envelope owner constant", filename)
		return
	}
	if _, owned := owners[identifier.Name]; !owned {
		t.Errorf("%s assigns SchemaVersion from non-local owner %s", filename, identifier.Name)
		return
	}
	usedOwners[identifier.Name] = struct{}{}
}
