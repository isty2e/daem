package execute

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

const (
	recoveryBaselineObservationAccesses = 3
	recoveryHostMutationAccesses        = 4
	recoveryRollbackObservationAccesses = 3
	recoveryRollbackMutationAccesses    = 4
	recoveryPostconditionAccesses       = 2
	maximumRollbackScratchTreeDepth     = recovery.MaximumArtifactTreeDepth + 1
	recoveryHostDestinationAccesses     = recoveryBaselineObservationAccesses +
		recoveryHostMutationAccesses +
		recoveryRollbackObservationAccesses +
		recoveryRollbackMutationAccesses +
		recoveryPostconditionAccesses
)

type hostRollback struct {
	dir          string
	entries      []hostRollbackEntry
	cleanupState *preparedRollbackCleanup
}

type preparedRollbackCleanup struct {
	root      *rootedpath.CapturedRoot
	authority *rootedpath.EntryAuthority
	identity  mutationfs.EntryIdentity
	limits    mutationfs.TreeTraversalLimits
	complete  bool
}

type hostRollbackEntry struct {
	destination        mutationDestination
	existed            bool
	kind               string
	maximumFileBytes   int64
	backupPath         string
	backup             rollbackBackup
	fileMode           os.FileMode
	identity           mutationfs.EntryIdentity
	stagedState        recoveryWholePathState
	stagedWork         recovery.ArtifactWork
	effectMaximumKinds map[string]recovery.ArtifactWork
	effectState        recoveryWholePathState
	effectKnown        bool
	attempted          bool
}

type rollbackStageAction struct {
	actionIndex int
	destination mutationDestination
	action      recoveryHostAction
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
	budget *recovery.PhysicalWorkBudget
}

type recoveryScratchPathReservation struct {
	budget *recovery.PhysicalWorkBudget
}

func (reservation recoveryScratchPathReservation) AdmitPathComponents(count int) error {
	return reservation.budget.ReserveScratchPathComponents(count)
}

func (reservation recoveryGeneralPathReservation) AdmitPathComponents(count int) error {
	return reservation.budget.ReserveGeneralPathComponents(count)
}

func reserveRecoveryHostExecution(
	authority *mutationAuthority,
	destination mutationDestination,
	baseline *hostRollbackEntry,
	actions []rollbackStageAction,
) error {
	if authority == nil || authority.physicalWorkBudget == nil ||
		baseline == nil || !destination.isRooted() {
		return fmt.Errorf("recovery host execution reservation requires bounded destination authority")
	}
	reservation := recoveryGeneralPathReservation{budget: authority.physicalWorkBudget}
	for range recoveryHostDestinationAccesses {
		if err := destination.root.ReserveDestinationAccess(
			destination.destination,
			recovery.MaximumPhysicalPathDepth,
			reservation,
		); err != nil {
			return fmt.Errorf("reserve recovery host destination access: %w", err)
		}
	}

	if baseline.existed {
		for range 2 {
			if err := reserveRecoveryObservation(
				authority.physicalWorkBudget,
				baseline.kind,
				baseline.stagedWork,
			); err != nil {
				return err
			}
		}
	}

	effectKinds := make(map[string]recovery.ArtifactWork)
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
	baseline.effectMaximumKinds = make(map[string]recovery.ArtifactWork, len(effectKinds))
	for kind, work := range effectKinds {
		baseline.effectMaximumKinds[kind] = work
		// One postcondition read is used only by aggregate documents. Reserving it
		// for every group keeps the certificate independent of branch order.
		if err := reserveRecoveryObservation(authority.physicalWorkBudget, kind, work); err != nil {
			return err
		}
	}

	// A failed or indeterminate mutation can leave either the staged baseline or
	// any admitted effect kind visible. Reserve one fresh rollback observation
	// for every structurally possible kind before the first host effect.
	rollbackKinds := make(map[string]recovery.ArtifactWork, len(effectKinds)+1)
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
		if err := reserveRecoveryObservation(authority.physicalWorkBudget, kind, work); err != nil {
			return err
		}
	}
	return nil
}

