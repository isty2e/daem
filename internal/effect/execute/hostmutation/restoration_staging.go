package hostmutation

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"

	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"

	"github.com/isty2e/daem/internal/effect/journal/recovery"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

const (
	recoveryBaselineObservationAccesses = 3
	recoveryHostMutationAccesses        = 4
	recoveryRollbackObservationAccesses = 3
	recoveryRollbackMutationAccesses    = 4
	recoveryPostconditionAccesses       = 2
	recoveryHostDestinationAccesses     = recoveryBaselineObservationAccesses +
		recoveryHostMutationAccesses +
		recoveryRollbackObservationAccesses +
		recoveryRollbackMutationAccesses +
		recoveryPostconditionAccesses
)

type hostRollback struct {
	scratch *RollbackScratch
	entries []hostRollbackEntry
}

type hostRollbackEntry struct {
	destination        mutationnamespace.Destination
	existed            bool
	kind               string
	maximumFileBytes   int64
	backupPath         string
	backup             RollbackBackup
	fileMode           os.FileMode
	identity           mutationfs.EntryIdentity
	stagedState        recoveryWholePathState
	stagedWork         workmeasure.ArtifactWork
	effectMaximumKinds map[string]workmeasure.ArtifactWork
	effectState        recoveryWholePathState
	effectKnown        bool
	attempted          bool
}

type rollbackStageAction struct {
	actionIndex int
	destination mutationnamespace.Destination
	action      RestorationAction
}

type recoveryWholePathState struct {
	existed     bool
	kind        string
	contentHash string
	fileMode    os.FileMode
}

func (state recoveryWholePathState) equal(other recoveryWholePathState) bool {
	return state.existed == other.existed &&
		state.kind == other.kind &&
		state.contentHash == other.contentHash &&
		state.fileMode.Perm() == other.fileMode.Perm()
}

type recoveryGeneralPathReservation struct {
	budget *physicalwork.Budget
}

func (reservation recoveryGeneralPathReservation) AdmitPathComponents(count int) error {
	return reservation.budget.ReserveGeneralPathComponents(count)
}

func reserveRecoveryHostExecution(
	authority *Restoration,
	destination mutationnamespace.Destination,
	baseline *hostRollbackEntry,
	actions []rollbackStageAction,
) error {
	if authority == nil || authority.original == nil ||
		baseline == nil || !destination.IsRooted() {
		return fmt.Errorf("recovery host execution reservation requires bounded destination authority")
	}
	reservation := recoveryGeneralPathReservation{budget: authority.original}
	for range recoveryHostDestinationAccesses {
		if err := authority.operation.destinations.ReserveDestinationAccess(destination, reservation); err != nil {
			return fmt.Errorf("reserve recovery host destination access: %w", err)
		}
	}

	if baseline.existed {
		for range 2 {
			if err := reserveRecoveryObservation(
				authority.original,
				baseline.kind,
				baseline.stagedWork,
			); err != nil {
				return err
			}
		}
	}

	effectKinds := make(map[string]workmeasure.ArtifactWork)
	for _, action := range actions {
		kind, work, present, err := recoveryActionMaximumWork(action.action, baseline.maximumFileBytes)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		current, exists := effectKinds[kind]
		if !exists {
			effectKinds[kind] = work
			continue
		}
		effectKinds[kind] = maximumRecoveryArtifactWork(current, work)
	}
	baseline.effectMaximumKinds = make(map[string]workmeasure.ArtifactWork, len(effectKinds))
	for kind, work := range effectKinds {
		baseline.effectMaximumKinds[kind] = work
		// One postcondition read is used only by aggregate documents. Reserving it
		// for every group keeps the certificate independent of branch order.
		if err := reserveRecoveryObservation(authority.original, kind, work); err != nil {
			return err
		}
	}

	// A failed or indeterminate mutation can leave either the staged baseline or
	// any admitted effect kind visible. Reserve one fresh rollback observation
	// for every structurally possible kind before the first host effect.
	rollbackKinds := make(map[string]workmeasure.ArtifactWork, len(effectKinds)+1)
	if baseline.existed {
		rollbackKinds[baseline.kind] = baseline.stagedWork
	}
	for kind, work := range effectKinds {
		if current, exists := rollbackKinds[kind]; exists {
			work = maximumRecoveryArtifactWork(current, work)
		}
		rollbackKinds[kind] = work
	}
	for kind, work := range rollbackKinds {
		if err := reserveRecoveryObservation(authority.original, kind, work); err != nil {
			return err
		}
	}
	return nil
}

