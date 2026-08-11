package execute

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/filesystem/artifactstage"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/supply/artifact"
)

// recoveryBackupAuthority identifies one content-addressed backup beneath the
// retained active journal directory. It grants no read capability by itself.
type recoveryBackupAuthority struct {
	root        *rootedpath.CapturedRoot
	destination rootedpath.Destination
	identity    artifact.ExactIdentity
	work        recovery.ArtifactWork
}

// recoveryBackup is executable backup authority bound to the operation's
// rooted reader and pre-reserved work budget.
type recoveryBackup struct {
	authority  recoveryBackupAuthority
	filesystem mutationfs.RootedReader
	budget     *recovery.PhysicalWorkBudget
}

func newRecoveryBackupAuthority(
	root *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
	reference string,
	kind string,
	contentHash string,
	work recovery.ArtifactWork,
) (recoveryBackupAuthority, error) {
	if root == nil {
		return recoveryBackupAuthority{}, fmt.Errorf("recovery backup root authority is required")
	}
	if err := destination.Validate(); err != nil {
		return recoveryBackupAuthority{}, fmt.Errorf("recovery backup destination: %w", err)
	}
	identity, err := artifact.NewExactIdentity(
		artifact.SourceID("recovery:backup"),
		artifact.ResolvedRef(reference),
		artifact.ArtifactKind(kind),
		artifact.ContentHash(contentHash),
	)
	if err != nil {
		return recoveryBackupAuthority{}, err
	}
	return recoveryBackupAuthority{
		root: root, destination: destination, identity: identity, work: work,
	}, nil
}

func (authority recoveryBackupAuthority) equal(other recoveryBackupAuthority) bool {
	return authority.root == other.root && authority.destination.Equal(other.destination) &&
		authority.identity.Kind() == other.identity.Kind() &&
		authority.identity.ResolvedRef() == other.identity.ResolvedRef() &&
		authority.identity.ContentHash() == other.identity.ContentHash() &&
		authority.work.Equal(other.work)
}

func (authority recoveryBackupAuthority) bind(
	filesystem mutationfs.RootedReader,
	budget *recovery.PhysicalWorkBudget,
) (recoveryBackup, error) {
	if filesystem == nil || budget == nil {
		return recoveryBackup{}, fmt.Errorf("recovery backup execution authority is unavailable")
	}
	return recoveryBackup{authority: authority, filesystem: filesystem, budget: budget}, nil
}