func recoveryActionMaximumWork(
	action recoveryHostAction,
	maximumDocumentBytes int64,
) (string, recovery.ArtifactWork, bool, error) {
	if action.ContentPath != "" {
		maximumBytes := maximumDocumentBytes
		if !action.BeforePathExisted {
			maximumBytes = 0
		}
		work, err := recovery.NewArtifactWork(0, maximumBytes)
		return recovery.PathKindFile, work, true, err
	}
	switch action.Kind {
	case recovery.ActionKindRestoreDelete:
		return "", recovery.ArtifactWork{}, false, nil
	case recovery.ActionKindRestoreWrite:
		return action.BackupKind, action.BackupWork, true, nil
	default:
		return "", recovery.ArtifactWork{}, false, fmt.Errorf(
			"recovery action %q has no execution work model",
			action.Kind,
		)
	}
}

func reserveRecoveryObservation(
	budget *recovery.PhysicalWorkBudget,
	kind string,
	work recovery.ArtifactWork,
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
	budget *recovery.PhysicalWorkBudget,
	kind string,
	work recovery.ArtifactWork,
) error {
	if budget == nil {
		return fmt.Errorf("general recovery execution budget is unavailable")
	}
	switch kind {
	case recovery.PathKindFile:
		readerBytes := max(int64(1), work.Bytes())
		readerWork, err := recovery.NewArtifactWork(0, readerBytes)
		if err != nil {
			return err
		}
		return budget.AdmitIndeterminateTreeWork(work, readerWork)
	case recovery.PathKindDirectory:
		readerWork, err := recovery.NewArtifactWork(work.Entries()+1, work.Bytes())
		if err != nil {
			return err
		}
		return budget.AdmitIndeterminateDirectoryWork(work, readerWork)
	default:
		return fmt.Errorf("recovery observation kind %q is unsupported", kind)
	}
}

func recoveryTraversalLimits(work recovery.ArtifactWork) (mutationfs.TreeTraversalLimits, error) {
	return mutationfs.NewTreeTraversalLimits(
		work.Entries(),
		recovery.MaximumArtifactTreeDepth,
		work.Bytes(),
	)
}

