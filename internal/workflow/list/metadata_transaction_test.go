package listworkflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestRunRefusesInterruptedMetadataTransactionBeforeManifestRead(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("[invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	metadatatx.WriteInterrupted(t, paths.StateDir)

	_, err = Run(context.Background(), Input{ManifestPath: manifestPath})
	if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
		t.Fatalf("error = %v, want interrupted file-set transaction before manifest decode", err)
	}
}

func TestRunOutputsRefusesInterruptedMetadataTransactionBeforeManifestRead(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("[invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	metadatatx.WriteInterrupted(t, paths.StateDir)

	_, err = RunOutputs(context.Background(), Input{ManifestPath: manifestPath})
	if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
		t.Fatalf("error = %v, want interrupted file-set transaction before manifest decode", err)
	}
}

func TestRunOutputsRefusesInterruptedApplyBeforeManifestRead(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(manifestPath, []byte("[invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(
		filepath.Join(root, ".daem", "recovery", "active-operation"),
		0o700,
	); err != nil {
		t.Fatal(err)
	}

	_, err := RunOutputs(context.Background(), Input{ManifestPath: manifestPath})
	if err == nil || !strings.Contains(err.Error(), "recovery inventory is blocked") {
		t.Fatalf("error = %v, want active recovery refusal before manifest decode", err)
	}
}
