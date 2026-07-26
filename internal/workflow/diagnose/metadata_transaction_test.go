package diagnoseworkflow

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestRunRefusesInterruptedMetadataTransactionBeforeDiagnostics(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	metadatatx.WriteInterrupted(t, paths.StateDir)
	counts, _ := installCountingManifestLoader(t)

	result, err := runCurrent(context.Background(), Input{
		ManifestPath:     manifestPath,
		ManifestExplicit: true,
		TargetExplicit:   true,
		TargetValues:     []string{"codex"},
	})
	if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
		t.Fatalf("error = %v, want interrupted file-set transaction", err)
	}
	if len(result.Checks) != 0 {
		t.Fatalf("metadata transaction ran diagnostic checks: %#v", result.Checks)
	}
	assertManifestStageCounts(t, counts, manifestStageCounts{})
}
