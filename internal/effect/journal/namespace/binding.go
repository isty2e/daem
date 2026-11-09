package namespace

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// WriterBinding requalifies the aliases before exposing their existing borrowed
// writer root and binding. It does not acquire ownership or execution leases.
func (namespace DocumentNamespace) WriterBinding(ctx context.Context, budget rootedpath.PhysicalTraversalBudget) (*rootedpath.CapturedRoot, rootedpath.Destination, error) {
	current, err := observeNativeDocumentNamespace(ctx, namespace.inputs, namespace.roots, namespace.protection, namespace.filesystem, budget, &namespace.writerPin)
	if err != nil {
		return nil, rootedpath.Destination{}, err
	}
	if !namespace.observation.Equal(current.observation) || !namespace.physical.equal(current.physical) {
		return nil, rootedpath.Destination{}, fmt.Errorf("native document writer namespace changed")
	}
	return current.writerRoot, current.writer, nil
}
