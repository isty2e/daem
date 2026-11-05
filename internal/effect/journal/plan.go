package journal

import (
	"context"
	"errors"
	"fmt"

	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	"github.com/isty2e/daem/internal/effect/journal/recoveryoperation"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// ErrNoRecoverableJournal reports that the recovery root has no active or
// cleanup-only operation.
var ErrNoRecoverableJournal = errors.New("no recoverable journal operation")

// PlanLoadOptions supplies effect-time authority for final recovery
// classification. Rooted capabilities and the ownership reader are borrowed
// for the load.
type PlanLoadOptions struct {
	RootedCapability   journalnamespace.CapabilityResolver
	Resolver           func(destination output.Destination) (string, error)
	OwnershipRegistry  ownershipmutation.RegistryReader
	Codecs             aggregate.CodecCatalog
	StateCodec         durable.SnapshotCodec
	StateReader        durable.SnapshotReader
	Filesystem         mutationfs.Reader
	PhysicalPathBudget rootedpath.PhysicalTraversalBudget
	NativeDocuments    *recoveryoperation.Profile
	// ValidateBeforeActiveObservation runs after one recovery-root inventory
	// selects active-journal recovery and before host, state, or ownership
	// observation. Cleanup-only selection never invokes it.
	ValidateBeforeActiveObservation func(context.Context) error
}

// RecoveryAuthorityKind identifies the exact journal authority selected for
// explicit recovery.
type RecoveryAuthorityKind string

const (
	RecoveryAuthorityActiveJournal  RecoveryAuthorityKind = "active_journal"
	RecoveryAuthorityJournalCleanup RecoveryAuthorityKind = "journal_cleanup"
)

// RecoverablePlan is one opaque active-journal or cleanup-only recovery
// selection. Only this package can construct a variant.
type RecoverablePlan interface {
	recoverablePlan()
	Clone() RecoverablePlan
	AuthorityKind() RecoveryAuthorityKind
	Blocked() bool
	HasErrors() bool
	SameExecutionAuthority(RecoverablePlan) bool
}

type activeRecoverablePlan struct {
	plan              recovery.Plan
	physicalAuthority ActiveJournalAuthority
	inventory         recoverableInventoryAuthority
}

func (activeRecoverablePlan) recoverablePlan() {}

func (plan activeRecoverablePlan) Clone() RecoverablePlan {
	return activeRecoverablePlan{
		plan:              plan.plan.Clone(),
		physicalAuthority: plan.physicalAuthority,
		inventory:         plan.inventory.clone(),
	}
}

func (activeRecoverablePlan) AuthorityKind() RecoveryAuthorityKind {
	return RecoveryAuthorityActiveJournal
}

func (plan activeRecoverablePlan) Blocked() bool {
	return plan.plan.Blocked()
}

func (plan activeRecoverablePlan) HasErrors() bool {
	return plan.plan.HasErrors()
}

func (plan activeRecoverablePlan) SameExecutionAuthority(other RecoverablePlan) bool {
	typed, ok := other.(activeRecoverablePlan)
	return ok &&
		plan.plan.SameExecutionAuthority(typed.plan) &&
		plan.physicalAuthority.Equal(typed.physicalAuthority) &&
		plan.inventory.equal(typed.inventory)
}

type cleanupRecoverablePlan struct {
	plan      retirement.CleanupPlan
	inventory recoverableInventoryAuthority
}

func (cleanupRecoverablePlan) recoverablePlan() {}

func (plan cleanupRecoverablePlan) Clone() RecoverablePlan {
	plan.inventory = plan.inventory.clone()
	return plan
}

func (cleanupRecoverablePlan) AuthorityKind() RecoveryAuthorityKind {
	return RecoveryAuthorityJournalCleanup
}

func (cleanupRecoverablePlan) Blocked() bool {
	return false
}

func (cleanupRecoverablePlan) HasErrors() bool {
	return false
}

func (plan cleanupRecoverablePlan) SameExecutionAuthority(other RecoverablePlan) bool {
	typed, ok := other.(cleanupRecoverablePlan)
	return ok &&
		plan.plan.SameExecutionAuthority(typed.plan) &&
		plan.inventory.equal(typed.inventory)
}

type recoverableInventoryAuthority struct {
	root    mutationfs.DirectorySnapshot
	control *mutationfs.DirectorySnapshot
}

func newRecoverableInventoryAuthority(
	inventory journalinventory.RootInventory,
	requireControl bool,
) (recoverableInventoryAuthority, error) {
	if inventory.RootSnapshot().RootIdentity() == nil {
		return recoverableInventoryAuthority{}, fmt.Errorf(
			"recovery-root inventory authority is uninitialized",
		)
	}
	if requireControl && inventory.ControlSnapshot() == nil {
		return recoverableInventoryAuthority{}, fmt.Errorf(
			"recovery control inventory authority is unavailable",
		)
	}
	if inventory.ControlSnapshot() != nil && inventory.ControlSnapshot().RootIdentity() == nil {
		return recoverableInventoryAuthority{}, fmt.Errorf(
			"recovery control inventory authority is uninitialized",
		)
	}
	return recoverableInventoryAuthority{
		root:    inventory.RootSnapshot(),
		control: cloneDirectorySnapshot(inventory.ControlSnapshot()),
	}, nil
}

func (authority recoverableInventoryAuthority) clone() recoverableInventoryAuthority {
	authority.control = cloneDirectorySnapshot(authority.control)
	return authority
}

func (authority recoverableInventoryAuthority) equal(
	other recoverableInventoryAuthority,
) bool {
	if !authority.root.Equal(other.root) ||
		(authority.control == nil) != (other.control == nil) {
		return false
	}
	return authority.control == nil || authority.control.Equal(*other.control)
}

func cloneDirectorySnapshot(
	snapshot *mutationfs.DirectorySnapshot,
) *mutationfs.DirectorySnapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	return &cloned
}

