package native

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

func TestRecordedPointResolutionDoesNotChooseALaterPreferredFile(t *testing.T) {
	home, work := t.TempDir(), t.TempDir()
	locations, err := NewLocations(work, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := output.NewRootReference("claude-account", "claude-account-point-v2")
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := locations.PointCandidates(reference)
	if err != nil || candidates != [2]string{filepath.Join(home, ".claude", ".config.json"), filepath.Join(home, ".claude.json")} {
		t.Fatal("point locator lost its ordered literal alternatives", candidates, err)
	}
	if err := os.MkdirAll(filepath.Dir(candidates[0]), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidates[0], []byte("preferred now exists"), 0o600); err != nil {
		t.Fatal(err)
	}
	selected, err := locations.ResolvePoint(reference, configuration.PointFallback)
	if err != nil || selected != candidates[1] {
		t.Fatal("frozen point resolution migrated to the active choice", selected, err)
	}
	if _, err := locations.Resolve(reference); err == nil {
		t.Fatal("point locator was silently interpreted as a directory")
	}
	if _, err := locations.ResolvePoint(reference, configuration.PointAlternative("future")); err == nil {
		t.Fatal("unknown recorded alternative was defaulted")
	}
}
