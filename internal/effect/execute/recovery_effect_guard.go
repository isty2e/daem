package execute

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func recoveryDocumentWholeState(document aggregate.Document, mode os.FileMode) recoveryWholePathState {
	if !document.Exists() {
		return recoveryWholePathState{}
	}
	return recoveryWholePathState{
		existed: true, kind: recovery.PathKindFile,
		contentHash: string(artifact.HashFileContentWithExecutable(
			document.Content(),
			mode.Perm()&0o111 != 0,
		)),
		fileMode: mode.Perm(),
	}
}

func validateRecoveryWholeFileInput(
	destination string,
	expected recoveryWholePathState,
	content []byte,
	mode os.FileMode,
	exists bool,
) error {
	current := recoveryWholePathState{}
	if exists {
		current = recoveryWholePathState{
			existed: true,
			kind:    recovery.PathKindFile,
			contentHash: string(artifact.HashFileContentWithExecutable(
				content,
				mode.Perm()&0o111 != 0,
			)),
			fileMode: mode.Perm(),
		}
	}
	if !current.equal(expected) {
		return fmt.Errorf(
			"recovery destination %q changed between recovery actions",
			destination,
		)
	}
	return nil
}

func validateRecoveryExpectedAbsent(action recoveryHostAction, codecs aggregate.CodecCatalog) error {
	if action.ContentPath != "" {
		return validateRecoveryExpectedContentPath(action, nil, 0, false, codecs)
	}
	if !action.ExpectedAfter.Equal(recovery.ExpectedPathState{}) {
		return fmt.Errorf("recovery destination %q differs from expected absent state", action.Destination)
	}
	return nil
}

func validateRecoveryExpectedFile(
	action recoveryHostAction,
	content []byte,
	mode os.FileMode,
	codecs aggregate.CodecCatalog,
) error {
	if action.ContentPath != "" {
		return validateRecoveryExpectedContentPath(action, content, mode, true, codecs)
	}
	expected := action.ExpectedAfter
	if !expected.Existed ||
		expected.PathExisted ||
		expected.Kind != recovery.PathKindFile ||
		expected.LinkTarget != "" ||
		expected.PathMode == nil ||
		expected.PathMode.FileMode().Perm() != mode.Perm() {
		return fmt.Errorf("recovery file %q differs from expected kind or mode", action.Destination)
	}
	contentHash := string(artifact.HashFileContentWithExecutable(
		content,
		mode.Perm()&0o111 != 0,
	))
	if contentHash != expected.ContentHash {
		return fmt.Errorf(
			"recovery file %q hash %q does not match expected %q",
			action.Destination,
			contentHash,
			expected.ContentHash,
		)
	}
	return nil
}

func validateRecoveryExpectedDirectory(
	action recoveryHostAction,
	contentHash string,
	kind string,
) error {
	expected := action.ExpectedAfter
	if action.ContentPath != "" ||
		!expected.Existed ||
		expected.PathExisted ||
		kind != recovery.PathKindDirectory ||
		expected.Kind != recovery.PathKindDirectory ||
		expected.PathMode != nil ||
		expected.LinkTarget != "" ||
		contentHash != expected.ContentHash {
		return fmt.Errorf("recovery directory %q differs from expected-after state", action.Destination)
	}
	return nil
}

func validateRecoveryExpectedContentPath(
	action recoveryHostAction,
	content []byte,
	mode os.FileMode,
	pathExists bool,
	codecs aggregate.CodecCatalog,
) error {
	expected := action.ExpectedAfter
	if pathExists != expected.PathExisted {
		return fmt.Errorf("recovery document presence changed after final planning for %q", action.Destination)
	}
	if pathExists {
		if expected.PathMode == nil || expected.PathMode.FileMode().Perm() != mode.Perm() {
			return fmt.Errorf("recovery document %q mode changed after final planning", action.Destination)
		}
	} else if expected.PathMode != nil {
		return fmt.Errorf("absent recovery document %q carries an expected mode", action.Destination)
	}

	projection, present, err := recoveryContentPathProjection(action, content, pathExists, codecs)
	if err != nil {
		return err
	}
	if present != expected.Existed {
		return fmt.Errorf("recovery selected state %q presence changed after final planning", action.ContentPath)
	}
	if !present {
		if expected.Kind != "" || expected.ContentHash != "" || expected.LinkTarget != "" {
			return fmt.Errorf("absent recovery selected state %q carries unexpected identity", action.ContentPath)
		}
		return nil
	}
	hash := string(artifact.HashFileContent(projection))
	if expected.Kind != recovery.PathKindFile ||
		expected.LinkTarget != "" ||
		hash != expected.ContentHash {
		return fmt.Errorf("recovery selected state %q changed after final planning", action.ContentPath)
	}
	return nil
}