// ActiveRecoveryPlan returns a defensive active plan for only that variant.
func ActiveRecoveryPlan(recoverable RecoverablePlan) (recovery.Plan, bool) {
	active, ok := recoverable.(activeRecoverablePlan)
	if !ok {
		return recovery.Plan{}, false
	}
	return active.plan.Clone(), true
}

// ActiveRecoveryJournalAuthority returns operation-local physical evidence for
// only an active-journal recovery selection.
func ActiveRecoveryJournalAuthority(
	recoverable RecoverablePlan,
) (ActiveJournalAuthority, bool) {
	if native, ok := NativeActiveRecoveryPlan(recoverable); ok {
		return native.active, native.active.Validate() == nil
	}
	active, ok := recoverable.(activeRecoverablePlan)
	if !ok || active.physicalAuthority.Validate() != nil {
		return ActiveJournalAuthority{}, false
	}
	return active.physicalAuthority, true
}

// JournalCleanupPlan returns the immutable cleanup plan for only that variant.
func JournalCleanupPlan(recoverable RecoverablePlan) (retirement.CleanupPlan, bool) {
	cleanup, ok := recoverable.(cleanupRecoverablePlan)
	if !ok {
		return retirement.CleanupPlan{}, false
	}
	return cleanup.plan, true
}

