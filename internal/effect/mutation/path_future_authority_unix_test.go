//go:build darwin || linux

package mutation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func TestFutureReferentAuthorityNeedsNoPayloadOrDirectoryReadPermission(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "search-only")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(parent, 0o700); err != nil {
			t.Error(err)
		}
	})
	file := filepath.Join(parent, "control\nname")
	if err := os.WriteFile(file, []byte("private"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o100); err != nil {
		t.Fatal(err)
	}

	for _, selected := range []string{parent, file} {
		oldBudget := &pathAuthorityTestBudget{limit: 100_000}
		resolved, err := rootedpath.ResolveFutureReferentPathBounded(selected, 256, oldBudget)
		if err != nil || resolved != selected {
			t.Fatalf("search-only referent = %q, %v; want %q", resolved, err, selected)
		}
		oldAuthority, err := ObserveDirectoryEntryAuthorityBounded(resolved, 256, oldBudget)
		if err != nil {
			t.Fatal(err)
		}
		got, authority, err := ObserveFutureReferentAuthorityBounded(selected, 256, &pathAuthorityTestBudget{limit: 100_000})
		if err != nil || got != selected || !authority.Equal(oldAuthority) {
			t.Fatalf("search-only combined observation = %q, %v; want %q and equal authority", got, err, selected)
		}
	}
}
