package lock

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
)

func TestLockConsumersFailClosedOnInterruptedMetadataTransaction(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	manifestPath := filepath.Join(root, "daem.toml")
	writeWorkflowTestFile(t, root, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")
	if _, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatalf("create baseline lockfile: %v", err)
	}
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	writeLockMetadataTransactionMarker(t, paths.StateDir)

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "dry-run",
			run: func() error {
				_, err := RunLock(context.Background(), LockInput{
					ManifestPath: manifestPath,
					DryRun:       true,
				})
				return err
			},
		},
		{
			name: "write",
			run: func() error {
				_, err := RunLock(context.Background(), LockInput{ManifestPath: manifestPath})
				return err
			},
		},
		{
			name: "outdated",
			run: func() error {
				_, err := RunOutdated(context.Background(), OutdatedInput{ManifestPath: manifestPath})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
				t.Fatalf("error = %v, want interrupted file-set transaction", err)
			}
		})
	}
}

func writeLockMetadataTransactionMarker(t *testing.T, stateDir string) {
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