func recoveryActionMaximumWork(
	action RestorationAction,
	maximumDocumentBytes int64,
) (string, workmeasure.ArtifactWork, bool, error) {
	if action.ContentPath != "" {
		maximumBytes := maximumDocumentBytes
		if !action.BeforePathExisted {
			maximumBytes = 0
		}
		work, err := workmeasure.NewArtifactWork(0, maximumBytes)
		return recovery.PathKindFile, work, true, err
	}
	switch action.Kind {
	case recovery.ActionKindRestoreDelete:
		return "", workmeasure.ArtifactWork{}, false, nil
	case recovery.ActionKindRestoreWrite:
		return action.BackupKind, action.BackupWork, true, nil
	default:
		return "", workmeasure.ArtifactWork{}, false, fmt.Errorf(
			"recovery action %q has no execution work model",
			action.Kind,
		)
	}
}

func reserveRecoveryObservation(
	budget *physicalwork.Budget,
	kind string,
	work workmeasure.ArtifactWork,
) error {
	switch kind {
	case recovery.PathKindFile:
		return budget.ReserveGeneralFileObservation(work)
	case recovery.PathKindDirectory:
		return budget.ReserveGeneralDirectoryObservation(work)
	default:
		return fmt.Errorf("recovery observation kind %q is unsupported", kind)
	}
}

func admitRecoveryObservation(
	budget *physicalwork.Budget,
	kind string,
	work workmeasure.ArtifactWork,
) error {
	if budget == nil {
		return fmt.Errorf("general recovery execution budget is unavailable")
	}
	switch kind {
	case recovery.PathKindFile:
		readerBytes := max(int64(1), work.Bytes())
		readerWork, err := workmeasure.NewArtifactWork(0, readerBytes)
		if err != nil {
			return err
		}
		return budget.AdmitIndeterminateTreeWork(work, readerWork)
	case recovery.PathKindDirectory:
		readerWork, err := workmeasure.NewArtifactWork(work.Entries()+1, work.Bytes())
		if err != nil {
			return err
		}
		return budget.AdmitIndeterminateDirectoryWork(work, readerWork)
	default:
		return fmt.Errorf("recovery observation kind %q is unsupported", kind)
	}
}

func recoveryTraversalLimits(work workmeasure.ArtifactWork) (mutationfs.TreeTraversalLimits, error) {
	return mutationfs.NewTreeTraversalLimits(
		work.Entries(),
		recovery.MaximumArtifactTreeDepth,
		work.Bytes(),
	)
}

func maximumRecoveryArtifactWork(
	left workmeasure.ArtifactWork,
	right workmeasure.ArtifactWork,
) workmeasure.ArtifactWork {
	work, err := workmeasure.NewArtifactWork(
		max(left.Entries(), right.Entries()),
		max(left.Bytes(), right.Bytes()),
	)
	if err != nil {
		panic(err)
	}
	return work
}

func stageRollbackDestinations(
	ctx context.Context,
	authority *Restoration,
	actions []rollbackStageAction,
	rollbackDir string,
	index int,
	codecs aggregate.CodecCatalog,
) ([]hostRollbackEntry, error) {
	if len(actions) == 0 {
		return nil, fmt.Errorf("rollback stage requires at least one action")
	}
	physicalDestination := filepath.Clean(actions[0].destination.HostPath())
	for _, action := range actions {
		if !action.destination.IsRooted() {
			return nil, fmt.Errorf("rollback destination is invalid")
		}
		if filepath.Clean(action.destination.HostPath()) != physicalDestination {
			return nil, fmt.Errorf("rollback stage actions do not share one physical destination")
		}
	}
	return stageRootedRollbackDestinations(ctx, authority, actions, rollbackDir, index, codecs)
}

