package status

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/declaration/transaction"
	daempaths "github.com/isty2e/daem/internal/paths"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestStatusFailsClosedOnInterruptedMetadataTransaction(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	manifestPath := filepath.Join(root, "daem.toml")
	writeTestFile(t, root, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	if _, err := workflowlock.RunLock(context.Background(), workflowlock.LockInput{
		ManifestPath: manifestPath,
	}); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	writeStatusMetadataTransactionMarker(t, paths.StateDir)

	_, err = Run(context.Background(), CommandInput{ManifestPath: manifestPath})
	if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
		t.Fatalf("error = %v, want interrupted file-set transaction", err)
	}
}

func writeStatusMetadataTransactionMarker(t *testing.T, stateDir string) {
	t.Helper()
	authorityPath, err := transaction.FileSetAuthorityPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(authorityPath, 0o700); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(stateDir, "absent-target")
	content := `{"version":` + strconv.Itoa(contractversion.MetadataTransaction) + `,"targets":[{"path":` + strconv.Quote(targetPath) +
		`,"before":{"exists":false},"write":false}]}`
	if err := os.WriteFile(filepath.Join(authorityPath, "transaction.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
