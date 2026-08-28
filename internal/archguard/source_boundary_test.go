package archguard

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSourceResolutionBatchDoesNotImportConcreteBackends(t *testing.T) {
	path := filepath.Join(findRepoRoot(t), "internal", "supply", "source", "resolution", "batch.go")
	file := parseSourceBoundaryFile(t, path)
	for _, imported := range file.Imports {
		importPath, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			t.Fatalf("unquote import %s: %v", imported.Path.Value, err)
		}
		if strings.Contains(importPath, "/internal/supply/source/backend/") {
			t.Fatalf("generic source batch imports concrete backend %q", importPath)
		}
	}
}

func TestSourceCacheDoesNotRegainSharedProcessGroup(t *testing.T) {
	root := filepath.Join(findRepoRoot(t), "internal", "supply", "source", "cache")
	if _, err := os.Stat(filepath.Join(root, "group.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retired source/cache/group.go exists or cannot be inspected: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(root, "*.go"))
	if err != nil {
		t.Fatalf("list source cache files: %v", err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file := parseSourceBoundaryFile(t, path)
		for _, declaration := range file.Decls {
			generic, ok := declaration.(*ast.GenDecl)
			if !ok || generic.Tok != token.TYPE {
				continue
			}
			for _, spec := range generic.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if ok && typeSpec.Name.Name == "Group" {
					t.Fatalf("source/cache exposes retired process coalescing type Group in %s", path)
				}
			}
		}
	}
}

func TestArtifactTreeWalkerDoesNotRescanParentDirectoryPerChild(t *testing.T) {
	path := filepath.Join(findRepoRoot(t), "internal", "supply", "artifact", "access", "native_hash_unix.go")
	file := parseSourceBoundaryFile(t, path)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "hashNativeDirectoryEntries" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			switch identifier.Name {
			case "verifyNativeEntry", "verifyNativeExactNameEntry":
				t.Fatalf("verified tree walker calls %s inside the child loop", identifier.Name)
			}
			return true
		})
		return
	}
	t.Fatal("hashNativeDirectoryEntries declaration is missing")
}

func parseSourceBoundaryFile(t *testing.T, path string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return file
}
