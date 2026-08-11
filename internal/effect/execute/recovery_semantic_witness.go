package execute

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
)

type recoverySemanticEntryBinding struct {
	root        *rootedpath.CapturedRoot
	destination rootedpath.Destination
}

func (binding recoverySemanticEntryBinding) valid() bool {
	return binding.root != nil && binding.destination.Validate() == nil
}

type recoverySemanticEntryWitness struct {
	identity mutationfs.EntryIdentity
	exists   bool
}

func (witness recoverySemanticEntryWitness) equal(other recoverySemanticEntryWitness) bool {
	if witness.exists != other.exists {
		return false
	}
	if !witness.exists {
		return true
	}
	return witness.identity != nil && other.identity != nil &&
		witness.identity.Equal(other.identity)
}

type recoverySemanticWitness struct {
	statefile    recoverySemanticEntryWitness
	ownership    recoverySemanticEntryWitness
	hasOwnership bool
	initialized  bool
}

func (witness recoverySemanticWitness) equal(other recoverySemanticWitness) bool {
	return witness.initialized && other.initialized &&
		witness.hasOwnership == other.hasOwnership &&
		witness.statefile.equal(other.statefile) &&
		(!witness.hasOwnership || witness.ownership.equal(other.ownership))
}

type recoverySemanticPathReservation struct {
	budget *recovery.PhysicalWorkBudget
}

func (reservation recoverySemanticPathReservation) AdmitPathComponents(count int) error {
	return reservation.budget.ReserveSemanticPathComponents(count)
}

func (authority *mutationAuthority) bindRecoveryStatefileSemanticEntry(path string) error {
	if authority == nil || authority.physicalWorkBudget == nil {
		return fmt.Errorf("recovery semantic authority is unavailable")
	}
	if authority.statefileSemanticEntry.valid() {
		return fmt.Errorf("recovery statefile semantic authority is already bound")
	}
	root, destination, err := rootedpath.CaptureDestinationBounded(
		path,
		recovery.MaximumPhysicalPathDepth,
		authority.generalTraversalPhase,
	)
	if err != nil {
		return fmt.Errorf("bind recovery statefile semantic authority: %w", err)
	}
	root, err = authority.retainRoot(root, destination)
	if err != nil {
		return err
	}
	authority.statefileSemanticEntry = recoverySemanticEntryBinding{
		root: root, destination: destination,
	}
	return nil
}

func (authority *mutationAuthority) bindOwnershipSemanticEntry(
	root *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
) error {
	if authority == nil || root == nil || destination.Validate() != nil {
		return fmt.Errorf("recovery ownership semantic authority is invalid")
	}
	if authority.ownershipSemanticEntry.valid() {
		return fmt.Errorf("recovery ownership semantic authority is already bound")
	}
	authority.ownershipSemanticEntry = recoverySemanticEntryBinding{
		root: root, destination: destination,
	}
	return nil
}

func (binding recoverySemanticEntryBinding) observe(
	ctx context.Context,
	filesystem mutationfs.RootedReader,
	budget rootedpath.PhysicalTraversalBudget,
) (recoverySemanticEntryWitness, error) {
	if !binding.valid() {
		return recoverySemanticEntryWitness{}, fmt.Errorf("recovery semantic entry authority is unavailable")
	}
	if filesystem == nil || budget == nil {
		return recoverySemanticEntryWitness{}, fmt.Errorf("recovery semantic observation capability is unavailable")
	}
	capability, err := binding.root.AcquireBounded(
		binding.destination,
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		return recoverySemanticEntryWitness{}, err
	}
	identity, observeErr := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	closeErr := capability.Close()
	if errors.Is(observeErr, fs.ErrNotExist) {
		return recoverySemanticEntryWitness{}, closeErr
	}
	if observeErr != nil || closeErr != nil {
		return recoverySemanticEntryWitness{}, errors.Join(observeErr, closeErr)
	}
	if identity == nil || identity.Kind() == mutationfs.EntryKindInvalid {
		return recoverySemanticEntryWitness{}, fmt.Errorf("recovery semantic entry identity is unavailable")
	}
	return recoverySemanticEntryWitness{identity: identity, exists: true}, nil
}

func (authority *mutationAuthority) observeRecoverySemanticWitness(
	ctx context.Context,
	budget rootedpath.PhysicalTraversalBudget,
) (recoverySemanticWitness, error) {
	if authority == nil || !authority.statefileSemanticEntry.valid() {
		return recoverySemanticWitness{}, fmt.Errorf("recovery statefile semantic authority is unavailable")
	}
	statefile, err := authority.statefileSemanticEntry.observe(ctx, authority.filesystem, budget)
	if err != nil {
		return recoverySemanticWitness{}, fmt.Errorf("observe recovery statefile semantics: %w", err)
	}
	witness := recoverySemanticWitness{statefile: statefile, initialized: true}
	if authority.ownershipSemanticEntry.valid() {
		ownership, err := authority.ownershipSemanticEntry.observe(ctx, authority.filesystem, budget)
		if err != nil {
			return recoverySemanticWitness{}, fmt.Errorf("observe recovery ownership semantics: %w", err)
		}
		witness.ownership = ownership
		witness.hasOwnership = true
	}
	return witness, nil
}

