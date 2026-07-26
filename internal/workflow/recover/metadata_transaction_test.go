package recover

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestRecoverRefusesInterruptedMetadataTransaction(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	metadatatx.WriteInterrupted(t, paths.StateDir)

	_, err = Plan(context.Background(), PlanInput{ManifestPath: manifestPath})
	if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
		t.Fatalf("error = %v, want interrupted file-set transaction", err)
	}
}
