package lock

import (
	"context"
	"path/filepath"
	"testing"
)

func TestExtensionOrderExampleBuildsTwoOrderConstraints(t *testing.T) {
	manifestPath := filepath.Join(
		"..",
		"..",
		"..",
		"examples",
		"extension-order.toml",
	)
	result, err := RunLock(context.Background(), LockInput{
		ManifestPath: manifestPath,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("lock extension-order example: %v", err)
	}
	constraints := result.Lockfile.Locked.OrderConstraints()
	if len(constraints) != 2 {
		t.Fatalf("order constraints = %#v, want Pi and OpenCode", constraints)
	}
}
