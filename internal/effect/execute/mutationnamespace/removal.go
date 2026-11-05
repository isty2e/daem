package mutationnamespace

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/removalnamespace"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func (namespace Namespace) ObserveRemoval(ctx context.Context, destination Destination, basis recovery.RemovalNamespaceAuthority, budget *physicalwork.Budget) (recovery.RemovalNamespaceObservation, error) {
	if err := ctx.Err(); err != nil {
		return recovery.RemovalNamespaceObservation{}, err
	}
	if !namespace.available() || destination.owner != namespace.state || !destination.IsRooted() {
		return recovery.RemovalNamespaceObservation{}, fmt.Errorf("removal namespace root authority is unavailable")
	}
	return removalnamespace.Observe(ctx, destination.root, destination.destination, basis, budget)
}

func (namespace Namespace) RequireRemoval(ctx context.Context, destination Destination, basis recovery.RemovalNamespaceAuthority, budget *physicalwork.Budget) error {
	observation, err := namespace.ObserveRemoval(ctx, destination, basis, budget)
	if err != nil {
		return err
	}
	if observation.Status() != recovery.RemovalNamespaceMatched {
		return fmt.Errorf("removal namespace is %s: %s", observation.Status(), observation.Detail())
	}
	return nil
}

// AcquireRemovalSlot confines the already-selected cleanup path to the destination
// root. Its caller selects the authorized private slot and owns mutation policy.
func (namespace Namespace) AcquireRemovalSlot(destination Destination, slotPath string, budget *physicalwork.Budget) (rootedpath.CommitCapability, error) {
	if !namespace.available() || destination.owner != namespace.state || !destination.IsRooted() {
		return nil, fmt.Errorf("removal slot root authority is unavailable")
	}
	authority, err := destination.root.AuthorityBounded(budget)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(authority.PhysicalRoot(), filepath.Clean(slotPath))
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("removal slot path %q escaped destination root", slotPath)
	}
	rootRelative, err := rootedpath.NewRelativeDestination(filepath.ToSlash(relative))
	if err != nil {
		return nil, err
	}
	bound, err := authority.Bind(rootRelative)
	if err != nil {
		return nil, err
	}
	return destination.root.AcquireBounded(bound, physicalwork.MaximumPhysicalPathDepth, budget)
}
