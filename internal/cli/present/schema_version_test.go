package clipresent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaVersionAssignmentsUseContractRegistry(t *testing.T) {
	registryPath := filepath.Join("..", "..", "contractversion", "current.go")
	registry, err := parser.ParseFile(
		token.NewFileSet(),
		registryPath,
		nil,
		parser.SkipObjectResolution,
	)
	if err != nil {
		t.Fatalf("parse contract version registry: %v", err)
	}
	versions := cliJSONVersionNames(registry)
	usedVersions := make(map[string]struct{})

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
		ast.Inspect(file, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.KeyValueExpr:
				key, ok := value.Key.(*ast.Ident)
				if ok && key.Name == "SchemaVersion" {
					assertRegisteredSchemaVersion(t, entry.Name(), versions, usedVersions, value.Value)
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
					assertRegisteredSchemaVersion(t, entry.Name(), versions, usedVersions, value.Rhs[index])
				}
			}
			return true
		})
	}
	for version := range versions {
		if _, used := usedVersions[version]; !used {
			t.Errorf("contract version registry declares unused CLI JSON schema %s", version)
		}
	}
}

func cliJSONVersionNames(file *ast.File) map[string]struct{} {
	versions := make(map[string]struct{})
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
				if strings.HasSuffix(name.Name, "JSON") {
					versions[name.Name] = struct{}{}
				}
			}
		}
	}
	return versions
}

func assertRegisteredSchemaVersion(
	t *testing.T,
	filename string,
	versions map[string]struct{},
	usedVersions map[string]struct{},
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
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		t.Errorf("%s assigns SchemaVersion without the contract version registry", filename)
		return
	}
	packageName, ok := selector.X.(*ast.Ident)
	if !ok || packageName.Name != "contractversion" {
		t.Errorf("%s assigns SchemaVersion outside contractversion", filename)
		return
	}
	if _, registered := versions[selector.Sel.Name]; !registered {
		t.Errorf("%s assigns SchemaVersion from unregistered contractversion.%s", filename, selector.Sel.Name)
		return
	}
	usedVersions[selector.Sel.Name] = struct{}{}
}