func stageRecoveryRollback(
	ctx context.Context,
	authority *Restoration,
	actions []RestorationAction,
	codecs aggregate.CodecCatalog,
) (hostRollback, error) {
	scratch, err := CreateRollbackScratch(authority.operation.filesystem)
	if err != nil {
		return hostRollback{}, err
	}

	rollback := hostRollback{
		scratch: scratch,
		entries: make([]hostRollbackEntry, len(actions)),
	}
	stageGroups := make([][]rollbackStageAction, 0, len(actions))
	stageGroupByDestination := make(map[string]int, len(actions))
	for index, action := range actions {
		logical, err := ParseRestorationDestination(action.Scope, action.Destination)
		if err != nil {
			return hostRollback{}, errors.Join(err, scratch.Abort(context.WithoutCancel(ctx), rollback.scratchArtifacts()))
		}
		destination, err := authority.operation.destinations.Resolve(action.Scope, logical)
		if err != nil {
			return hostRollback{}, errors.Join(err, scratch.Abort(context.WithoutCancel(ctx), rollback.scratchArtifacts()))
		}
		key := filepath.Clean(destination.HostPath())
		groupIndex, present := stageGroupByDestination[key]
		if !present {
			groupIndex = len(stageGroups)
			stageGroupByDestination[key] = groupIndex
			stageGroups = append(stageGroups, nil)
		}
		stageGroups[groupIndex] = append(stageGroups[groupIndex], rollbackStageAction{
			actionIndex: index,
			destination: destination,
			action:      action,
		})
	}
	for groupIndex, group := range stageGroups {
		entries, err := stageRollbackDestinations(ctx, authority, group, scratch.Directory(), groupIndex, codecs)
		if err != nil {
			return hostRollback{}, errors.Join(err, scratch.Abort(context.WithoutCancel(ctx), rollback.scratchArtifacts()))
		}
		if len(entries) != len(group) {
			return hostRollback{}, errors.Join(
				fmt.Errorf("rollback stage entry count %d does not match action count %d", len(entries), len(group)),
				scratch.Abort(context.WithoutCancel(ctx), rollback.scratchArtifacts()),
			)
		}
		for index, entry := range entries {
			rollback.entries[group[index].actionIndex] = entry
		}
	}

	if err := scratch.PrepareCleanup(ctx, authority.original, rollback.scratchArtifacts()); err != nil {
		return hostRollback{}, errors.Join(
			err,
			scratch.Abort(context.WithoutCancel(ctx), rollback.scratchArtifacts()),
		)
	}
	return rollback, nil
}

func (rollback hostRollback) scratchArtifacts() []ScratchArtifact {
	artifacts := make([]ScratchArtifact, 0, len(rollback.entries))
	for _, entry := range rollback.entries {
		if entry.existed && entry.backupPath != "" {
			artifacts = append(artifacts, ScratchArtifact{Path: entry.backupPath, Work: entry.stagedWork})
		}
	}
	return artifacts
}

