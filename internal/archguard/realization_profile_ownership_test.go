package archguard

import (
	"go/ast"
	"strings"
	"testing"
)

func TestRealizationProfileDoesNotRestoreLegacyWideModels(t *testing.T) {
	legacy := map[string]struct{}{
		"ResourceLocation": {},
		"ResourceSurface":  {},
		"SurfaceBinding":   {},
		"SurfaceKind":      {},
	}
	root := findRepoRoot(t)
	walkProductionGoFiles(t, root, func(relativePath string, file *ast.File) {
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, specification := range general.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if _, forbidden := legacy[typeSpec.Name.Name]; forbidden {
					t.Errorf("%s declares retired wide profile model %s", relativePath, typeSpec.Name.Name)
				}
			}
		}
	})
}

func TestPlacementModelsDoNotOwnRouteOrDiscoveryFields(t *testing.T) {
	for _, typeName := range []string{"ManagedPathPlacement", "HookAssetPlacement"} {
		assertStructOmitsFields(t, typeName, map[string]struct{}{
			"adaptercontractversion": {},
			"importpolicy":           {},
			"priority":               {},
			"removerouteid":          {},
			"routeid":                {},
			"writerouteid":           {},
		})
	}
}

func assertStructOmitsFields(t *testing.T, typeName string, forbidden map[string]struct{}) {
	t.Helper()
	found := false
	root := findRepoRoot(t)
	walkProductionGoFiles(t, root, func(relativePath string, file *ast.File) {
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, specification := range general.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok || typeSpec.Name.Name != typeName {
					continue
				}
				structure, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					t.Errorf("%s declares %s as a non-struct", relativePath, typeName)
					continue
				}
				found = true
				for _, field := range structure.Fields.List {
					for _, name := range field.Names {
						if _, rejected := forbidden[strings.ToLower(name.Name)]; rejected {
							t.Errorf("%s gives %s forbidden field %s", relativePath, typeName, name.Name)
						}
					}
				}
			}
		}
	})
	if !found {
		t.Errorf("production tree does not declare %s", typeName)
	}
}
