package hostmutation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/effect/workmeasure"
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
	work, err := workmeasure.NewArtifactWork(recovery.MaximumArtifactTreeDepth+1, 0)
	if err != nil {
		t.Fatalf("construct rollback stage work: %v", err)
	}
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatalf("construct rollback cleanup budget: %v", err)
	}
	scratch := &RollbackScratch{rollbackScratchState: &rollbackScratchState{
		directory: rollbackDir, filesystem: storagecommit.Adapter{},
	}}
	if err := scratch.PrepareCleanup(context.Background(), budget, []ScratchArtifact{{Path: backupPath, Work: work}}); err != nil {
		t.Fatalf("prepare maximum-depth rollback cleanup: %v", err)
	}
	if err := scratch.Cleanup(context.Background()); err != nil {
		t.Fatalf("execute maximum-depth rollback cleanup: %v", err)
	}
	if _, err := os.Stat(rollbackDir); !os.IsNotExist(err) {
		t.Fatalf("rollback stage stat error = %v, want absence", err)
	}
	if err := scratch.Cleanup(context.Background()); err != nil {
		t.Fatalf("repeated completed cleanup: %v", err)
	}
}

func TestRollbackScratchAbortRetainsItsAdmittedCleanupPlan(t *testing.T) {
	scratch, err := CreateRollbackScratch(storagecommit.Adapter{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(scratch.Directory()) })
	backupPath := filepath.Join(scratch.Directory(), "000000")
	content := []byte("before")
	if err := os.WriteFile(backupPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	work, err := workmeasure.NewArtifactWork(0, int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	artifacts := []ScratchArtifact{{Path: backupPath, Work: work}}
	copied := *scratch
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := scratch.PrepareCleanup(t.Context(), budget, artifacts); err != nil {
		t.Fatal(err)
	}
	unexpected := filepath.Join(scratch.Directory(), "unexpected")
	if err := os.WriteFile(unexpected, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	emptyWork, err := workmeasure.NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	expanded := append(artifacts, ScratchArtifact{Path: unexpected, Work: emptyWork})
	if err := copied.Abort(t.Context(), expanded); err == nil {
		t.Fatal("prepared abort expanded its admitted cleanup authority from later artifact facts")
	}
	if _, err := os.Stat(unexpected); err != nil {
		t.Fatalf("unexpected scratch entry changed after refused cleanup: %v", err)
	}
	if _, err := os.Stat(scratch.Directory()); err != nil {
		t.Fatalf("scratch directory changed after refused cleanup: %v", err)
	}
}

func TestRollbackScratchCleanupRefusesSourceBeyondDepthCeiling(t *testing.T) {
	rollbackDir := filepath.Join(t.TempDir(), "rollback")
	backupPath := filepath.Join(rollbackDir, "000000")
	current := backupPath
	for depth := 1; depth <= recovery.MaximumArtifactTreeDepth+1; depth++ {
		current = filepath.Join(current, fmt.Sprintf("d%02d", depth))
	}
	if err := os.MkdirAll(current, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(current, "empty"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	work, err := workmeasure.NewArtifactWork(recovery.MaximumArtifactTreeDepth+2, 0)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	scratch := &RollbackScratch{rollbackScratchState: &rollbackScratchState{
		directory: rollbackDir, filesystem: storagecommit.Adapter{},
	}}
	if err := scratch.PrepareCleanup(t.Context(), budget, []ScratchArtifact{{Path: backupPath, Work: work}}); err != nil {
		t.Fatal(err)
	}
	if err := scratch.Cleanup(t.Context()); err == nil {
		t.Fatal("scratch cleanup admitted a source deeper than the staged-source ceiling")
	}
	if _, err := os.Stat(rollbackDir); err != nil {
		t.Fatalf("over-depth scratch root changed after refused cleanup: %v", err)
	}
}

func TestRollbackAbortCleanupUsesExactStagedWorkEnvelope(t *testing.T) {
	rollbackDir := filepath.Join(t.TempDir(), "rollback")
	if err := os.Mkdir(rollbackDir, 0o700); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(rollbackDir, "000000")
	content := []byte("before")
	if err := os.WriteFile(backupPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	work, err := workmeasure.NewArtifactWork(0, int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	scratch := &RollbackScratch{rollbackScratchState: &rollbackScratchState{
		directory: rollbackDir, filesystem: storagecommit.Adapter{},
	}}
	if err := scratch.Abort(t.Context(), []ScratchArtifact{{Path: backupPath, Work: work}}); err != nil {
		t.Fatalf("abort rollback cleanup: %v", err)
	}
	if _, err := os.Stat(rollbackDir); !os.IsNotExist(err) {
		t.Fatalf("rollback stage stat error = %v, want absence", err)
	}
}