func recoveryContentPathProjection(
	action recoveryHostAction,
	content []byte,
	pathExists bool,
	codecs aggregate.CodecCatalog,
) ([]byte, bool, error) {
	if action.AggregateContract == nil {
		return nil, false, fmt.Errorf(
			"recovery content path %q has no aggregate contract",
			action.ContentPath,
		)
	}
	contract := action.AggregateContract.Clone()
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		return nil, false, err
	}
	codec, ok := codecs.Lookup(contract.CodecContractID())
	if !ok {
		return nil, false, fmt.Errorf("aggregate recovery codec %q is not admitted", contract.CodecContractID())
	}
	document := aggregate.AbsentDocument()
	if pathExists {
		document = aggregate.ExistingDocument(content)
	}
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		return nil, false, failure
	}
	state := snapshot.States()[0]
	return []byte(state.CanonicalProjection()), state.Present(), nil
}

func stageRootedRollbackDestinations(
	ctx context.Context,
	authority *mutationAuthority,
	actions []rollbackStageAction,
	rollbackDir string,
	index int,
	codecs aggregate.CodecCatalog,
) (result []hostRollbackEntry, resultErr error) {
	if len(actions) == 0 {
		return nil, fmt.Errorf("rooted rollback stage requires at least one action")
	}
	maximumFileBytes, err := recoveryStageMaximumFileBytes(actions, codecs)
	if err != nil {
		return nil, err
	}
	destination := actions[0].destination
	finish := func(baseline hostRollbackEntry) ([]hostRollbackEntry, error) {
		if err := reserveRecoveryHostExecution(authority, destination, &baseline, actions); err != nil {
			return nil, err
		}
		return rollbackEntriesForStageActions(baseline, actions), nil
	}
	capability, err := authority.acquire(destination)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := capability.Close(); closeErr != nil {
			result = nil
			resultErr = errors.Join(resultErr, closeErr)
		}
	}()

	identity, err := authority.filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if errors.Is(err, os.ErrNotExist) {
		for _, action := range actions {
			if err := validateRecoveryExpectedAbsent(action.action, codecs); err != nil {
				return nil, err
			}
		}
		return finish(hostRollbackEntry{
			maximumFileBytes: maximumFileBytes,
			stagedState:      recoveryWholePathState{},
		})
	}
	if err != nil {
		return nil, fmt.Errorf("inspect rooted rollback source %q: %w", destination.logical, err)
	}

	backupPath := filepath.Join(rollbackDir, fmt.Sprintf("%06d", index))
	baseline := hostRollbackEntry{
		existed:          true,
		maximumFileBytes: maximumFileBytes,
		backupPath:       backupPath,
	}
	switch identity.Kind() {
	case mutationfs.EntryKindFile:
		readLimit := min(maximumFileBytes, authority.physicalWorkBudget.RemainingBytes())
		if readLimit <= 0 {
			readLimit = 1
		}
		content, mode, captured, err := authority.filesystem.ReadRootedRegularFileUpTo(
			ctx,
			capability,
			readLimit,
		)
		if err != nil {
			return nil, fmt.Errorf("read rooted rollback source %q: %w", destination.logical, err)
		}
		if !identity.Equal(captured) {
			return nil, fmt.Errorf("rooted rollback source %q changed while staging", destination.logical)
		}
		work, err := recovery.NewArtifactWork(0, int64(len(content)))
		if err != nil {
			return nil, err
		}
		if err := authority.physicalWorkBudget.AdmitTree(work); err != nil {
			return nil, fmt.Errorf("charge rooted rollback file staging: %w", err)
		}
		for _, action := range actions {
			if err := validateRecoveryExpectedFile(action.action, content, mode, codecs); err != nil {
				return nil, err
			}
		}
		if err := os.WriteFile(backupPath, content, 0o600); err != nil {
			return nil, fmt.Errorf("stage rooted rollback file %q: %w", destination.logical, err)
		}
		baseline.kind = recovery.PathKindFile
		baseline.stagedWork = work
		baseline.fileMode = mode.Perm()
		baseline.identity = captured
		baseline.stagedState = recoveryWholePathState{
			existed: true, kind: recovery.PathKindFile,
			contentHash: string(artifact.HashFileContentWithExecutable(content, mode.Perm()&0o111 != 0)),
			fileMode:    mode.Perm(),
		}
		baseline.backup, err = newRollbackBackup(
			backupPath,
			filepath.Base(backupPath),
			baseline.kind,
			baseline.stagedState.contentHash,
		)
		if err != nil {
			return nil, fmt.Errorf("bind rooted rollback file %q: %w", destination.logical, err)
		}
		return finish(baseline)
	case mutationfs.EntryKindDirectory:
		sink := newRollbackTreeBackupSink(ctx, backupPath)
		limits, err := recoveryStagingTraversalLimits(authority.physicalWorkBudget)
		if err != nil {
			return nil, err
		}
		captured, err := authority.filesystem.SnapshotRootedDirectory(
			ctx,
			capability,
			limits,
			sink,
		)
		if err != nil {
			return nil, fmt.Errorf("stage rooted rollback directory %q: %w", destination.logical, err)
		}
		if !identity.Equal(captured) {
			return nil, fmt.Errorf("rooted rollback source %q changed while staging", destination.logical)
		}
		if err := sink.finalize(); err != nil {
			return nil, fmt.Errorf("finalize rooted rollback directory %q: %w", destination.logical, err)
		}
		work, err := sink.artifactWork()
		if err != nil {
			return nil, err
		}
		if err := authority.physicalWorkBudget.AdmitTree(work); err != nil {
			return nil, fmt.Errorf("charge rooted rollback directory staging: %w", err)
		}
		contentHash := sink.contentHash()
		for _, action := range actions {
			if err := validateRecoveryExpectedDirectory(
				action.action,
				string(contentHash),
				string(artifact.ArtifactKindDirectory),
			); err != nil {
				return nil, err
			}
		}
		baseline.kind = recovery.PathKindDirectory
		baseline.stagedWork = work
		baseline.identity = captured
		baseline.stagedState = recoveryWholePathState{
			existed: true, kind: recovery.PathKindDirectory, contentHash: string(contentHash),
		}
		baseline.backup, err = newRollbackBackup(
			backupPath,
			filepath.Base(backupPath),
			baseline.kind,
			baseline.stagedState.contentHash,
		)
		if err != nil {
			return nil, fmt.Errorf("bind rooted rollback directory %q: %w", destination.logical, err)
		}
		return finish(baseline)
	default:
		return nil, fmt.Errorf(
			"rooted rollback source %q has unsupported kind %q",
			destination.logical,
			identity.Kind(),
		)
	}
}

