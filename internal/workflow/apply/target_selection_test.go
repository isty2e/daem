package apply

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	lock "github.com/isty2e/daem/internal/realization/lock"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestPlanDryRunPreservesCanonicalTargetSelectionSentinel(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	writeApplyManifestFile(t, manifestPath)
	writeApplyLockfile(t, lockfilePath, lock.File{Version: lock.CurrentVersion})

	_, err := PlanDryRun(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{"claude-code"},
	})
	if !errors.Is(err, targetselection.ErrInvalid) {
		t.Fatalf("PlanDryRun error = %v, want targetselection.ErrInvalid", err)
	}
}