func (authority *mutationAuthority) establishRecoverySemanticWitness(
	before recoverySemanticWitness,
	after recoverySemanticWitness,
) error {
	if authority == nil {
		return fmt.Errorf("recovery semantic authority is unavailable")
	}
	if authority.semanticWitness.initialized {
		return fmt.Errorf("recovery semantic witness was already established")
	}
	if !before.equal(after) {
		return errors.Join(
			mutation.StaleSnapshotError{},
			fmt.Errorf("recovery statefile or ownership semantics changed while classifying the plan"),
		)
	}
	authority.semanticWitness = after
	return nil
}

func (authority *mutationAuthority) validateRecoverySemanticWitnessPair(
	before recoverySemanticWitness,
	after recoverySemanticWitness,
	detail string,
) error {
	if authority == nil || !authority.semanticWitness.initialized {
		return fmt.Errorf("recovery semantic witness is unavailable")
	}
	if !authority.semanticWitness.equal(before) {
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

func (authority *mutationAuthority) acceptRecoveryOwnershipSuccessor(
	ctx context.Context,
	next ownership.Registry,
) error {
	if authority == nil || !authority.semanticWitness.initialized ||
		!authority.ownershipSemanticEntry.valid() {
		return fmt.Errorf("recovery ownership semantic authority is unavailable")
	}
	current, err := authority.observeRecoverySemanticWitness(
		ctx,
		authority.semanticExecutionWorkBudget,
	)
	if err != nil {
		return err
	}
	if !authority.semanticWitness.statefile.equal(current.statefile) {
		return errors.Join(
			mutation.StaleSnapshotError{},
			fmt.Errorf("recovery statefile semantics changed while advancing ownership"),
		)
	}
	if !current.hasOwnership {
		return fmt.Errorf("recovery ownership successor has no persisted authority")
	}
	authority.semanticWitness.ownership = current.ownership
	authority.ownershipSuccessor = next
	authority.hasOwnershipSuccessor = true
	return nil
}

func (authority *mutationAuthority) validateExpectedRecoveryOwnership(ctx context.Context) error {
	if authority == nil || !authority.hasOwnershipSuccessor {
		return nil
	}
	if !authority.hasOwnershipRegistry || authority.ownershipRegistry == nil {
		return fmt.Errorf("recovery ownership registry is unavailable")
	}
	current, err := authority.ownershipRegistry.Load(ctx)
	if err != nil {
		return fmt.Errorf("load recovery ownership successor: %w", err)
	}
	if !authority.ownershipSuccessor.Equal(current) {
		return errors.Join(
			mutation.StaleSnapshotError{},
			fmt.Errorf("recovery ownership registry differs from its admitted convergence successor"),
		)
	}
	return nil
}

func (authority *mutationAuthority) validateRecoverySemanticWitness(ctx context.Context) error {
	if authority == nil {
		return fmt.Errorf("recovery semantic authority is unavailable")
	}
	if !authority.recoverySemanticValidation {
		return nil
	}
	if authority.semanticExecutionWorkBudget == nil ||
		!authority.semanticWitness.initialized {
		return fmt.Errorf("recovery semantic execution witness is unavailable")
	}
	current, err := authority.observeRecoverySemanticWitness(
		ctx,
		authority.semanticExecutionWorkBudget,
	)
	if err != nil {
		return err
	}
	if !authority.semanticWitness.equal(current) {
		return errors.Join(
			mutation.StaleSnapshotError{},
			fmt.Errorf("recovery statefile or ownership semantics changed before the next effect"),
		)
	}
	return nil
}

func (authority *mutationAuthority) reserveRecoverySemanticValidations(count int) error {
	if authority == nil || authority.physicalWorkBudget == nil || count <= 0 {
		return fmt.Errorf("recovery semantic validation count must be positive")
	}
	reservation := recoverySemanticPathReservation{budget: authority.physicalWorkBudget}
	entries := []recoverySemanticEntryBinding{authority.statefileSemanticEntry}
	if authority.ownershipSemanticEntry.valid() {
		entries = append(entries, authority.ownershipSemanticEntry)
	}
	for _, entry := range entries {
		if !entry.valid() {
			return fmt.Errorf("recovery semantic entry authority is unavailable")
		}
		for range count {
			if err := entry.root.ReserveDestinationAccess(
				entry.destination,
				recovery.MaximumPhysicalPathDepth,
				reservation,
			); err != nil {
				return fmt.Errorf("reserve recovery semantic validation: %w", err)
			}
		}
	}
	return nil
}

func (authority *mutationAuthority) beginRecoverySemanticExecution() error {
	if authority == nil || authority.physicalWorkBudget == nil {
		return fmt.Errorf("recovery semantic authority is unavailable")
	}
	if authority.semanticExecutionWorkBudget != nil {
		return fmt.Errorf("recovery semantic execution was already started")
	}
	if !authority.semanticWitness.initialized {
		return fmt.Errorf("recovery semantic witness was not established before execution")
	}
	budget, err := authority.physicalWorkBudget.BeginReservedSemanticExecution()
	if err != nil {
		return err
	}
	authority.semanticExecutionWorkBudget = budget
	authority.recoverySemanticValidation = true
	return nil
}
