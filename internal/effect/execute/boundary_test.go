package execute

import (
	"testing"

	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func destinationResolver(paths Paths) DestinationResolver {
	return DestinationResolver(
		hostpath.NewResolverWithManagedDataRoot(
			paths.ManifestRoot,
			paths.DataDir,
		).Resolve,
	)
}

func testSelectedTargets(t *testing.T, values ...target.Target) reconcile.SelectedTargets {
	t.Helper()
	selected, err := reconcile.NewSelectedTargets(values)
	if err != nil {
		t.Fatalf("NewSelectedTargets returned error: %v", err)
	}
	return selected
}