func (rollback hostRollback) restore(
	ctx context.Context,
	authority *Restoration,
	budget *physicalwork.Budget,
	gate applyprogram.VisibilityGate,
) error {
	var failures []error
	groups := make(map[string][]hostRollbackEntry, len(rollback.entries))
	for _, entry := range rollback.entries {
		key := filepath.Clean(entry.destination.HostPath())
		groups[key] = append(groups[key], entry)
	}
	restored := make(map[string]struct{})
	for index := len(rollback.entries) - 1; index >= 0; index-- {
		key := filepath.Clean(rollback.entries[index].destination.HostPath())
		if _, present := restored[key]; present {
			continue
		}
		restored[key] = struct{}{}
		if !rollbackGroupAttempted(groups[key]) {
			continue
		}
		if err := gate.ValidateBefore(ctx); err != nil {
			failures = append(failures, fmt.Errorf("validate recovery rollback authority: %w", err))
			continue
		}
		if err := restoreRollbackGroup(ctx, authority, budget, groups[key]); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := gate.AcceptAfter(ctx); err != nil {
			failures = append(failures, fmt.Errorf("accept recovery rollback visibility: %w", err))
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}

	return nil
}

func rollbackGroupAttempted(entries []hostRollbackEntry) bool {
	for _, entry := range entries {
		if entry.attempted {
			return true
		}
	}
	return false
}

func restoreRollbackGroup(
	ctx context.Context,
	authority *Restoration,
	budget *physicalwork.Budget,
	entries []hostRollbackEntry,
) error {
	if len(entries) == 0 {
		return nil
	}
	baseline := entries[0]
	attempted := false
	for _, entry := range entries {
		if !entry.stagedState.equal(baseline.stagedState) {
			return fmt.Errorf("rollback stage for %q has inconsistent physical baselines", baseline.destination.Logical())
		}
		if entry.maximumFileBytes != baseline.maximumFileBytes {
			return fmt.Errorf("rollback stage for %q has inconsistent file byte limits", baseline.destination.Logical())
		}
		attempted = attempted || entry.attempted
	}
	if !attempted {
		return nil
	}
	maximumKinds := make(map[string]workmeasure.ArtifactWork, len(baseline.effectMaximumKinds)+1)
	maps.Copy(maximumKinds, baseline.effectMaximumKinds)
	if baseline.existed {
		if work, present := maximumKinds[baseline.kind]; present {
			maximumKinds[baseline.kind] = maximumRecoveryArtifactWork(work, baseline.stagedWork)
		} else {
			maximumKinds[baseline.kind] = baseline.stagedWork
		}
	}
	current, identity, err := observeRecoveryWholePathState(
		ctx,
		authority,
		budget,
		baseline.destination,
		baseline.maximumFileBytes,
		maximumKinds,
	)
	if err != nil {
		return fmt.Errorf(
			"rollback destination %q changed outside the recovery attempt: %w",
			baseline.destination.Logical(),
			err,
		)
	}
	if current.equal(baseline.stagedState) {
		return nil
	}
	matchesEffect := false
	for _, entry := range entries {
		if entry.attempted && entry.effectKnown && current.equal(entry.effectState) {
			matchesEffect = true
			break
		}
	}
	if !matchesEffect {
		return fmt.Errorf("rollback destination %q changed outside the recovery attempt", baseline.destination.Logical())
	}
	if !baseline.existed {
		if err := authority.control.Validate(ctx); err != nil {
			return fmt.Errorf("validate recovery compensation semantics: %w", err)
		}
		if err := authority.operation.RemoveAgainst(ctx, baseline.destination, identity); err != nil {
			return fmt.Errorf("rollback remove %q: %w", baseline.destination.Logical(), err)
		}
		return nil
	}
	switch baseline.kind {
	case recovery.PathKindFile:
		if err := admitRecoveryObservation(
			budget,
			baseline.kind,
			baseline.stagedWork,
		); err != nil {
			return err
		}
		content, err := baseline.backup.ReadFile(ctx, max(int64(1), baseline.stagedWork.Bytes()))
		if err != nil {
			return fmt.Errorf("read rollback file for %q: %w", baseline.destination.Logical(), err)
		}
		if err := authority.control.Validate(ctx); err != nil {
			return fmt.Errorf("validate recovery compensation semantics: %w", err)
		}
		if err := authority.operation.CommitFileAgainst(
			ctx,
			baseline.destination,
			content,
			baseline.fileMode,
			current.existed,
			identity,
		); err != nil {
			return fmt.Errorf("rollback restore file %q: %w", baseline.destination.Logical(), err)
		}
	case recovery.PathKindDirectory:
		if err := admitRecoveryObservation(
			budget,
			baseline.kind,
			baseline.stagedWork,
		); err != nil {
			return err
		}
		if err := authority.control.Validate(ctx); err != nil {
			return fmt.Errorf("validate recovery compensation semantics: %w", err)
		}
		if err := authority.operation.CommitDirectoryAgainst(
			ctx,
			baseline.destination,
			current.existed,
			identity,
			func(writer mutationfs.RootedTreeWriter) error {
				return baseline.backup.CopyDirectory(ctx, writer, baseline.stagedWork)
			},
		); err != nil {
			return fmt.Errorf("rollback restore directory %q: %w", baseline.destination.Logical(), err)
		}
	default:
		return fmt.Errorf("rollback kind %q for %q is not supported", baseline.kind, baseline.destination.Logical())
	}
	return nil
}
