//go:build darwin || linux

package mutation

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type controlPathCountingBudget struct {
	components int
}

func (budget *controlPathCountingBudget) AdmitPathComponents(count int) error {
	if count < 0 {
		return fmt.Errorf("negative path-component charge")
	}
	budget.components += count
	return nil
}

func TestBoundedDirectoryEntryResolversChargeControlBearingAliasExpansion(t *testing.T) {
	tests := []struct {
		name    string
		resolve func(string, *controlPathCountingBudget) (string, error)
	}{
		{
			name: "canonical mutation path",
			resolve: func(path string, budget *controlPathCountingBudget) (string, error) {
				return CanonicalDirectoryEntryPathBounded(path, 256, budget)
			},
		},
		{
			name: "search-only recovery path",
			resolve: func(path string, budget *controlPathCountingBudget) (string, error) {
				return ResolveDirectoryEntryPathBounded(path, 256, budget)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			shortPath, shortTarget := controlBearingAliasPath(t, 1)
			shortBudget := &controlPathCountingBudget{}
			shortResolved, err := test.resolve(shortPath, shortBudget)
			if err != nil {
				t.Fatalf("resolve short alias: %v", err)
			}
			if shortResolved != shortTarget {
				t.Fatalf("short resolved path = %q, want %q", shortResolved, shortTarget)
			}

			longPath, longTarget := controlBearingAliasPath(t, 24)
			longBudget := &controlPathCountingBudget{}
			longResolved, err := test.resolve(longPath, longBudget)
			if err != nil {
				t.Fatalf("resolve long alias: %v", err)
			}
			if longResolved != longTarget {
				t.Fatalf("long resolved path = %q, want %q", longResolved, longTarget)
			}
			if longBudget.components < shortBudget.components+20 {
				t.Fatalf(
					"long alias charged %d components, short charged %d; alias expansion was not fully charged",
					longBudget.components,
					shortBudget.components,
				)
			}
		})
	}
}

func controlBearingAliasPath(t *testing.T, aliases int) (string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	controlRoot := filepath.Join(root, "control\npath")
	target := filepath.Join(controlRoot, "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	for index := aliases - 1; index >= 0; index-- {
		name := fmt.Sprintf("alias-%02d", index)
		targetName := "target"
		if index+1 < aliases {
			targetName = fmt.Sprintf("alias-%02d", index+1)
		}
		if err := os.Symlink(targetName, filepath.Join(controlRoot, name)); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(controlRoot, "alias-00", "recovery"), filepath.Join(target, "recovery")
}