func rollbackEntriesForStageActions(
	baseline hostRollbackEntry,
	actions []rollbackStageAction,
) []hostRollbackEntry {
	entries := make([]hostRollbackEntry, len(actions))
	for index, action := range actions {
		entry := baseline
		entry.destination = action.destination
		entries[index] = entry
	}
	return entries
}

func observeRecoveryWholePathState(
	ctx context.Context,
	authority *mutationAuthority,
	destination mutationDestination,
	maximumFileBytes int64,
	maximumKinds map[string]recovery.ArtifactWork,
) (result recoveryWholePathState, identity mutationfs.EntryIdentity, resultErr error) {
	if maximumFileBytes <= 0 {
		return recoveryWholePathState{}, nil, fmt.Errorf("recovery observation file byte limit must be positive")
	}
	capability, err := authority.acquire(destination)
	if err != nil {
		return recoveryWholePathState{}, nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, capability.Close())
	}()
	identity, err = authority.filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if errors.Is(err, os.ErrNotExist) {
		return recoveryWholePathState{}, nil, nil
	}
	if err != nil {
		return recoveryWholePathState{}, nil, err
	}
	kind := ""
	switch identity.Kind() {
	case mutationfs.EntryKindFile:
		kind = recovery.PathKindFile
	case mutationfs.EntryKindDirectory:
		kind = recovery.PathKindDirectory
	default:
		return recoveryWholePathState{}, nil, fmt.Errorf(
			"rollback source %q has unsupported kind %q",
			destination.logical,
			identity.Kind(),
		)
	}
	maximumWork, admitted := maximumKinds[kind]
	if !admitted {
		return recoveryWholePathState{}, nil, fmt.Errorf(
			"rollback source %q changed to unreserved kind %q",
			destination.logical,
			kind,
		)
	}
	if err := admitRecoveryObservation(
		authority.generalExecutionWorkBudget,
		kind,
		maximumWork,
	); err != nil {
		return recoveryWholePathState{}, nil, err
	}
	switch identity.Kind() {
	case mutationfs.EntryKindFile:
		content, mode, captured, err := authority.filesystem.ReadRootedRegularFileUpTo(
			ctx,
			capability,
			min(maximumFileBytes, max(int64(1), maximumWork.Bytes())),
		)
		if err != nil {
			return recoveryWholePathState{}, nil, err
		}
		if !identity.Equal(captured) {
			return recoveryWholePathState{}, nil, fmt.Errorf(
				"rollback source %q changed while observing",
				destination.logical,
			)
		}
		return recoveryWholePathState{
			existed: true, kind: recovery.PathKindFile,
			contentHash: string(artifact.HashFileContentWithExecutable(content, mode.Perm()&0o111 != 0)),
			fileMode:    mode.Perm(),
		}, captured, nil
	case mutationfs.EntryKindDirectory:
		sink := newManagedPathHashSink(ctx)
		limits, err := recoveryTraversalLimits(maximumWork)
		if err != nil {
			return recoveryWholePathState{}, nil, err
		}
		captured, err := authority.filesystem.SnapshotRootedDirectory(
			ctx,
			capability,
			limits,
			sink,
		)
		if err != nil {
			return recoveryWholePathState{}, nil, err
		}
		if !identity.Equal(captured) {
			return recoveryWholePathState{}, nil, fmt.Errorf(
				"rollback source %q changed while observing",
				destination.logical,
			)
		}
		hash, err := sink.sum()
		if err != nil {
			return recoveryWholePathState{}, nil, err
		}
		return recoveryWholePathState{
			existed: true, kind: recovery.PathKindDirectory, contentHash: string(hash),
		}, captured, nil
	}
	return recoveryWholePathState{}, nil, fmt.Errorf("rollback source %q has no admitted state", destination.logical)
}

