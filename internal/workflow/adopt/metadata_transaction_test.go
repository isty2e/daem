package adopt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestBuildCommandPlanRefusesInterruptedMetadataTransaction(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	metadatatx.WriteInterrupted(t, paths.StateDir)

	_, err = BuildCommandPlan(context.Background(), CommandInput{
		TargetValues: []string{"codex"},
		ManifestPath: manifestPath,
		SourceDir:    filepath.Join(root, "import"),
	})
	if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
		t.Fatalf("error = %v, want interrupted file-set transaction", err)
	}
}

func TestExecuteCommandPlanRefusesMetadataTransactionStartedAfterPlanning(
	t *testing.T,
) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(
		filepath.Join(root, "AGENTS.md"),
		[]byte("# Agents\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	previousWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWorkingDirectory); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	planned, err := BuildCommandPlan(context.Background(), CommandInput{
		TargetValues: []string{"codex"},
		ManifestPath: manifestPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	metadatatx.WriteInterrupted(t, paths.StateDir)

	_, err = ExecuteCommandPlan(context.Background(), planned, nil)
	if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
		t.Fatalf("error = %v, want interrupted file-set transaction", err)
	}
}