// LoadRecoverablePlanWithOptions selects exactly one active-journal or
// cleanup-only plan from a single stable recovery-root inventory.
func LoadRecoverablePlanWithOptions(
	ctx context.Context,
	paths journalpublication.Paths,
	options PlanLoadOptions,
) (RecoverablePlan, error) {
	if err := validatePlanLoadFilesystem(options); err != nil {
		return nil, err
	}
	planningBudget, err := planLoadPhysicalPathBudget(options.PhysicalPathBudget)
	if err != nil {
		return nil, err
	}
	options.PhysicalPathBudget = planningBudget
	inventory, err := journalinventory.LoadRoot(
		ctx,
		paths.RecoveryDir,
		inventoryOptionsFromPlan(options),
	)
	if err != nil {
		return nil, err
	}
	switch inventory.Decision().State() {
	case retirement.StateActive, retirement.StatePrepared:
		if err := validateActivePlanLoadOptions(options); err != nil {
			return nil, err
		}
		if inventory.Active() != nil {
			if _, native := inventory.Active().Record().Native(); native {
				if options.ValidateBeforeActiveObservation != nil {
					if err := options.ValidateBeforeActiveObservation(ctx); err != nil {
						return nil, err
					}
				}
				operation, err := loadNativeRecoveryOperation(ctx, paths, options, inventory)
				if err != nil {
					return nil, err
				}
				plan := operation.Plan()
				if err := operation.Close(); err != nil {
					return nil, err
				}
				return plan, nil
			}
		}
		plan, err := loadActivePlanFromInventory(
			ctx,
			paths,
			nil,
			nil,
			options,
			inventory,
		)
		if err != nil {
			return nil, err
		}
		authority, err := newRecoverableInventoryAuthority(
			inventory,
			inventory.Decision().State() == retirement.StatePrepared,
		)
		if err != nil {
			return nil, err
		}
		return activeRecoverablePlan{
			plan:              plan.Clone(),
			physicalAuthority: inventory.Active().PhysicalAuthority(),
			inventory:         authority,
		}, nil
	case retirement.StateRetained,
		retirement.StateFinalizing:
		plan, ok := inventory.Decision().CleanupPlan()
		if !ok {
			return nil, fmt.Errorf(
				"journal cleanup classification has no cleanup plan",
			)
		}
		authority, err := newRecoverableInventoryAuthority(inventory, true)
		if err != nil {
			return nil, err
		}
		return cleanupRecoverablePlan{
			plan:      plan,
			inventory: authority,
		}, nil
	case retirement.StateBlocked:
		return nil, fmt.Errorf(
			"recovery inventory is blocked: %s",
			inventory.Decision().Detail(),
		)
	default:
		return nil, ErrNoRecoverableJournal
	}
}

// LoadActivePlanWithOptions classifies the active journal using any supplied
// rooted global-destination authority.
func LoadActivePlanWithOptions(ctx context.Context, paths journalpublication.Paths, options PlanLoadOptions) (recovery.Plan, error) {
	return loadActivePlan(ctx, paths, nil, nil, options)
}

// LoadActivePlanForStateWithOptions classifies the active journal against
// caller-owned state and any supplied rooted global-destination authority.
func LoadActivePlanForStateWithOptions(
	ctx context.Context,
	paths journalpublication.Paths,
	currentState durable.Snapshot,
	options PlanLoadOptions,
) (recovery.Plan, error) {
	return loadActivePlan(ctx, paths, &currentState, nil, options)
}

// LoadActivePlanForStateEntriesWithOptions classifies selected host entries
// using caller-owned state and any supplied rooted global-destination authority.
func LoadActivePlanForStateEntriesWithOptions(
	ctx context.Context,
	paths journalpublication.Paths,
	currentState durable.Snapshot,
	selected []journalrecord.EntrySelection,
	options PlanLoadOptions,
) (recovery.Plan, error) {
	return loadActivePlan(ctx, paths, &currentState, &selected, options)
}

func loadActivePlan(
	ctx context.Context,
	paths journalpublication.Paths,
	suppliedState *durable.Snapshot,
	selected *[]journalrecord.EntrySelection,
	options PlanLoadOptions,
) (recovery.Plan, error) {
	if err := validateActivePlanLoadOptions(options); err != nil {
		return recovery.Plan{}, err
	}
	planningBudget, err := planLoadPhysicalPathBudget(options.PhysicalPathBudget)
	if err != nil {
		return recovery.Plan{}, err
	}
	options.PhysicalPathBudget = planningBudget
	inventory, err := journalinventory.LoadRoot(
		ctx,
		paths.RecoveryDir,
		inventoryOptionsFromPlan(options),
	)
	if err != nil {
		return recovery.Plan{}, err
	}
	return loadActivePlanFromInventory(
		ctx,
		paths,
		suppliedState,
		selected,
		options,
		inventory,
	)
}