func validateStagedRecoveryDestination(
	ctx context.Context,
	authority *mutationAuthority,
	entry hostRollbackEntry,
) error {
	if !entry.existed {
		capability, err := authority.acquire(entry.destination)
		if err != nil {
			return err
		}
		_, observeErr := authority.filesystem.CaptureRootedEntryIdentity(ctx, capability)
		closeErr := capability.Close()
		if errors.Is(observeErr, os.ErrNotExist) {
			return closeErr
		}
		if observeErr != nil {
			return errors.Join(observeErr, closeErr)
		}
		return errors.Join(
			fmt.Errorf("absent recovery destination %q gained entry identity", entry.destination.logical),
			closeErr,
		)
	}
	current, identity, err := observeRecoveryWholePathState(
		ctx,
		authority,
		entry.destination,
		entry.maximumFileBytes,
		map[string]recovery.ArtifactWork{entry.kind: entry.stagedWork},
	)
	if err != nil {
		return fmt.Errorf(
			"recovery destination %q changed after rollback staging: %w",
			entry.destination.logical,
			err,
		)
	}
	if !current.equal(entry.stagedState) {
		return fmt.Errorf(
			"recovery destination %q changed after rollback staging",
			entry.destination.logical,
		)
	}
	if entry.identity == nil || identity == nil || !entry.identity.Equal(identity) {
		return fmt.Errorf(
			"recovery destination %q changed entry identity after rollback staging",
			entry.destination.logical,
		)
	}
	return nil
}

func recoveryStageMaximumFileBytes(
	actions []rollbackStageAction,
	codecs aggregate.CodecCatalog,
) (int64, error) {
	if len(actions) == 0 {
		return 0, fmt.Errorf("recovery stage requires at least one action")
	}
	contentPathMode := actions[0].action.ContentPath != ""
	if !contentPathMode {
		for _, action := range actions {
			if action.action.ContentPath != "" || action.action.AggregateContract != nil {
				return 0, fmt.Errorf("rollback stage mixes whole-path and aggregate recovery actions")
			}
		}
		return recovery.MaximumRecoveryBackupFileBytes, nil
	}

	contracts := make([]aggregate.ProjectionContract, 0, len(actions))
	for _, action := range actions {
		if action.action.ContentPath == "" || action.action.AggregateContract == nil {
			return 0, fmt.Errorf("rollback stage mixes aggregate and whole-path recovery actions")
		}
		if err := action.action.validateAggregateCorrelation(); err != nil {
			return 0, err
		}
		contracts = append(contracts, action.action.AggregateContract.Clone())
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		return 0, fmt.Errorf("rollback aggregate selection: %w", err)
	}
	codec, ok := codecs.Lookup(selection.CodecContractID())
	if !ok {
		return 0, fmt.Errorf("aggregate recovery codec %q is not admitted", selection.CodecContractID())
	}
	return codec.MaximumDocumentBytes(), nil
}

