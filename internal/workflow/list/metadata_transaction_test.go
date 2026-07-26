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