func loadActivePlanFromInventory(
	ctx context.Context,
	paths journalpublication.Paths,
	suppliedState *durable.Snapshot,
	selected *[]journalrecord.EntrySelection,
	options PlanLoadOptions,
	inventory journalinventory.RootInventory,
) (recovery.Plan, error) {
	switch inventory.Decision().State() {
	case retirement.StateActive, retirement.StatePrepared:
	case retirement.StateBlocked:
		return recovery.Plan{}, fmt.Errorf(
			"recovery inventory is blocked: %s",
			inventory.Decision().Detail(),
		)
	default:
		return recovery.Plan{}, fmt.Errorf("no active recovery journal")
	}
	if inventory.Active() == nil {
		return recovery.Plan{}, fmt.Errorf("active recovery inventory is incomplete")
	}
	journal, ok := inventory.Active().Record().Ordinary()
	if !ok {
		return recovery.Plan{}, fmt.Errorf("legacy recovery planning requires a journal15 inventory record")
	}
	if options.ValidateBeforeActiveObservation != nil {
		if err := options.ValidateBeforeActiveObservation(ctx); err != nil {
			return recovery.Plan{}, err
		}
	}
	operationDir := inventory.Active().OperationDir()
	basis, err := journal.RecoveryPlanningBasis(operationDir, inventory.Active().Identity().JournalAuthorityFingerprint(), selected)
	if err != nil {
		return recovery.Plan{}, err
	}
	if err := basis.Validate(); err != nil {
		return recovery.Plan{}, err
	}
	claimTransitions, provisionalAcquires := basis.ClaimTransitions(), basis.ProvisionalAcquires()
	authority, selectedIndexes := basis.Authority(), basis.SelectedIndexes()
	planningEntries := basis.PlanningEntries()
	resolver := options.Resolver
	planningBudget, err := physicalwork.NewBudgetWithPathBudget(
		basis.RemovalIntentCount(),
		options.PhysicalPathBudget,
	)
	if err != nil {
		return recovery.Plan{}, err
	}
	if err := journalqualification.ValidateGlobalPathBindings(
		ctx,
		basis.AllEntries(),
		options.Resolver,
		options.RootedCapability,
		planningBudget,
	); err != nil {
		return recovery.Plan{}, err
	}
	if len(claimTransitions) != 0 || len(provisionalAcquires) != 0 {
		statefileAuthority, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(
			paths.StatefilePath,
			physicalwork.MaximumPhysicalPathDepth,
			planningBudget,
		)
		if err != nil {
			return recovery.Plan{}, fmt.Errorf("canonicalize recovery state authority: %w", err)
		}
		if err := journalrecord.ValidateClaimAuthorities(
			claimTransitions,
			statefileAuthority,
		); err != nil {
			return recovery.Plan{}, err
		}
		if err := journalrecord.ValidateIntentAuthorities(
			provisionalAcquires,
			statefileAuthority,
		); err != nil {
			return recovery.Plan{}, err
		}
	}
	registry := ownership.EmptyRegistry()
	if len(claimTransitions) != 0 || len(provisionalAcquires) != 0 {
		if options.OwnershipRegistry == nil {
			return recovery.Plan{}, fmt.Errorf("ownership registry reader is required for recovery ownership classification")
		}
		registry, err = options.OwnershipRegistry.LoadForClaimRemovals(
			ctx,
			recoveryClaimRemovalCandidates(claimTransitions),
			physicalwork.MaximumPhysicalPathDepth,
			planningBudget,
		)
		if err != nil {
			return recovery.Plan{}, err
		}
	}

	currentState := durable.EmptySnapshot()
	if suppliedState != nil {
		currentState = *suppliedState
	} else {
		if options.StateReader == nil {
			return recovery.Plan{}, fmt.Errorf("recovery state reader is required")
		}
		currentState, err = options.StateReader(ctx)
		if err != nil {
			return recovery.Plan{}, err
		}
	}
	manifestAuthority, err := journalnamespace.RecaptureManifestAuthority(paths.ManifestRoot, basis.ManifestProvenance(), planningBudget)
	if err != nil {
		return recovery.Plan{}, err
	}
	if manifestAuthority != nil {
		defer manifestAuthority.Close()
	}
	observations, err := journalqualification.ObservePaths(
		ctx,
		planningEntries,
		options.Filesystem,
		manifestAuthority,
		resolver,
		options.RootedCapability,
		options.Codecs,
		planningBudget,
	)
	if err != nil {
		return recovery.Plan{}, err
	}
	observations, err = journalqualification.ApplyIntentOwnershipEvidence(
		ctx,
		observations,
		provisionalAcquires,
		registry,
		resolver,
		planningBudget,
	)
	if err != nil {
		return recovery.Plan{}, err
	}
	if manifestAuthority != nil {
		if err := manifestAuthority.Close(); err != nil {
			return recovery.Plan{}, fmt.Errorf("close recovery manifest root authority: %w", err)
		}
	}
	backupObservations, err := journalqualification.ObserveBackups(
		ctx,
		operationDir,
		inventory.Active().PhysicalAuthority(),
		planningEntries,
		options.Filesystem,
		planningBudget,
	)
	if err != nil {
		return recovery.Plan{}, err
	}
	selection, err := recovery.NewSelection(authority, selectedIndexes)
	if err != nil {
		return recovery.Plan{}, err
	}
	plan, err := recovery.Classify(
		authority,
		selection,
		currentState,
		recovery.ClonePathEvidence(observations),
		recovery.CloneBackupEvidence(backupObservations),
		registry,
	)
	if err != nil {
		return recovery.Plan{}, err
	}
	return journalqualification.AssessRemovalCleanupPlan(ctx, plan, journalqualification.RemovalReadOptions{Filesystem: options.Filesystem, Resolver: options.Resolver}, planningBudget)
}

