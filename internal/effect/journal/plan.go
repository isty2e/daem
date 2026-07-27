package journal

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

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
	if options.RootedCapability != nil && options.Resolver == nil {
		return recovery.Plan{}, fmt.Errorf("recovery plan rooted capability requires a destination resolver")
	}
	if options.Filesystem == nil {
		return recovery.Plan{}, fmt.Errorf("recovery plan filesystem is required")
	}
	operations, err := activeRecoveryOperations(paths.RecoveryDir)
	if err != nil {
		return recovery.Plan{}, err
	}
	if len(operations) == 0 {
		return recovery.Plan{}, fmt.Errorf("no active recovery journal")
	}
	if len(operations) > 1 {
		return recovery.Plan{}, fmt.Errorf("multiple active recovery journals found")
	}

	operationID := operations[0]
	operationDir, err := mutation.CanonicalDirectoryEntryPath(filepath.Join(paths.RecoveryDir, operationID))
	if err != nil {
		return recovery.Plan{}, fmt.Errorf("canonicalize active recovery operation: %w", err)
	}
	journal, err := loadRecoveryJournal(
		ctx,
		options.Filesystem,
		filepath.Join(operationDir, recoveryJournalFileName),
		options.StateCodec,
	)
	if err != nil {
		return recovery.Plan{}, err
	}
	if journal.OperationID != operationID {
		return recovery.Plan{}, fmt.Errorf("recovery journal operation_id %q does not match directory %q", journal.OperationID, operationID)
	}
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
	fingerprint, err := recoveryJournalAuthorityFingerprint(journal, options.StateCodec)
	if err != nil {
		return recovery.Plan{}, err
	}
	authority, err := canonicalRecoveryAuthority(journal, operationDir, claimTransitions, fingerprint)
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
