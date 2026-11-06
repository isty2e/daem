// Package controlwitness guards recovery effects against changes to their
// admitted statefile and ownership Registry evidence.
package controlwitness

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output/ownership"
)

// Guard owns one operation's classification witness and next-effect checks.
// Its control roots and Registry store remain borrowed from that operation.
type Guard struct {
	filesystem      mutationfs.RootedReader
	statefile       entryBinding
	registryEntry   entryBinding
	snapshot        Snapshot
	registry        ownershipmutation.RegistryReader
	successor       ownership.Registry
	hasSuccessor    bool
	validating      bool
	nativeCheck     func(context.Context) error
	executionBudget *physicalwork.Budget
}

func New(filesystem mutationfs.RootedReader) *Guard {
	return &Guard{filesystem: filesystem}
}

type entryBinding struct {
	root        *rootedpath.CapturedRoot
	destination rootedpath.Destination
}

func (binding entryBinding) valid() bool {
	return binding.root != nil && binding.destination.Validate() == nil
}

type entryWitness struct {
	identity mutationfs.EntryIdentity
	exists   bool
}

func (witness entryWitness) equal(other entryWitness) bool {
	if witness.exists != other.exists {
		return false
	}
	if !witness.exists {
		return true
	}
	return witness.identity != nil && other.identity != nil &&
		witness.identity.Equal(other.identity)
}

// Snapshot is source-bound observation evidence, not permission to perform effects.
type Snapshot struct {
	source       *Guard
	statefile    entryWitness
	ownership    entryWitness
	hasOwnership bool
	initialized  bool
}

func (witness Snapshot) equal(other Snapshot) bool {
	return witness.source != nil && witness.source == other.source &&
		witness.initialized && other.initialized &&
		witness.hasOwnership == other.hasOwnership &&
		witness.statefile.equal(other.statefile) &&
		(!witness.hasOwnership || witness.ownership.equal(other.ownership))
}

type pathReservation struct {
	budget *physicalwork.Budget
}

func (reservation pathReservation) AdmitPathComponents(count int) error {
	return reservation.budget.ReserveSemanticPathComponents(count)
}

func (guard *Guard) StatefileBound() bool {
	return guard != nil && guard.statefile.valid()
}

func (guard *Guard) BindStatefile(root *rootedpath.CapturedRoot, destination rootedpath.Destination) error {
	if guard == nil || root == nil || destination.Validate() != nil {
		return fmt.Errorf("recovery semantic authority is unavailable")
	}
	if guard.statefile.valid() {
		return fmt.Errorf("recovery statefile semantic authority is already bound")
	}
	guard.statefile = entryBinding{
		root: root, destination: destination,
	}
	return nil
}

func (guard *Guard) BindOwnership(
	root *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
	registry ownershipmutation.RegistryReader,
) error {
	if guard == nil || root == nil || destination.Validate() != nil || registry == nil {
		return fmt.Errorf("recovery ownership semantic authority is invalid")
	}
	if guard.registryEntry.valid() {
		return fmt.Errorf("recovery ownership semantic authority is already bound")
	}
	guard.registryEntry = entryBinding{
		root: root, destination: destination,
	}
	guard.registry = registry
	return nil
}

func (binding entryBinding) observe(
	ctx context.Context,
	filesystem mutationfs.RootedReader,
	budget rootedpath.PhysicalTraversalBudget,
) (entryWitness, error) {
	if !binding.valid() {
		return entryWitness{}, fmt.Errorf("recovery semantic entry authority is unavailable")
	}
	if filesystem == nil || budget == nil {
		return entryWitness{}, fmt.Errorf("recovery semantic observation capability is unavailable")
	}
	capability, err := binding.root.AcquireBounded(
		binding.destination,
		physicalwork.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		return entryWitness{}, err
	}
	identity, observeErr := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	closeErr := capability.Close()
	if errors.Is(observeErr, fs.ErrNotExist) {
		return entryWitness{}, closeErr
	}
	if observeErr != nil || closeErr != nil {
		return entryWitness{}, errors.Join(observeErr, closeErr)
	}
	if identity == nil || identity.Kind() == mutationfs.EntryKindInvalid {
		return entryWitness{}, fmt.Errorf("recovery semantic entry identity is unavailable")
	}
	return entryWitness{identity: identity, exists: true}, nil
}

func (guard *Guard) Observe(
	ctx context.Context,
	budget rootedpath.PhysicalTraversalBudget,
) (Snapshot, error) {
	if guard == nil || !guard.statefile.valid() {
		return Snapshot{}, fmt.Errorf("recovery statefile semantic authority is unavailable")
	}
	statefile, err := guard.statefile.observe(ctx, guard.filesystem, budget)
	if err != nil {
		return Snapshot{}, fmt.Errorf("observe recovery statefile semantics: %w", err)
	}
	witness := Snapshot{source: guard, statefile: statefile, initialized: true}
	if guard.registryEntry.valid() {
		ownership, err := guard.registryEntry.observe(ctx, guard.filesystem, budget)
		if err != nil {
			return Snapshot{}, fmt.Errorf("observe recovery ownership semantics: %w", err)
		}
		witness.ownership = ownership
		witness.hasOwnership = true
	}
	return witness, nil
}

func (guard *Guard) Establish(
	before Snapshot,
	after Snapshot,
) error {
	if guard == nil {
		return fmt.Errorf("recovery semantic authority is unavailable")
	}
	if guard.snapshot.initialized {
		return fmt.Errorf("recovery semantic witness was already established")
	}
	if before.source != guard || !before.equal(after) {
		return errors.Join(
			mutation.StaleSnapshotError{},
			fmt.Errorf("recovery statefile or ownership semantics changed while classifying the plan"),
		)
	}
	guard.snapshot = after
	return nil
}