type rollbackTreeBackupSink struct {
	ctx            context.Context
	root           string
	hashBuilder    *artifact.DirectoryHashBuilder
	hash           artifact.ContentHash
	directoryModes []rollbackTreeDirectoryMode
	entries        int
	bytes          int64
	initialized    bool
	finalized      bool
}

type rollbackTreeDirectoryMode struct {
	path string
	mode fs.FileMode
}

func newRollbackTreeBackupSink(
	ctx context.Context,
	root string,
) *rollbackTreeBackupSink {
	return &rollbackTreeBackupSink{
		ctx:         ctx,
		root:        root,
		hashBuilder: artifact.NewDirectoryHashBuilder(),
	}
}

func (sink *rollbackTreeBackupSink) VisitRoot(mode fs.FileMode) error {
	if sink == nil || sink.root == "" || sink.initialized {
		return fmt.Errorf("rollback tree backup sink is not ready for a root")
	}
	if err := os.Mkdir(sink.root, 0o700); err != nil {
		return err
	}
	sink.directoryModes = append(sink.directoryModes, rollbackTreeDirectoryMode{
		path: sink.root,
		mode: mode.Perm(),
	})
	sink.initialized = true
	return nil
}

func (sink *rollbackTreeBackupSink) VisitDirectory(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
) error {
	if !sink.initialized || sink.finalized {
		return fmt.Errorf("rollback tree backup root is not initialized")
	}
	backupPath := filepath.Join(sink.root, filepath.FromSlash(path.Path()))
	if err := os.Mkdir(backupPath, 0o700); err != nil {
		return err
	}
	if err := sink.hashBuilder.AddDirectory(path.Path()); err != nil {
		return err
	}
	sink.entries++
	sink.directoryModes = append(sink.directoryModes, rollbackTreeDirectoryMode{
		path: backupPath,
		mode: mode.Perm(),
	})
	return nil
}

func (sink *rollbackTreeBackupSink) VisitRegularFile(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
	size int64,
	content io.Reader,
) error {
	if !sink.initialized || sink.finalized {
		return fmt.Errorf("rollback tree backup root is not initialized")
	}
	if size < 0 {
		return fmt.Errorf("rollback tree file size must not be negative")
	}
	sink.entries++
	sink.bytes += size
	backupPath := filepath.Join(sink.root, filepath.FromSlash(path.Path()))
	file, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	copyErr := sink.hashBuilder.AddFile(
		sink.ctx,
		path.Path(),
		mode.Perm()&0o111 != 0,
		size,
		io.TeeReader(content, file),
	)
	if copyErr == nil {
		copyErr = file.Chmod(mode.Perm())
	}
	return errors.Join(copyErr, file.Close())
}

func (sink *rollbackTreeBackupSink) finalize() error {
	if sink == nil || !sink.initialized || sink.finalized {
		return fmt.Errorf("rollback tree backup is not initialized")
	}
	for index := len(sink.directoryModes) - 1; index >= 0; index-- {
		directory := sink.directoryModes[index]
		if err := os.Chmod(directory.path, directory.mode.Perm()); err != nil {
			return err
		}
	}
	contentHash, err := sink.hashBuilder.Sum()
	if err != nil {
		return err
	}
	sink.hash = contentHash
	sink.finalized = true
	return nil
}

func (sink *rollbackTreeBackupSink) contentHash() artifact.ContentHash {
	if sink == nil || !sink.finalized {
		return ""
	}
	return sink.hash
}

func (sink *rollbackTreeBackupSink) artifactWork() (recovery.ArtifactWork, error) {
	if sink == nil || !sink.finalized {
		return recovery.ArtifactWork{}, fmt.Errorf("rollback tree backup is not finalized")
	}
	return recovery.NewArtifactWork(sink.entries, sink.bytes)
}

func recoveryStagingTraversalLimits(
	budget *recovery.PhysicalWorkBudget,
) (mutationfs.TreeTraversalLimits, error) {
	if budget == nil {
		return mutationfs.TreeTraversalLimits{}, fmt.Errorf("recovery staging budget is required")
	}
	return mutationfs.NewTreeTraversalLimits(
		min(recovery.MaximumArtifactTreeEntries, budget.RemainingEntries()),
		recovery.MaximumArtifactTreeDepth,
		min(recovery.MaximumArtifactTreeBytes, budget.RemainingBytes()),
	)
}