func maximumRecoveryArtifactWork(
	left recovery.ArtifactWork,
	right recovery.ArtifactWork,
) recovery.ArtifactWork {
	work, err := recovery.NewArtifactWork(
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
	authority *mutationAuthority,
	actions []rollbackStageAction,
	rollbackDir string,
	index int,
	codecs aggregate.CodecCatalog,
) ([]hostRollbackEntry, error) {
	if len(actions) == 0 {
		return nil, fmt.Errorf("rollback stage requires at least one action")
	}
	physicalDestination := filepath.Clean(actions[0].destination.hostPath)
	for _, action := range actions {
		if !action.destination.isRooted() {
			return nil, fmt.Errorf("rollback destination is invalid")
		}
		if filepath.Clean(action.destination.hostPath) != physicalDestination {
			return nil, fmt.Errorf("rollback stage actions do not share one physical destination")
		}
	}
	return stageRootedRollbackDestinations(ctx, authority, actions, rollbackDir, index, codecs)
}

func stageRecoveryRollback(
	ctx context.Context,
	authority *mutationAuthority,
	actions []recoveryHostAction,
	codecs aggregate.CodecCatalog,
) (hostRollback, error) {
	rollbackDir, err := os.MkdirTemp("", "daem-recovery-rollback-")
	if err != nil {
		return hostRollback{}, fmt.Errorf("create recovery rollback directory: %w", err)
	}

	rollback := hostRollback{
		dir:     rollbackDir,
		entries: make([]hostRollbackEntry, len(actions)),
	}
	stageGroups := make([][]rollbackStageAction, 0, len(actions))
	stageGroupByDestination := make(map[string]int, len(actions))
	for index, action := range actions {
		logical, err := recoveryDestination(action.Scope, action.Destination)
		if err != nil {
			return hostRollback{}, errors.Join(err, rollback.abortCleanup(context.WithoutCancel(ctx), authority))
		}
		destination, err := authority.resolveBoundDestination(action.Scope, logical)
		if err != nil {
			return hostRollback{}, errors.Join(err, rollback.abortCleanup(context.WithoutCancel(ctx), authority))
		}
		key := filepath.Clean(destination.hostPath)
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
		entries, err := stageRollbackDestinations(ctx, authority, group, rollbackDir, groupIndex, codecs)
		if err != nil {
			return hostRollback{}, errors.Join(err, rollback.abortCleanup(context.WithoutCancel(ctx), authority))
		}
		if len(entries) != len(group) {
			return hostRollback{}, errors.Join(
				fmt.Errorf("rollback stage entry count %d does not match action count %d", len(entries), len(group)),
				rollback.abortCleanup(context.WithoutCancel(ctx), authority),
			)
		}
		for index, entry := range entries {
			rollback.entries[group[index].actionIndex] = entry
		}
	}

	if err := rollback.prepareCleanup(ctx, authority); err != nil {
		return hostRollback{}, errors.Join(
			err,
			rollback.abortCleanup(context.WithoutCancel(ctx), authority),
		)
	}
	return rollback, nil
}

func (rollback hostRollback) restore(
	ctx context.Context,
	authority *mutationAuthority,
	gate visibilityEffectGate,
) error {
	var failures []error
	groups := make(map[string][]hostRollbackEntry, len(rollback.entries))
	for _, entry := range rollback.entries {
		key := filepath.Clean(entry.destination.hostPath)
		groups[key] = append(groups[key], entry)
	}
	restored := make(map[string]struct{})
	for index := len(rollback.entries) - 1; index >= 0; index-- {
		key := filepath.Clean(rollback.entries[index].destination.hostPath)
		if _, present := restored[key]; present {
			continue
		}
		restored[key] = struct{}{}
		if !rollbackGroupAttempted(groups[key]) {
			continue
		}
		if err := gate.validateBefore(ctx); err != nil {
			failures = append(failures, fmt.Errorf("validate recovery rollback authority: %w", err))
			continue
		}
		if err := restoreRollbackGroup(ctx, authority, groups[key]); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := gate.acceptAfter(ctx); err != nil {
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

func (rollback *hostRollback) prepareCleanup(
	ctx context.Context,
	authority *mutationAuthority,
) (returnErr error) {
	if rollback == nil || rollback.dir == "" {
		return fmt.Errorf("recovery rollback stage is unavailable")
	}
	if rollback.cleanupState != nil {
		return fmt.Errorf("recovery rollback cleanup is already prepared")
	}
	if authority == nil || authority.physicalWorkBudget == nil || authority.filesystem == nil {
		return fmt.Errorf("recovery rollback cleanup requires bounded mutation authority")
	}
	work, err := rollback.cleanupWork()
	if err != nil {
		return err
	}
	root, destination, err := rootedpath.CaptureDestinationBounded(
		rollback.dir,
		recovery.MaximumPhysicalPathDepth,
		authority.physicalWorkBudget,
	)
	if err != nil {
		return fmt.Errorf("capture recovery rollback cleanup authority: %w", err)
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, root.Close())
		}
	}()

	reservation := recoveryScratchPathReservation{budget: authority.physicalWorkBudget}
	for range 3 {
		if err := root.ReserveDestinationAccess(
			destination,
			recovery.MaximumPhysicalPathDepth,
			reservation,
		); err != nil {
			return fmt.Errorf("reserve recovery rollback cleanup path: %w", err)
		}
	}
	if err := authority.physicalWorkBudget.ReserveScratchCleanup(work); err != nil {
		return fmt.Errorf("reserve recovery rollback cleanup tree: %w", err)
	}
	executionBudget, reservedWork, err := authority.physicalWorkBudget.BeginReservedScratchCleanup()
	if err != nil {
		return err
	}
	entryAuthority, err := rootedpath.BindCapturedEntryAuthorityBounded(
		root,
		destination,
		recovery.MaximumPhysicalPathDepth,
		executionBudget,
	)
	if err != nil {
		return fmt.Errorf("bind recovery rollback cleanup entry: %w", err)
	}
	capability, err := entryAuthority.Acquire()
	if err != nil {
		return errors.Join(err, entryAuthority.Close())
	}
	identity, err := authority.filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		return errors.Join(err, capability.Close(), entryAuthority.Close())
	}
	if identity.Kind() != mutationfs.EntryKindDirectory {
		return errors.Join(
			fmt.Errorf("recovery rollback stage is not a directory"),
			capability.Close(),
			entryAuthority.Close(),
		)
	}
	if err := capability.Close(); err != nil {
		return errors.Join(err, entryAuthority.Close())
	}
	limits, err := mutationfs.NewTreeTraversalLimits(
		reservedWork.Entries(),
		maximumRollbackScratchTreeDepth,
		reservedWork.Bytes(),
	)
	if err != nil {
		return errors.Join(err, entryAuthority.Close())
	}
	rollback.cleanupState = &preparedRollbackCleanup{
		root: root, authority: entryAuthority, identity: identity, limits: limits,
	}
	return nil
}

func (rollback hostRollback) cleanupWork() (recovery.ArtifactWork, error) {
	seen := make(map[string]struct{}, len(rollback.entries))
	entries := 0
	var bytes int64
	for _, entry := range rollback.entries {
		if !entry.existed || entry.backupPath == "" {
			continue
		}
		backupPath := filepath.Clean(entry.backupPath)
		if backupPath == rollback.dir ||
			!strings.HasPrefix(backupPath, rollback.dir+string(filepath.Separator)) {
			return recovery.ArtifactWork{}, fmt.Errorf("rollback backup escaped its private stage")
		}
		if _, duplicate := seen[backupPath]; duplicate {
			continue
		}
		seen[backupPath] = struct{}{}
		entries += entry.stagedWork.Entries() + 1
		bytes += entry.stagedWork.Bytes()
	}
	return recovery.NewArtifactWork(entries, bytes)
}

func (rollback *hostRollback) cleanup(
	ctx context.Context,
	authority *mutationAuthority,
) error {
	if rollback == nil || rollback.dir == "" {
		return nil
	}
	if rollback.cleanupState == nil {
		return fmt.Errorf("recovery rollback cleanup was not prepared")
	}
	if rollback.cleanupState.complete {
		return nil
	}
	if authority == nil || authority.filesystem == nil {
		return fmt.Errorf("recovery rollback cleanup filesystem is unavailable")
	}
	return rollback.cleanupState.execute(ctx, authority.filesystem)
}

func (cleanup *preparedRollbackCleanup) execute(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
) (returnErr error) {
	if cleanup == nil || cleanup.root == nil || cleanup.authority == nil || cleanup.identity == nil {
		return fmt.Errorf("recovery rollback cleanup is uninitialized")
	}
	defer func() {
		returnErr = errors.Join(returnErr, cleanup.authority.Close(), cleanup.root.Close())
		if returnErr == nil {
			cleanup.complete = true
		}
	}()
	capability, err := cleanup.authority.Acquire()
	if err != nil {
		return err
	}
	current, err := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		return errors.Join(err, capability.Close())
	}
	if !cleanup.identity.Equal(current) {
		return errors.Join(
			fmt.Errorf("recovery rollback stage changed before cleanup"),
			capability.Close(),
		)
	}
	outcome, err := filesystem.CleanupRootedEntry(
		ctx,
		capability,
		current,
		cleanup.limits,
	)
	if err != nil {
		return fmt.Errorf(
			"cleanup recovery rollback stage (%s): %w",
			outcome.State(),
			err,
		)
	}
	return nil
}

