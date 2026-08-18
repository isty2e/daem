//go:build darwin

package platform

import (
	"context"
	"testing"
)

func TestCurrentRecordsNativeMacOSProductVersion(t *testing.T) {
	observation, err := Current(context.Background())
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	version, observed := observation.Version()
	if !observed {
		t.Fatalf("native product version was not observed: reason=%s", observation.Reason())
	}
	t.Logf("observed macOS product version %s", version)
}

func TestCurrentParsesProductVersionWhenAmbientSecretMatchesOutput(t *testing.T) {
	first, err := Current(context.Background())
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	version, observed := first.Version()
	if !observed {
		t.Fatalf("native product version was not observed: reason=%s", first.Reason())
	}
	t.Setenv("PR66_API_KEY", version.String())
	observation, err := Current(context.Background())
	if err != nil {
		t.Fatalf("Current with matching ambient secret: %v", err)
	}
	got, observed := observation.Version()
	if !observed || got.String() != version.String() {
		t.Fatalf("version = %s,%t, want %s after ambient secret matched output", got, observed, version)
	}
}
