package recover

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanDoesNotReadManifestAsRecoveryAuthority(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "missing", "daem.toml")

	_, err := Plan(context.Background(), PlanInput{ManifestPath: manifestPath})
	if err == nil {
		t.Fatalf("Plan returned nil error")
	}
	if !strings.Contains(err.Error(), "no active recovery journal") {
		t.Fatalf("error = %v, want recovery journal diagnostic", err)
	}
	if strings.Contains(err.Error(), "manifest") {
		t.Fatalf("error = %v, recover should not read manifest as authority", err)
	}
}
