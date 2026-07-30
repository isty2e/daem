package journal

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// ErrNoRecoverableJournal reports that the recovery root has no active or
// cleanup-only operation.
var ErrNoRecoverableJournal = errors.New("no recoverable journal operation")

// PlanLoadOptions supplies effect-time authority for final recovery
// classification. Rooted capabilities and the ownership reader are borrowed
// for the load.
type PlanLoadOptions struct {
	RootedCapability  RootedCapabilityResolver
	Resolver          func(destination output.Destination) (string, error)
	OwnershipRegistry ownershipmutation.RegistryReader
	Codecs            aggregate.CodecCatalog
	StateCodec        durable.SnapshotCodec
	StateReader       durable.SnapshotReader
	Filesystem        mutationfs.Reader
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
		plan.physicalAuthority.equal(typed.physicalAuthority) &&
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
	inventory recoveryRootInventory,
	requireControl bool,
) (recoverableInventoryAuthority, error) {
	if inventory.root.RootIdentity() == nil {
		return recoverableInventoryAuthority{}, fmt.Errorf(
			"recovery-root inventory authority is uninitialized",
		)
	}
	if requireControl && inventory.control == nil {
		return recoverableInventoryAuthority{}, fmt.Errorf(
			"recovery control inventory authority is unavailable",
		)
	}
	if inventory.control != nil && inventory.control.RootIdentity() == nil {
		return recoverableInventoryAuthority{}, fmt.Errorf(
			"recovery control inventory authority is uninitialized",
		)
	}
	return recoverableInventoryAuthority{
		root:    inventory.root,
		control: cloneDirectorySnapshot(inventory.control),
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
	active, ok := recoverable.(activeRecoverablePlan)
	if !ok || !active.physicalAuthority.valid() {
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
	paths Paths,
	options PlanLoadOptions,
) (RecoverablePlan, error) {
	if err := validatePlanLoadFilesystem(options); err != nil {
		return nil, err
	}
	inventory, err := loadRecoveryRootInventory(
		ctx,
		paths.RecoveryDir,
		inventoryOptionsFromPlan(options),
	)
	if err != nil {
		return nil, err
	}
	switch inventory.decision.State() {
	case retirement.StateActive, retirement.StatePrepared:
		if err := validateActivePlanLoadOptions(options); err != nil {
			return nil, err
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
			inventory.decision.State() == retirement.StatePrepared,
		)
		if err != nil {
			return nil, err
		}
		return activeRecoverablePlan{
			plan:              plan.Clone(),
			physicalAuthority: inventory.active.physicalAuthority,
			inventory:         authority,
		}, nil
	case retirement.StateRetained, retirement.StateFinalizing:
		plan, ok := inventory.decision.CleanupPlan()
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
			inventory.decision.Detail(),
		)
	default:
		return nil, ErrNoRecoverableJournal
	}
}

// LoadActivePlanWithOptions classifies the active journal using any supplied
// rooted global-destination authority.
func LoadActivePlanWithOptions(ctx context.Context, paths Paths, options PlanLoadOptions) (recovery.Plan, error) {
	return loadActivePlan(ctx, paths, nil, nil, options)
}

// LoadActivePlanForStateWithOptions classifies the active journal against
// caller-owned state and any supplied rooted global-destination authority.
func LoadActivePlanForStateWithOptions(
	ctx context.Context,
	paths Paths,
	currentState durable.Snapshot,
	options PlanLoadOptions,
) (recovery.Plan, error) {
	return loadActivePlan(ctx, paths, &currentState, nil, options)
}

// LoadActivePlanForStateEntriesWithOptions classifies selected host entries
// using caller-owned state and any supplied rooted global-destination authority.
func LoadActivePlanForStateEntriesWithOptions(
	ctx context.Context,
	paths Paths,
	currentState durable.Snapshot,
	selected []EntrySelection,
	options PlanLoadOptions,
) (recovery.Plan, error) {
	return loadActivePlan(ctx, paths, &currentState, &selected, options)
}

func loadActivePlan(
	ctx context.Context,
	paths Paths,
	suppliedState *durable.Snapshot,
	selected *[]EntrySelection,
	options PlanLoadOptions,
) (recovery.Plan, error) {
	if err := validateActivePlanLoadOptions(options); err != nil {
		return recovery.Plan{}, err
	}
	inventory, err := loadRecoveryRootInventory(
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
	paths Paths,
	suppliedState *durable.Snapshot,
	selected *[]EntrySelection,
	options PlanLoadOptions,
	inventory recoveryRootInventory,
) (recovery.Plan, error) {
	switch inventory.decision.State() {
	case retirement.StateActive, retirement.StatePrepared:
	case retirement.StateBlocked:
		return recovery.Plan{}, fmt.Errorf(
			"recovery inventory is blocked: %s",
			inventory.decision.Detail(),
		)
	default:
		return recovery.Plan{}, fmt.Errorf("no active recovery journal")
	}
	if inventory.active == nil {
		return recovery.Plan{}, fmt.Errorf("active recovery inventory is incomplete")
	}
	if options.ValidateBeforeActiveObservation != nil {
		if err := options.ValidateBeforeActiveObservation(ctx); err != nil {
			return recovery.Plan{}, err
		}
	}
	operationDir := inventory.active.operationDir
	journal := inventory.active.journal
	claimTransitions, err := canonicalClaimTransitions(journal.ClaimTransitions)
	if err != nil {
		return recovery.Plan{}, err
	}
	resolver := options.Resolver
	if err := validateRecoveryClaimCoverage(journal.Entries, claimTransitions, resolver); err != nil {
		return recovery.Plan{}, fmt.Errorf("validate recovery ownership coverage: %w", err)
	}
	planningEntries := journal.Entries
	var selectedIndexes []int
	if selected != nil {
		selectedIndexes, err = selectedRecoveryEntryIndexes(journal.Entries, *selected)
		if err != nil {
			return recovery.Plan{}, err
		}
		planningEntries = recoveryEntriesAtIndexes(journal.Entries, selectedIndexes)
	}
	registry := ownership.EmptyRegistry()
	if len(claimTransitions) != 0 {
		if options.OwnershipRegistry == nil {
			return recovery.Plan{}, fmt.Errorf("ownership registry reader is required for recovery claim classification")
		}
		registry, err = options.OwnershipRegistry(ctx)
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
	projectAuthority, err := projectAuthorityForRecovery(paths, journal)
	if err != nil {
		return recovery.Plan{}, err
	}
	if projectAuthority != nil {
		defer projectAuthority.close()
	}
	observations := recoveryPathObservations(
		ctx,
		planningEntries,
		options.Filesystem,
		projectAuthority,
		resolver,
		options.RootedCapability,
		options.Codecs,
	)
	if projectAuthority != nil {
		if err := projectAuthority.close(); err != nil {
			return recovery.Plan{}, fmt.Errorf("close recovery project root authority: %w", err)
		}
	}
	backupObservations := recoveryBackupObservations(
		ctx,
		operationDir,
		planningEntries,
	)
	authority, err := canonicalRecoveryAuthority(
		journal,
		operationDir,
		claimTransitions,
		inventory.active.identity.JournalAuthorityFingerprint(),
	)
	if err != nil {
		return recovery.Plan{}, err
	}
	selection, err := recovery.NewSelection(authority, selectedIndexes)
	if err != nil {
		return recovery.Plan{}, err
	}
	return recovery.Classify(
		authority,
		selection,
		currentState,
		canonicalRecoveryPathEvidence(observations),
		canonicalRecoveryBackupEvidence(backupObservations),
		registry,
	)
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

func inventoryOptionsFromPlan(options PlanLoadOptions) inventoryOptions {
	return inventoryOptions{
		Filesystem: options.Filesystem,
		StateCodec: options.StateCodec,
	}
}

func selectedRecoveryEntryIndexes(entries []recoveryEntry, selected []EntrySelection) ([]int, error) {
	keys := make(map[entrySelectionKey]struct{}, len(selected))
	for _, selection := range selected {
		if !selection.initialized {
			return nil, fmt.Errorf("recovery entry selection is uninitialized")
		}
		key := selection.key
		if _, exists := keys[key]; exists {
			return nil, fmt.Errorf("duplicate selected recovery entry for %q", key.destination)
		}
		keys[key] = struct{}{}
	}

	result := make([]int, 0, len(selected))
	for index, entry := range entries {
		key, err := entrySelectionKeyFromRecoveryEntry(entry)
		if err != nil {
			return nil, err
		}
		if _, exists := keys[key]; !exists {
			continue
		}
		result = append(result, index)
		delete(keys, key)
	}
	if len(keys) != 0 {
		return nil, fmt.Errorf("selected recovery entries do not match the active journal")
	}
	return result, nil
}

func recoveryEntriesAtIndexes(entries []recoveryEntry, indexes []int) []recoveryEntry {
	selected := make([]recoveryEntry, len(indexes))
	for resultIndex, entryIndex := range indexes {
		selected[resultIndex] = entries[entryIndex]
	}
	return selected
}

func entrySelectionKeyFromRecoveryEntry(entry recoveryEntry) (entrySelectionKey, error) {
	agentTarget, _ := target.ParseTarget(entry.Target)
	scope, _ := target.ParseScope(entry.Scope)
	subject, err := entry.Subject.canonical()
	if err != nil {
		return entrySelectionKey{}, fmt.Errorf("recovery entry subject: %w", err)
	}
	destination, err := output.Parse(entry.Path)
	if err != nil {
		return entrySelectionKey{}, fmt.Errorf("recovery entry destination: %w", err)
	}
	return entrySelectionKey{
		subject:     subject,
		target:      agentTarget,
		consumers:   strings.Join(entry.Targets, "\x00"),
		scope:       scope,
		destination: destination,
		contentPath: output.ContentPath(entry.ContentPath),
	}, nil
}

// EntrySelection identifies one validated capture mutation without exposing
// expected physical state or persisted journal syntax.
type EntrySelection struct {
	key         entrySelectionKey
	initialized bool
}

type entrySelectionKey struct {
	subject     topology.SubjectID
	target      target.Target
	consumers   string
	scope       target.Scope
	destination output.Destination
	contentPath output.ContentPath
}

// EntrySelections derives rollback-selection identities in host mutation
// order: managed paths first, followed by managed aggregate projections.
func EntrySelections(
	managed []ManagedPathMutation,
	aggregates []ManagedAggregateMutation,
) ([]EntrySelection, error) {
	result := make([]EntrySelection, 0, len(managed)+len(aggregates))
	seen := make(map[entrySelectionKey]struct{}, cap(result))
	appendSelection := func(key entrySelectionKey) error {
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("duplicate recovery entry selection for %q", key.destination)
		}
		seen[key] = struct{}{}
		result = append(result, EntrySelection{key: key, initialized: true})
		return nil
	}

	for index, mutation := range managed {
		if err := mutation.validate(); err != nil {
			return nil, fmt.Errorf("managed path journal mutation[%d]: %w", index, err)
		}
		facts := mutation.facts()
		if err := appendSelection(entrySelectionKey{
			subject:     facts.subject,
			consumers:   consumerTargetKey(facts.consumerTargets),
			scope:       facts.scope,
			destination: facts.destination,
		}); err != nil {
			return nil, err
		}
	}
	for index, mutation := range aggregates {
		if err := mutation.validate(); err != nil {
			return nil, fmt.Errorf("managed aggregate journal mutation[%d]: %w", index, err)
		}
		if err := appendSelection(entrySelectionKeyFromMutation(pathMutationFromAggregate(mutation))); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func entrySelectionKeyFromMutation(mutation pathMutation) entrySelectionKey {
	return entrySelectionKey{
		subject: mutation.Subject, target: mutation.Target,
		consumers: consumerTargetKey(mutation.ConsumerTargets), scope: mutation.Scope,
		destination: mutation.Destination, contentPath: mutation.ContentPath,
	}
}

func consumerTargetKey(values []target.Target) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, string(value))
	}
	return strings.Join(parts, "\x00")
}