func (guard *Guard) ValidatePair(
	before Snapshot,
	after Snapshot,
	detail string,
) error {
	if guard == nil || !guard.snapshot.initialized {
		return fmt.Errorf("recovery semantic witness is unavailable")
	}
	if !guard.snapshot.equal(before) {
		return errors.Join(
			mutation.StaleSnapshotError{},
			fmt.Errorf("recovery statefile or ownership semantics changed %s", detail),
		)
	}
	if !before.equal(after) {
		return errors.Join(
			mutation.StaleSnapshotError{},
			fmt.Errorf("recovery statefile or ownership semantics changed while %s", detail),
		)
	}
	return nil
}

func (guard *Guard) AdmitRegistrySuccessor(
	ctx context.Context,
	next ownership.Registry,
) error {
	if guard == nil || !guard.snapshot.initialized ||
		!guard.registryEntry.valid() {
		return fmt.Errorf("recovery ownership semantic authority is unavailable")
	}
	current, err := guard.Observe(
		ctx,
		guard.executionBudget,
	)
	if err != nil {
		return err
	}
	if !guard.snapshot.statefile.equal(current.statefile) {
		return errors.Join(
			mutation.StaleSnapshotError{},
			fmt.Errorf("recovery statefile semantics changed while advancing ownership"),
		)
	}
	if !current.hasOwnership {
		return fmt.Errorf("recovery ownership successor has no persisted authority")
	}
	guard.snapshot.ownership = current.ownership
	guard.successor = next
	guard.hasSuccessor = true
	return nil
}

func (guard *Guard) ValidateRegistrySuccessor(ctx context.Context) error {
	if guard == nil || !guard.hasSuccessor {
		return nil
	}
	if guard.registry == nil {
		return fmt.Errorf("recovery ownership registry is unavailable")
	}
	current, err := guard.registry.Load(ctx)
	if err != nil {
		return fmt.Errorf("load recovery ownership successor: %w", err)
	}
	if !guard.successor.Equal(current) {
		return errors.Join(
			mutation.StaleSnapshotError{},
			fmt.Errorf("recovery ownership registry differs from its admitted convergence successor"),
		)
	}
	return nil
}

func (guard *Guard) Validate(ctx context.Context) error {
	if guard == nil {
		return fmt.Errorf("recovery semantic authority is unavailable")
	}
	if guard.nativeCheck != nil {
		if guard.validating {
			return fmt.Errorf("recovery has two semantic decision owners")
		}
		return guard.nativeCheck(ctx)
	}
	if !guard.validating {
		return nil
	}
	if guard.executionBudget == nil ||
		!guard.snapshot.initialized {
		return fmt.Errorf("recovery semantic execution witness is unavailable")
	}
	current, err := guard.Observe(
		ctx,
		guard.executionBudget,
	)
	if err != nil {
		return err
	}
	if !guard.snapshot.equal(current) {
		return errors.Join(
			mutation.StaleSnapshotError{},
			fmt.Errorf("recovery statefile or ownership semantics changed before the next effect"),
		)
	}
	return nil
}

func (guard *Guard) Reserve(count int, budget *physicalwork.Budget) error {
	if guard == nil || budget == nil || count <= 0 {
		return fmt.Errorf("recovery semantic validation count must be positive")
	}
	reservation := pathReservation{budget: budget}
	entries := []entryBinding{guard.statefile}
	if guard.registryEntry.valid() {
		entries = append(entries, guard.registryEntry)
	}
	for _, entry := range entries {
		if !entry.valid() {
			return fmt.Errorf("recovery semantic entry authority is unavailable")
		}
		for range count {
			if err := entry.root.ReserveDestinationAccess(
				entry.destination,
				physicalwork.MaximumPhysicalPathDepth,
				reservation,
			); err != nil {
				return fmt.Errorf("reserve recovery semantic validation: %w", err)
			}
		}
	}
	return nil
}

func (guard *Guard) BeginExecution(parent *physicalwork.Budget) error {
	if guard == nil || parent == nil {
		return fmt.Errorf("recovery semantic authority is unavailable")
	}
	if guard.executionBudget != nil {
		return fmt.Errorf("recovery semantic execution was already started")
	}
	if !guard.snapshot.initialized {
		return fmt.Errorf("recovery semantic witness was not established before execution")
	}
	if guard.nativeCheck != nil {
		return fmt.Errorf("recovery has two semantic decision owners")
	}
	budget, err := parent.BeginReservedSemanticExecution()
	if err != nil {
		return err
	}
	guard.executionBudget = budget
	guard.validating = true
	return nil
}

func (guard *Guard) ObserveExecution(ctx context.Context) (Snapshot, error) {
	if guard == nil {
		return Snapshot{}, fmt.Errorf("recovery semantic authority is unavailable")
	}
	return guard.Observe(ctx, guard.executionBudget)
}

// InstallNativeCheck delegates one serialized recovery scope to native
// qualification. Its idempotent release cannot clear a later scope.
func (guard *Guard) InstallNativeCheck(check func(context.Context) error) (func(), error) {
	if guard == nil || check == nil {
		return nil, fmt.Errorf("native recovery semantic check is required")
	}
	if guard.validating || guard.nativeCheck != nil {
		return nil, fmt.Errorf("ordinary recovery already has a semantic decision owner")
	}
	guard.nativeCheck = check
	released := false
	return func() {
		if !released {
			guard.nativeCheck = nil
			released = true
		}
	}, nil
}
