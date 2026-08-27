//go:build darwin

package rootedpath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDestinationRejectsEmptyAliasTargetAsDangling(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(root, "empty")
	if err := os.Symlink("", alias); err != nil {
		t.Fatal(err)
	}

	budget := &boundedCaptureTestBudget{limit: 1 << 20}
	_, err := ResolveDestinationPathBounded(
		filepath.Join(alias, "state", "recovery"),
		256,
		budget,
	)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty alias target error = %v, want dangling not-exist identity", err)
	}
}