func (rollback *hostRollback) abortCleanup(
	ctx context.Context,
	authority *mutationAuthority,
) error {
	if rollback == nil || rollback.dir == "" {
		return nil
	}
	if rollback.cleanupState != nil {
		return rollback.cleanup(ctx, authority)
	}
	if authority == nil || authority.filesystem == nil {
		return fmt.Errorf("recovery rollback abort cleanup filesystem is unavailable")
	}
	budget, err := recovery.NewPhysicalWorkBudget(0)
	if err != nil {
		return err
	}
	root, destination, err := rootedpath.CaptureDestinationBounded(
		rollback.dir,
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		return err
	}
	entryAuthority, err := rootedpath.BindCapturedEntryAuthorityBounded(
		root,
		destination,
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		return errors.Join(err, root.Close())
	}
	capability, err := entryAuthority.Acquire()
	if err != nil {
		return errors.Join(err, entryAuthority.Close(), root.Close())
	}
	identity, err := authority.filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		return errors.Join(err, capability.Close(), entryAuthority.Close(), root.Close())
	}
	limits, err := mutationfs.NewTreeTraversalLimits(
		recovery.MaximumPhysicalEntries,
		maximumRollbackScratchTreeDepth,
		recovery.MaximumPhysicalBytes,
	)
	if err != nil {
		return errors.Join(err, capability.Close(), entryAuthority.Close(), root.Close())
	}
	outcome, cleanupErr := authority.filesystem.CleanupRootedEntry(
		ctx,
		capability,
		identity,
		limits,
	)
	return errors.Join(
		func() error {
			if cleanupErr == nil {
				return nil
			}
			return fmt.Errorf("abort recovery rollback stage (%s): %w", outcome.State(), cleanupErr)
		}(),
		entryAuthority.Close(),
		root.Close(),
	)
}

