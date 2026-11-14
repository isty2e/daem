package statefile

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// ReadOptions binds filesystem loading to an operation-selected codec and path budget.
type ReadOptions struct {
	Codec              PhysicalCodec
	PhysicalPathBudget rootedpath.PhysicalTraversalBudget
}

// LoadOptionalWithOptions reads complete state with the selected codec. Missing
// files and exact empty retirement records retain their ordinary empty meaning.
func LoadOptionalWithOptions(ctx context.Context, path string, options ReadOptions) (durable.Snapshot, error) {
	state, err := LoadOptionalPhysicalWithOptions(ctx, path, options)
	if err != nil {
		return durable.Snapshot{}, err
	}
	return state.Snapshot(), nil
}
