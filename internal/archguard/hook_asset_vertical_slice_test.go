package archguard

import (
	"errors"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestHookOwnsCommandAssetReferenceGrammar(t *testing.T) {
	root := findRepoRoot(t)
	const owner = "internal/desired/hook/asset_reference.go"
	walkProductionGoFiles(t, root, func(relativePath string, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				return true
			}
			if (strings.Contains(value, "hook_file:") || strings.Contains(value, "hook_dir:")) &&
				relativePath != owner {
				t.Errorf("%s owns Hook command asset-reference grammar outside %s", relativePath, owner)
			}
			return true
		})
	})
}

func TestHookAssetVerticalSliceCannotRegainLegacyOperationalAuthority(t *testing.T) {
	root := findRepoRoot(t)
	legacyProjector := filepath.Join(root, "internal", "output", "project", "hookasset")
	if _, err := os.Stat(legacyProjector); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retired HookAsset DesiredOutput projector exists or cannot be inspected: %v", err)
	}

	for _, packagePath := range []string{
		"internal/reconcile",
		"internal/effect/execute",
		"internal/effect/journal",
		"internal/assurance/observe/live",
		"internal/workflow/apply",
	} {
		assertProductionPackageOmits(t, root, packagePath, []string{
			"ResourceKindHookAsset",
			"KindHookAsset",
		})
	}

	for _, check := range []struct {
		path      string
		forbidden []string
	}{
		{
			path: filepath.Join(root, "internal", "assurance", "observe", "lock", "hookasset.go"),
			forbidden: []string{
				"ResourceKindHookAsset",
				"HookAssetPathProjectionContract",
				"HookAssetPlacementFor",
				"internal/realization/profile",
			},
		},
		{
			path: filepath.Join(root, "internal", "effect", "payload", "build", "hook_asset.go"),
			forbidden: []string{
				"ResourceKindHookAsset",
				"HookAssetPathProjectionContract",
				"target/selection",
			},
		},
	} {
		content, err := os.ReadFile(check.path)
		if err != nil {
			t.Fatalf("read HookAsset convergence consumer %q: %v", check.path, err)
		}
		for _, forbidden := range check.forbidden {
			if strings.Contains(string(content), forbidden) {
				t.Fatalf("HookAsset convergence consumer %q regained legacy authority through %q", check.path, forbidden)
			}
		}
	}

	managedPathReadiness := filepath.Join(root, "internal", "workflow", "readiness", "managed_path.go")
	content, err := os.ReadFile(managedPathReadiness)
	if err != nil {
		t.Fatalf("read managed path readiness assembly: %v", err)
	}
	for _, forbidden := range []string{
		"internal/desired/hookasset",
		".HookAssets()",
		"HookAssetPathProjectionContract",
		"KindHookAsset",
	} {
		if strings.Contains(string(content), forbidden) {
			t.Fatalf("generic managed path readiness assembly regained HookAsset refinement %q", forbidden)
		}
	}
}
