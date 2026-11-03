package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// PreparedProtectionStore owns read-only layout and work projection. Opening
// still requires fresh native authority; no descriptor or grant is retained.
type PreparedProtectionStore struct {
	layout      rootedpath.PathLayout
	observation mutation.DirectoryEntryObservationPlan
	depth       int
}

func PrepareProtectionStore(ctx context.Context, path string, depth int, budget mutation.PhysicalObservationBudget) (PreparedProtectionStore, error) {
	if ctx == nil || budget == nil {
		return PreparedProtectionStore{}, fmt.Errorf("protection store preparation requires context and allocation")
	}
	if err := ctx.Err(); err != nil {
		return PreparedProtectionStore{}, err
	}
	layout, err := rootedpath.CaptureDirectoryEntryLayoutBounded(path, depth, budget)
	if err != nil {
		return PreparedProtectionStore{}, err
	}
	observation, err := mutation.PrepareDirectoryEntryAuthorityBounded(ctx, layout.CanonicalPath(), depth, budget)
	if err != nil {
		return PreparedProtectionStore{}, err
	}
	return PreparedProtectionStore{layout: layout, observation: observation, depth: depth}, nil
}

// OpenBounded returns fresh authority that the caller must close.
func (prepared PreparedProtectionStore) OpenBounded(ctx context.Context, budget mutation.PhysicalObservationBudget) (Store, *rootedpath.CapturedRoot, error) {
	if ctx == nil || budget == nil {
		return Store{}, nil, fmt.Errorf("protection store opening requires context and allocation")
	}
	if err := ctx.Err(); err != nil {
		return Store{}, nil, err
	}
	root, destination, err := prepared.layout.CaptureDestinationBounded(prepared.depth, budget)
	if err != nil {
		return Store{}, nil, err
	}
	store, err := NewRooted(root, destination, prepared.depth, budget)
	if err != nil {
		return Store{}, nil, errors.Join(err, root.Close())
	}
	store.protectionObservation = &prepared.observation
	return store, root, nil
}

func (prepared PreparedProtectionStore) ReserveOpen(budget mutation.PhysicalObservationBudget) error {
	if err := prepared.layout.ReserveDestinationCapture(prepared.depth, budget); err != nil {
		return err
	}
	return prepared.layout.ReserveDestinationAccess(prepared.depth, budget)
}

// ReserveRead includes the existing registry payload envelope. Actual reads
// continue to charge their observed size, not this admission allowance.
func (prepared PreparedProtectionStore) ReserveRead(budget mutation.PhysicalObservationBudget) error {
	if err := prepared.layout.ReserveDestinationAccess(prepared.depth, budget); err != nil {
		return err
	}
	if err := prepared.observation.ReserveObservation(budget); err != nil {
		return err
	}
	return budget.AdmitPhysicalWork(0, 1, maximumOwnershipRegistryBytes)
}

func (prepared PreparedProtectionStore) ReserveTransition(budget mutation.PhysicalObservationBudget) error {
	if err := prepared.ReserveRead(budget); err != nil {
		return err
	}
	if err := prepared.observation.ReserveObservation(budget); err != nil {
		return err
	}
	if err := rootedpath.ChargeAbsolutePath(prepared.layout.CanonicalPath(), prepared.depth, budget); err != nil {
		return err
	}
	return budget.AdmitPhysicalWork(0, 1, maximumOwnershipRegistryBytes)
}
