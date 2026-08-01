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
