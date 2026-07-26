package status

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestRunPreservesCanonicalTargetSelectionSentinel(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	writeTestFile(t, root, "daem.toml", "version = 1\ntargets = [\"codex\"]\n")

	_, err := Run(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		TargetValues: []string{"claude-code"},
	})
	if !errors.Is(err, targetselection.ErrInvalid) {
		t.Fatalf("Run error = %v, want targetselection.ErrInvalid", err)
	}
}