func recoveryClaimRemovalCandidates(transitions []ownershipmutation.ClaimTransition) []ownership.Claim {
	claims := make([]ownership.Claim, 0, len(transitions))
	for _, transition := range transitions {
		if transition.Kind() != ownershipmutation.TransitionRelease &&
			transition.Kind() != ownershipmutation.TransitionAcquire {
			continue
		}
		claim, present := transition.Prepared().Get()
		if present {
			claims = append(claims, claim)
		}
	}
	return claims
}

func validateActivePlanLoadOptions(options PlanLoadOptions) error {
	if options.RootedCapability != nil && options.Resolver == nil {
		return fmt.Errorf("recovery plan rooted capability requires a destination resolver")
	}
	return validatePlanLoadFilesystem(options)
}

func validatePlanLoadFilesystem(options PlanLoadOptions) error {
	if options.Filesystem == nil {
		return fmt.Errorf("recovery plan filesystem is required")
	}
	return nil
}

func inventoryOptionsFromPlan(options PlanLoadOptions) journalinventory.Options {
	result := journalinventory.Options{
		Filesystem: options.Filesystem, StateCodec: options.StateCodec,
		PhysicalPathBudget: options.PhysicalPathBudget,
	}
	if options.NativeDocuments != nil {
		result.Decoder = nativeRecoveryInventoryDecoder(options.StateCodec, options.NativeDocuments.StateCodec)
	}
	return result
}

func planLoadPhysicalPathBudget(
	provided rootedpath.PhysicalTraversalBudget,
) (rootedpath.PhysicalTraversalBudget, error) {
	if provided != nil {
		return provided, nil
	}
	return physicalwork.NewPathBudget(), nil
}
