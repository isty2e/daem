//go:build !darwin && !linux && !windows

package mutation

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type unsupportedControlPathBudget struct{}

func (unsupportedControlPathBudget) AdmitPathComponents(int) error { return nil }

func TestControlBearingRecoveryPathFailsClosedWithoutRootedTraversal(t *testing.T) {
	path := filepath.Join(string(filepath.Separator), "control\npath", "recovery")
	_, err := ResolveDirectoryEntryPathBounded(path, 256, unsupportedControlPathBudget{})
	var failure *rootedpath.Failure
	if !errors.As(err, &failure) || failure.Kind() != rootedpath.FailureUnsupportedPlatform {
		t.Fatalf("ResolveDirectoryEntryPathBounded error = %v, want %s", err, rootedpath.FailureUnsupportedPlatform)
	}
}
