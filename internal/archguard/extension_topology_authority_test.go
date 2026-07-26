package archguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtensionTopologyOwnsCarrierStructuralIdentity(t *testing.T) {
	root := findRepoRoot(t)
	tests := []struct {
		path      string
		forbidden []string
	}{
		{
			path: "internal/realization/lock/refine/extension.go",
			forbidden: []string{
				"topology.NewSubjectID",
			},
		},
		{
			path: "internal/realization/lock/delegated_relation_contract.go",
			forbidden: []string{
				"topology.NewSubjectID",
				"SubjectNamespace string",
			},
		},
		{
			path: "internal/realization/relation/relation.go",
			forbidden: []string{
				"topology.NewSubjectID",
				"type CarrierSubject",
				"type ContributionKind",
				"type ContributionSubject",
				"type ContributionSubjectSpec",
				"contributionIdentityKey",
			},
		},
	}

	for _, test := range tests {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(test.path)))
		if err != nil {
			t.Fatalf("read %s: %v", test.path, err)
		}
		for _, forbidden := range test.forbidden {
			if strings.Contains(string(content), forbidden) {
				t.Errorf("%s retains forbidden extension topology authority %q", test.path, forbidden)
			}
		}
	}
}

func TestExtensionTopologyCarrierNamespaceCatalogIsDataOnly(t *testing.T) {
	root := findRepoRoot(t)
	path := filepath.Join(root, "internal", "topology", "extension", "carrier_namespace_catalog.go")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read carrier namespace catalog: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, content, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse carrier namespace catalog: %v", err)
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch node.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			t.Errorf("carrier namespace catalog must contain data rows only, found %T", node)
			return false
		default:
			return true
		}
	})
}