func restoreRollbackGroup(
	ctx context.Context,
	authority *mutationAuthority,
	entries []hostRollbackEntry,
) error {
	if len(entries) == 0 {
		return nil
	}
	baseline := entries[0]
	attempted := false
	for _, entry := range entries {
		if !entry.stagedState.equal(baseline.stagedState) {
			return fmt.Errorf("rollback stage for %q has inconsistent physical baselines", baseline.destination.logical)
		}
		if entry.maximumFileBytes != baseline.maximumFileBytes {
			return fmt.Errorf("rollback stage for %q has inconsistent file byte limits", baseline.destination.logical)
		}
		attempted = attempted || entry.attempted
	}
	if !attempted {
		return nil
	}
	maximumKinds := make(map[string]recovery.ArtifactWork, len(baseline.effectMaximumKinds)+1)
	for kind, work := range baseline.effectMaximumKinds {
		maximumKinds[kind] = work
	}
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
		baseline.destination,
		baseline.maximumFileBytes,
		maximumKinds,
	)
	if err != nil {
		return fmt.Errorf(
			"rollback destination %q changed outside the recovery attempt: %w",
			baseline.destination.logical,
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
		return fmt.Errorf("rollback destination %q changed outside the recovery attempt", baseline.destination.logical)
	}
	if !baseline.existed {
		if err := authority.validateRecoverySemanticWitness(ctx); err != nil {
			return fmt.Errorf("validate recovery compensation semantics: %w", err)
		}
		if err := removeDestinationAgainst(ctx, authority, baseline.destination, identity); err != nil {
			return fmt.Errorf("rollback remove %q: %w", baseline.destination.logical, err)
		}
		return nil
	}
	switch baseline.kind {
	case recovery.PathKindFile:
		if err := admitRecoveryObservation(
			authority.generalExecutionWorkBudget,
			baseline.kind,
			baseline.stagedWork,
		); err != nil {
			return err
		}
		content, err := baseline.backup.readFile(ctx, max(int64(1), baseline.stagedWork.Bytes()))
		if err != nil {
			return fmt.Errorf("read rollback file for %q: %w", baseline.destination.logical, err)
		}
		if err := authority.validateRecoverySemanticWitness(ctx); err != nil {
			return fmt.Errorf("validate recovery compensation semantics: %w", err)
		}
		if err := commitFileDestinationAgainst(
			ctx,
			authority,
			baseline.destination,
			content,
			baseline.fileMode,
			current.existed,
			identity,
		); err != nil {
			return fmt.Errorf("rollback restore file %q: %w", baseline.destination.logical, err)
		}
	case recovery.PathKindDirectory:
		if err := admitRecoveryObservation(
			authority.generalExecutionWorkBudget,
			baseline.kind,
			baseline.stagedWork,
		); err != nil {
			return err
		}
		if err := authority.validateRecoverySemanticWitness(ctx); err != nil {
			return fmt.Errorf("validate recovery compensation semantics: %w", err)
		}
		if err := commitRecoveryDirectoryDestinationAgainst(
			ctx,
			authority,
			boundedRollbackDirectorySource{
				backup: baseline.backup,
				work:   baseline.stagedWork,
			},
			baseline.destination,
			current.existed,
			identity,
		); err != nil {
			return fmt.Errorf("rollback restore directory %q: %w", baseline.destination.logical, err)
		}
	default:
		return fmt.Errorf("rollback kind %q for %q is not supported", baseline.kind, baseline.destination.logical)
	}
	return nil
}

func rollbackError(primary error, rollback error) error {
	if rollback == nil {
		return primary
	}

	return fmt.Errorf("%w; rollback failed: %v", primary, rollback)
}
