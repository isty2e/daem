package apply

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration/transaction"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestApplyPlanningFailsClosedOnInterruptedMetadataTransaction(t *testing.T) {
	t.Run("dry-run", func(t *testing.T) {
		manifestPath, stateDir := applyMetadataTransactionFixture(t)
		writeApplyMetadataTransactionMarker(t, stateDir)

		_, err := PlanDryRun(context.Background(), CommandInput{ManifestPath: manifestPath})
		assertApplyMetadataTransactionError(t, err)
	})

	t.Run("write", func(t *testing.T) {
		manifestPath, stateDir := applyMetadataTransactionFixture(t)
		writeApplyMetadataTransactionMarker(t, stateDir)

		prepared, err := PlanWrite(context.Background(), CommandInput{ManifestPath: manifestPath})
		if prepared != nil {
			t.Cleanup(func() { _ = prepared.Close() })
		}
		assertApplyMetadataTransactionError(t, err)
	})
}

func TestApplyExecutionRejectsMetadataTransactionStartedAfterPlanning(t *testing.T) {
	manifestPath, stateDir := applyMetadataTransactionFixture(t)
	prepared, err := PlanWrite(context.Background(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	writeApplyMetadataTransactionMarker(t, stateDir)

	_, err = ExecuteWithOptions(context.Background(), prepared, ExecuteOptions{})
	if err == nil {
		t.Fatal("ExecuteWithOptions succeeded after metadata transaction evidence appeared")
	}
}

func applyMetadataTransactionFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	manifestPath := filepath.Join(root, "daem.toml")
	writeApplyFile(t, manifestPath, "version = 1\ntargets = [\"codex\"]\n")
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{
		ManifestPath: manifestPath,
	}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	paths := applyTestPaths(t, root)
	return manifestPath, paths.StateDir
}

func writeApplyMetadataTransactionMarker(t *testing.T, stateDir string) {
	t.Helper()
	authorityPath, err := transaction.FileSetAuthorityPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(authorityPath, 0o700); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(stateDir, "absent-target")
	content := `{"version":1,"targets":[{"path":` + strconv.Quote(targetPath) +
		`,"before":{"exists":false},"write":false}]}`
	if err := os.WriteFile(filepath.Join(authorityPath, "transaction.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertApplyMetadataTransactionError(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
		t.Fatalf("error = %v, want interrupted file-set transaction", err)
	}
}