func (backup recoveryBackup) acquire(
	budget *recovery.PhysicalWorkBudget,
) (rootedpath.CommitCapability, error) {
	if budget == nil {
		return nil, fmt.Errorf("recovery backup execution budget is required")
	}
	return backup.authority.root.AcquireBounded(
		backup.authority.destination,
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
}

func (backup recoveryBackup) readFile(ctx context.Context) ([]byte, error) {
	if backup.filesystem == nil || backup.budget == nil {
		return nil, fmt.Errorf("recovery backup execution authority is unavailable")
	}
	if backup.authority.identity.Kind() != artifact.ArtifactKindFile || backup.authority.work.Entries() != 0 {
		return nil, fmt.Errorf("recovery backup is not a bounded regular file")
	}
	capability, err := backup.acquire(backup.budget)
	if err != nil {
		return nil, err
	}
	content, mode, _, readErr := backup.filesystem.ReadRootedRegularFileUpTo(
		ctx,
		capability,
		max(int64(1), backup.authority.work.Bytes()),
	)
	closeErr := capability.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	actual, err := recovery.NewArtifactWork(0, int64(len(content)))
	if err != nil {
		return nil, err
	}
	if err := backup.budget.AdmitTree(actual); err != nil {
		return nil, err
	}
	if !actual.Equal(backup.authority.work) {
		return nil, fmt.Errorf(
			"recovery file backup work changed: got %d bytes, want %d",
			actual.Bytes(),
			backup.authority.work.Bytes(),
		)
	}
	hash := artifact.HashFileContentWithExecutable(content, mode.Perm()&0o111 != 0)
	if hash != backup.authority.identity.ContentHash() {
		return nil, fmt.Errorf(
			"recovery file backup hash %q does not match expected hash %q",
			hash,
			backup.authority.identity.ContentHash(),
		)
	}
	return append([]byte(nil), content...), nil
}

func (backup recoveryBackup) copyDirectory(
	ctx context.Context,
	writer mutationfs.RootedTreeWriter,
) error {
	if backup.filesystem == nil || backup.budget == nil {
		return fmt.Errorf("recovery backup execution authority is unavailable")
	}
	if backup.authority.identity.Kind() != artifact.ArtifactKindDirectory {
		return fmt.Errorf("recovery backup is not a directory")
	}
	limits, err := mutationfs.NewTreeTraversalLimits(
		backup.authority.work.Entries(),
		recovery.MaximumArtifactTreeDepth,
		backup.authority.work.Bytes(),
	)
	if err != nil {
		return err
	}
	capability, err := backup.acquire(backup.budget)
	if err != nil {
		return err
	}
	sink, err := newRecoveryDirectoryRestoreSink(ctx, writer)
	if err != nil {
		return errors.Join(err, capability.Close())
	}
	_, snapshotErr := backup.filesystem.SnapshotRootedDirectory(ctx, capability, limits, sink)
	closeErr := capability.Close()
	if snapshotErr != nil || closeErr != nil {
		return errors.Join(snapshotErr, closeErr)
	}
	actual, err := sink.work()
	if err != nil {
		return err
	}
	if err := backup.budget.AdmitTree(actual); err != nil {
		return err
	}
	if !actual.Equal(backup.authority.work) {
		return fmt.Errorf(
			"recovery directory backup work changed: got entries=%d bytes=%d, want entries=%d bytes=%d",
			actual.Entries(),
			actual.Bytes(),
			backup.authority.work.Entries(),
			backup.authority.work.Bytes(),
		)
	}
	hash, err := sink.hash()
	if err != nil {
		return err
	}
	if hash != backup.authority.identity.ContentHash() {
		return fmt.Errorf(
			"recovery directory backup hash %q does not match expected hash %q",
			hash,
			backup.authority.identity.ContentHash(),
		)
	}
	return nil
}

type recoveryBackupPathReservation struct {
	budget *recovery.PhysicalWorkBudget
}

func (reservation recoveryBackupPathReservation) AdmitPathComponents(count int) error {
	return reservation.budget.ReserveBackupPathComponents(count)
}

func (authority *mutationAuthority) prepareRecoveryBackups(
	ctx context.Context,
	operationDir string,
	actions []recoveryHostAction,
) error {
	if authority == nil || authority.filesystem == nil || authority.physicalWorkBudget == nil {
		return fmt.Errorf("recovery backup authority is unavailable")
	}
	if authority.recoveryBackupExecution != nil {
		return nil
	}
	restoreCount := 0
	for _, action := range actions {
		if action.Kind == recovery.ActionKindRestoreWrite {
			restoreCount++
		}
	}
	if restoreCount == 0 {
		execution, err := authority.physicalWorkBudget.BeginReservedBackupExecution()
		if err != nil {
			return err
		}
		authority.recoveryBackups = map[string]recoveryBackup{}
		authority.recoveryBackupExecution = execution
		return nil
	}

	root, err := rootedpath.CaptureRootNoFollowBounded(
		operationDir,
		recovery.MaximumPhysicalPathDepth,
		authority.physicalWorkBudget,
	)
	if err != nil {
		return fmt.Errorf("capture recovery backup root: %w", err)
	}
	retained := false
	defer func() {
		if !retained {
			_ = root.Close()
		}
	}()
	if err := journal.ValidateActiveJournalRoot(
		ctx,
		authority.filesystem,
		root,
		authority.physicalWorkBudget,
		authority.journalBasis.activeAuthority,
	); err != nil {
		return fmt.Errorf("validate recovery backup root: %w", err)
	}
	rootAuthority, err := root.AuthorityBounded(authority.physicalWorkBudget)
	if err != nil {
		return err
	}
	backupAuthorities := make(map[string]recoveryBackupAuthority, restoreCount)
	for index, action := range actions {
		if action.Kind != recovery.ActionKindRestoreWrite {
			continue
		}
		relative, err := rootedpath.NewRelativeDestination(action.BackupPath)
		if err != nil {
			return fmt.Errorf("recovery action[%d] backup path: %w", index, err)
		}
		destination, err := rootAuthority.Bind(relative)
		if err != nil {
			return fmt.Errorf("bind recovery action[%d] backup: %w", index, err)
		}
		backupAuthority, err := newRecoveryBackupAuthority(
			root,
			destination,
			action.BackupPath,
			action.BackupKind,
			action.BackupHash,
			action.BackupWork,
		)
		if err != nil {
			return fmt.Errorf("recovery action[%d] backup authority: %w", index, err)
		}
		if existing, present := backupAuthorities[action.BackupPath]; present {
			if !existing.equal(backupAuthority) {
				return fmt.Errorf("recovery backup %q has conflicting action authority", action.BackupPath)
			}
		} else {
			backupAuthorities[action.BackupPath] = backupAuthority
		}

		reservation := recoveryBackupPathReservation{budget: authority.physicalWorkBudget}
		if err := root.ReserveDestinationAccess(
			destination,
			recovery.MaximumPhysicalPathDepth,
			reservation,
		); err != nil {
			return fmt.Errorf("reserve recovery action[%d] backup path work: %w", index, err)
		}
		switch action.BackupKind {
		case recovery.PathKindFile:
			if err := authority.physicalWorkBudget.ReserveBackupFileExecution(action.BackupWork); err != nil {
				return fmt.Errorf("reserve recovery action[%d] file backup work: %w", index, err)
			}
		case recovery.PathKindDirectory:
			if err := authority.physicalWorkBudget.ReserveBackupDirectoryExecution(action.BackupWork); err != nil {
				return fmt.Errorf("reserve recovery action[%d] directory backup work: %w", index, err)
			}
		default:
			return fmt.Errorf("recovery action[%d] backup kind %q is unsupported", index, action.BackupKind)
		}
	}
	execution, err := authority.physicalWorkBudget.BeginReservedBackupExecution()
	if err != nil {
		return err
	}
	backups := make(map[string]recoveryBackup, len(backupAuthorities))
	for path, backupAuthority := range backupAuthorities {
		backup, err := backupAuthority.bind(authority.filesystem, execution)
		if err != nil {
			return err
		}
		backups[path] = backup
	}
	authority.retainedRoots = append(authority.retainedRoots, root)
	retained = true
	authority.recoveryBackups = backups
	authority.recoveryBackupExecution = execution
	return nil
}

func (authority *mutationAuthority) recoveryBackupForAction(
	action recoveryHostAction,
) (recoveryBackup, error) {
	if authority == nil || authority.recoveryBackupExecution == nil {
		return recoveryBackup{}, fmt.Errorf("recovery backup authority is not prepared")
	}
	backup, present := authority.recoveryBackups[action.BackupPath]
	if !present {
		return recoveryBackup{}, fmt.Errorf("recovery backup %q is not bound", action.BackupPath)
	}
	expected, err := newRecoveryBackupAuthority(
		backup.authority.root,
		backup.authority.destination,
		action.BackupPath,
		action.BackupKind,
		action.BackupHash,
		action.BackupWork,
	)
	if err != nil {
		return recoveryBackup{}, err
	}
	if !backup.authority.equal(expected) {
		return recoveryBackup{}, fmt.Errorf("recovery backup %q action authority changed", action.BackupPath)
	}
	return backup, nil
}

type recoveryDirectoryRestoreSink struct {
	ctx     context.Context
	stage   artifactstage.Sink
	hasher  *artifact.DirectoryHashBuilder
	entries int
	bytes   int64
}

func newRecoveryDirectoryRestoreSink(
	ctx context.Context,
	writer mutationfs.RootedTreeWriter,
) (*recoveryDirectoryRestoreSink, error) {
	stage, err := artifactstage.New(writer)
	if err != nil {
		return nil, err
	}
	return &recoveryDirectoryRestoreSink{
		ctx: ctx, stage: stage, hasher: artifact.NewDirectoryHashBuilder(),
	}, nil
}

func (sink *recoveryDirectoryRestoreSink) VisitRoot(mode fs.FileMode) error {
	return sink.stage.BeginDirectory(".", mode)
}

func (sink *recoveryDirectoryRestoreSink) VisitDirectory(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
) error {
	if err := sink.hasher.AddDirectory(path.Path()); err != nil {
		return err
	}
	if err := sink.stage.BeginDirectory(path.Path(), mode); err != nil {
		return err
	}
	sink.entries++
	return nil
}

func (sink *recoveryDirectoryRestoreSink) VisitRegularFile(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
	size int64,
	content io.Reader,
) error {
	target, err := sink.stage.OpenFile(path.Path(), mode, size)
	if err != nil {
		return err
	}
	hashErr := sink.hasher.AddFile(
		sink.ctx,
		path.Path(),
		mode.Perm()&0o111 != 0,
		size,
		io.TeeReader(content, target),
	)
	closeErr := target.Close()
	if hashErr != nil || closeErr != nil {
		return errors.Join(hashErr, closeErr)
	}
	sink.entries++
	sink.bytes += size
	return nil
}

func (sink *recoveryDirectoryRestoreSink) work() (recovery.ArtifactWork, error) {
	return recovery.NewArtifactWork(sink.entries, sink.bytes)
}

func (sink *recoveryDirectoryRestoreSink) hash() (artifact.ContentHash, error) {
	return sink.hasher.Sum()
}
