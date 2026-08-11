package execute

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
)

func TestRollbackScratchCleanupAdmitsMaximumDepthSourceBelowWrapper(t *testing.T) {
	base := t.TempDir()
	rollbackDir := filepath.Join(base, "rollback")
	backupPath := filepath.Join(rollbackDir, "000000")
	current := backupPath
	for depth := 1; depth <= recovery.MaximumArtifactTreeDepth; depth++ {
		current = filepath.Join(current, fmt.Sprintf("d%02d", depth))
	}
	if err := os.MkdirAll(current, 0o700); err != nil {
		t.Fatalf("create maximum-depth rollback stage: %v", err)
	}
	if err := os.WriteFile(filepath.Join(current, "empty"), nil, 0o600); err != nil {
		t.Fatalf("write zero-byte rollback leaf: %v", err)
	}
	work, err := recovery.NewArtifactWork(recovery.MaximumArtifactTreeDepth+1, 0)
	if err != nil {
		t.Fatalf("construct rollback stage work: %v", err)
	}
	paths := Paths{
		ManifestRoot:  base,
		StateDir:      filepath.Join(base, "state"),
		StatefilePath: filepath.Join(base, "state", "state.json"),
		RecoveryDir:   filepath.Join(base, "state", "recovery"),
		DataDir:       filepath.Join(base, "state", "data"),
	}
	authority, err := captureMutationAuthority(
		paths,
		true,
		nil,
		destinationResolver(paths),
		testFilesystem(),
	)
	if err != nil {
		t.Fatalf("capture rollback cleanup authority: %v", err)
	}
	defer func() {
		if err := authority.close(); err != nil {
			t.Errorf("close rollback cleanup authority: %v", err)
		}
	}()

	rollback := hostRollback{
		dir: rollbackDir,
		entries: []hostRollbackEntry{{
			existed: true, backupPath: backupPath, stagedWork: work,
		}},
	}
	if err := rollback.prepareCleanup(context.Background(), authority); err != nil {
		t.Fatalf("prepare maximum-depth rollback cleanup: %v", err)
	}
	if err := rollback.cleanup(context.Background(), authority); err != nil {
		t.Fatalf("execute maximum-depth rollback cleanup: %v", err)
	}
	if _, err := os.Stat(rollbackDir); !os.IsNotExist(err) {
		t.Fatalf("rollback stage stat error = %v, want absence", err)
	}
}

func TestRollbackScratchCleanupKeepsSourceDepthCeiling(t *testing.T) {
	if maximumRollbackScratchTreeDepth != recovery.MaximumArtifactTreeDepth+1 {
		t.Fatalf(
			"rollback scratch depth = %d, want source maximum plus one wrapper",
			maximumRollbackScratchTreeDepth,
		)
	}
	budget, err := recovery.NewPhysicalWorkBudget(0)
	if err != nil {
		t.Fatalf("construct recovery budget: %v", err)
	}
	limits, err := recoveryStagingTraversalLimits(budget)
	if err != nil {
		t.Fatalf("construct recovery staging limits: %v", err)
	}
	if limits.MaximumDepth() != recovery.MaximumArtifactTreeDepth {
		t.Fatalf(
			"source staging depth = %d, want %d",
			limits.MaximumDepth(),
			recovery.MaximumArtifactTreeDepth,
		)
	}
}
