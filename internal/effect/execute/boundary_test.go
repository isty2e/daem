package execute

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func destinationResolver(paths Paths) mutationnamespace.Resolver {
	return func(destination output.Destination, _ rootedpath.PhysicalTraversalBudget) (string, error) {
		return hostpath.NewResolverWithManagedDataRoot(paths.ManifestRoot, paths.DataDir).Resolve(destination)
	}
}

func journalDestinationResolver(paths Paths) func(output.Destination) (string, error) {
	return func(destination output.Destination) (string, error) {
		return destinationResolver(paths)(destination, nil)
	}
}

func testSelectedTargets(t *testing.T, values ...target.Target) reconcile.SelectedTargets {
	t.Helper()
	selected, err := reconcile.NewSelectedTargets(values)
	if err != nil {
		t.Fatalf("NewSelectedTargets returned error: %v", err)
	}
	return selected
}
