package archguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const artifactImportPath = "github.com/isty2e/daem/internal/supply/artifact"

func TestContentHashHasOneCanonicalTypeOwner(t *testing.T) {
	root := findRepoRoot(t)
	var owners []string
	walkProductionGoFiles(t, root, func(relativePath string, parsed *ast.File) {
		for _, declaration := range parsed.Decls {
			generic, ok := declaration.(*ast.GenDecl)
			if !ok || generic.Tok != token.TYPE {
				continue
			}
			for _, specification := range generic.Specs {
				typed, ok := specification.(*ast.TypeSpec)
				if ok && typed.Name.Name == "ContentHash" {
					owners = append(owners, relativePath)
				}
			}
		}
	})
	if len(owners) != 1 || owners[0] != "internal/supply/artifact/model.go" {
		t.Fatalf("ContentHash type owners = %v, want [internal/supply/artifact/model.go]", owners)
	}
}

func TestAssuranceAndReconciliationUseArtifactPackageOnlyForContentHash(t *testing.T) {
	root := findRepoRoot(t)
	walkProductionGoFiles(t, root, func(relativePath string, parsed *ast.File) {
		packagePath := filepath.ToSlash(filepath.Dir(relativePath))
		block := semanticDependencyBlockForPackage(packagePath)
		if block != dependencyAssurance && block != dependencyReconciliation {
			return
		}

		qualifiers := artifactImportQualifiers(t, relativePath, parsed)
		if len(qualifiers) == 0 {
			return
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			qualifier, ok := selector.X.(*ast.Ident)
			if !ok || !qualifiers[qualifier.Name] {
				return true
			}
			if selector.Sel.Name != "ContentHash" {
				t.Errorf(
					"%s uses artifact.%s; Assurance and Reconciliation may import only artifact.ContentHash",
					relativePath,
					selector.Sel.Name,
				)
			}
			return true
		})
	})
}

func artifactImportQualifiers(t *testing.T, relativePath string, parsed *ast.File) map[string]bool {
	t.Helper()
	qualifiers := make(map[string]bool)
	for _, imported := range parsed.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			t.Fatalf("%s has invalid import path %q: %v", relativePath, imported.Path.Value, err)
		}
		if path != artifactImportPath {
			continue
		}
		qualifier := "artifact"
		if imported.Name != nil {
			qualifier = imported.Name.Name
		}
		if qualifier == "." || qualifier == "_" {
			t.Fatalf("%s must use a finite qualifier for %s", relativePath, artifactImportPath)
		}
		qualifiers[qualifier] = true
	}
	return qualifiers
}

func walkProductionGoFiles(t *testing.T, root string, visit func(string, *ast.File)) {
	t.Helper()
	internalRoot := filepath.Join(root, "internal")
	err := filepath.WalkDir(internalRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, content, 0)
		if err != nil {
			return err
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		visit(filepath.ToSlash(relativePath), parsed)
		return nil
	})
	if err != nil {
		t.Fatalf("walk production Go files: %v", err)
	}
}
