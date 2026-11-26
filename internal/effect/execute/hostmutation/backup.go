package hostmutation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	"github.com/isty2e/daem/internal/effect/journal/recovery"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/filesystem/artifactstage"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/supply/artifact"
)

// recoveryBackupAuthority identifies one content-addressed backup beneath the
// retained active journal directory. It grants no read capability by itself.
type recoveryBackupAuthority struct {
	root        *rootedpath.CapturedRoot
	destination rootedpath.Destination
	kind        artifact.ArtifactKind
	ref         artifact.ResolvedRef
	hash        artifact.ContentHash
	work        workmeasure.ArtifactWork
}

// Backup is executable backup authority bound to the operation's
// rooted reader and pre-reserved work budget.
type Backup struct {
	authority  recoveryBackupAuthority
	filesystem mutationfs.RootedReader
	budget     *physicalwork.Budget
}

type BackupRequest struct {
	Index      int
	BackupPath string
	BackupKind string
	BackupHash string
	BackupWork workmeasure.ArtifactWork
}

type Backups struct {
	operation      *Operation
	planningBudget *physicalwork.Budget
	execution      *physicalwork.Budget
	readers        map[string]Backup
}

func NewBackups(operation *Operation, planningBudget *physicalwork.Budget) *Backups {
	return &Backups{operation: operation, planningBudget: planningBudget}
}

func newRecoveryBackupAuthority(
	root *rootedpath.CapturedRoot,
	destination rootedpath.Destination,
	reference string,
	kind string,
	contentHash string,
	work workmeasure.ArtifactWork,
) (recoveryBackupAuthority, error) {
	if root == nil {
		return recoveryBackupAuthority{}, fmt.Errorf("recovery backup root authority is required")
	}
	if err := destination.Validate(); err != nil {
		return recoveryBackupAuthority{}, fmt.Errorf("recovery backup destination: %w", err)
	}
	if err := errors.Join(artifact.ResolvedRef(reference).Validate(), artifact.ArtifactKind(kind).Validate(), artifact.ContentHash(contentHash).ValidateManaged()); err != nil {
		return recoveryBackupAuthority{}, err
	}
	return recoveryBackupAuthority{
		root: root, destination: destination, kind: artifact.ArtifactKind(kind), ref: artifact.ResolvedRef(reference), hash: artifact.ContentHash(contentHash), work: work,
	}, nil
}

func (authority recoveryBackupAuthority) equal(other recoveryBackupAuthority) bool {
	return authority.root == other.root && authority.destination.Equal(other.destination) &&
		authority.kind == other.kind && authority.ref == other.ref && authority.hash == other.hash &&
		authority.work.Equal(other.work)
}

func (authority recoveryBackupAuthority) bind(
	filesystem mutationfs.RootedReader,
	budget *physicalwork.Budget,
) (Backup, error) {
	if filesystem == nil || budget == nil {
		return Backup{}, fmt.Errorf("recovery backup execution authority is unavailable")
	}
	return Backup{authority: authority, filesystem: filesystem, budget: budget}, nil
}

func (backup Backup) acquire(
	budget *physicalwork.Budget,
) (rootedpath.CommitCapability, error) {
	if budget == nil {
		return nil, fmt.Errorf("recovery backup execution budget is required")
	}
	return backup.authority.root.AcquireBounded(
		backup.authority.destination,
		physicalwork.MaximumPhysicalPathDepth,
		budget,
	)
}

func (backup Backup) ReadFile(ctx context.Context) ([]byte, error) {
	if backup.filesystem == nil || backup.budget == nil {
		return nil, fmt.Errorf("recovery backup execution authority is unavailable")
	}
	if backup.authority.kind != artifact.ArtifactKindFile || backup.authority.work.Entries() != 0 {
		return nil, fmt.Errorf("recovery backup is not a bounded regular file")
	}
	capability, err := backup.acquire(backup.budget)
	if err != nil {
		return nil, err
	}
	content, _, _, readErr := backup.filesystem.ReadRootedRegularFileUpTo(
		ctx,
		capability,
		max(int64(1), backup.authority.work.Bytes()),
	)
	closeErr := capability.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	actual, err := workmeasure.NewArtifactWork(0, int64(len(content)))
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
	if !backup.authority.hash.MatchesFile(content) {
		return nil, fmt.Errorf("recovery file backup does not match expected hash %q", backup.authority.hash)
	}
	return append([]byte(nil), content...), nil
}

func (backup Backup) restoreDirectory(ctx context.Context, writer mutationfs.RootedTreeWriter, packed recovery.TreeModes) (artifact.ContentHash, error) {
	if backup.filesystem == nil || backup.budget == nil {
		return "", fmt.Errorf("recovery backup execution authority is unavailable")
	}
	if backup.authority.kind != artifact.ArtifactKindDirectory {
		return "", fmt.Errorf("recovery backup is not a directory")
	}
	limits, err := mutationfs.NewTreeTraversalLimits(
		backup.authority.work.Entries(),
		recovery.MaximumArtifactTreeDepth,
		backup.authority.work.Bytes(),
	)
	if err != nil {
		return "", err
	}
	capability, err := backup.acquire(backup.budget)
	if err != nil {
		return "", err
	}
	sink, err := newRecoveryDirectoryRestoreSink(ctx, writer)
	if err != nil {
		return "", errors.Join(err, capability.Close())
	}
	if packed != "" {
		sink.beforeModes, err = packed.Unpack()
		if err != nil || len(sink.beforeModes) != backup.authority.work.Entries()+1 {
			return "", errors.Join(fmt.Errorf("invalid permission list for backup tree"), err, capability.Close())
		}
	}
	_, snapshotErr := backup.filesystem.SnapshotRootedDirectory(ctx, capability, limits, sink)
	closeErr := capability.Close()
	if snapshotErr != nil || closeErr != nil {
		return "", errors.Join(snapshotErr, closeErr)
	}
	actual, err := sink.work()
	if err != nil {
		return "", err
	}
	if err := backup.budget.AdmitTree(actual); err != nil {
		return "", err
	}
	if !actual.Equal(backup.authority.work) {
		return "", fmt.Errorf(
			"recovery directory backup work changed: got entries=%d bytes=%d, want entries=%d bytes=%d",
			actual.Entries(),
			actual.Bytes(),
			backup.authority.work.Entries(),
			backup.authority.work.Bytes(),
		)
	}
	hashes, err := sink.hasher.Hashes()
	if err != nil {
		return "", err
	}
	if !hashes.Matches(backup.authority.hash) {
		return "", fmt.Errorf(
			"recovery directory backup hash %q does not match expected hash %q",
			hashes.Installed,
			backup.authority.hash,
		)
	}
	return hashes.Installed, nil
}

type recoveryBackupPathReservation struct {
	budget *physicalwork.Budget
}

func (reservation recoveryBackupPathReservation) AdmitPathComponents(count int) error {
	return reservation.budget.ReserveBackupPathComponents(count)
}

func (set *Backups) Prepare(
	ctx context.Context,
	operationDir string,
	actions []BackupRequest,
	active journalinventory.ActiveJournalAuthority,
) error {
	if set == nil || set.operation == nil || set.operation.filesystem == nil || set.planningBudget == nil {
		return fmt.Errorf("recovery backup authority is unavailable")
	}
	if set.execution != nil {
		return nil
	}
	restoreCount := len(actions)
	if restoreCount == 0 {
		execution, err := set.planningBudget.BeginReservedBackupExecution()
		if err != nil {
			return err
		}
		set.readers = map[string]Backup{}
		set.execution = execution
		return nil
	}

	root, err := rootedpath.CaptureCanonicalRootNoFollowBounded(
		operationDir,
		physicalwork.MaximumPhysicalPathDepth,
		set.planningBudget,
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
	if err := journalinventory.ValidateActiveJournalRoot(
		ctx,
		set.operation.filesystem,
		root,
		set.planningBudget,
		active,
	); err != nil {
		return fmt.Errorf("validate recovery backup root: %w", err)
	}
	rootAuthority, err := root.AuthorityBounded(set.planningBudget)
	if err != nil {
		return err
	}
	backupAuthorities := make(map[string]recoveryBackupAuthority, restoreCount)
	for _, action := range actions {
		index := action.Index
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

		reservation := recoveryBackupPathReservation{budget: set.planningBudget}
		if err := root.ReserveDestinationAccess(
			destination,
			physicalwork.MaximumPhysicalPathDepth,
			reservation,
		); err != nil {
			return fmt.Errorf("reserve recovery action[%d] backup path work: %w", index, err)
		}
		switch action.BackupKind {
		case recovery.PathKindFile:
			if err := set.planningBudget.ReserveBackupFileExecution(action.BackupWork); err != nil {
				return fmt.Errorf("reserve recovery action[%d] file backup work: %w", index, err)
			}
		case recovery.PathKindDirectory:
			if err := set.planningBudget.ReserveBackupDirectoryExecution(action.BackupWork); err != nil {
				return fmt.Errorf("reserve recovery action[%d] directory backup work: %w", index, err)
			}
		default:
			return fmt.Errorf("recovery action[%d] backup kind %q is unsupported", index, action.BackupKind)
		}
	}
	execution, err := set.planningBudget.BeginReservedBackupExecution()
	if err != nil {
		return err
	}
	backups := make(map[string]Backup, len(backupAuthorities))
	for path, backupAuthority := range backupAuthorities {
		backup, err := backupAuthority.bind(set.operation.filesystem, execution)
		if err != nil {
			return err
		}
		backups[path] = backup
	}
	if err := set.operation.destinations.RetainArtifactRoot(root); err != nil {
		return err
	}
	retained = true
	set.readers = backups
	set.execution = execution
	return nil
}

func (set *Backups) For(
	action BackupRequest,
) (Backup, error) {
	if set == nil || set.execution == nil {
		return Backup{}, fmt.Errorf("recovery backup authority is not prepared")
	}
	backup, present := set.readers[action.BackupPath]
	if !present {
		return Backup{}, fmt.Errorf("recovery backup %q is not bound", action.BackupPath)
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
		return Backup{}, err
	}
	if !backup.authority.equal(expected) {
		return Backup{}, fmt.Errorf("recovery backup %q action authority changed", action.BackupPath)
	}
	return backup, nil
}

type recoveryDirectoryRestoreSink struct {
	beforeModes []fs.FileMode
	ctx         context.Context
	stage       artifactstage.Sink
	hasher      *artifact.DirectoryHashBuilder
	entries     int
	bytes       int64
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
		ctx: ctx, stage: stage, hasher: artifact.NewDirectoryEvidenceBuilder(),
	}, nil
}

func (sink *recoveryDirectoryRestoreSink) VisitRoot(mode fs.FileMode) error {
	return sink.stage.BeginDirectory(".", sink.restoreMode(mode))
}

func (sink *recoveryDirectoryRestoreSink) restoreMode(backed fs.FileMode) fs.FileMode {
	mode := backed.Perm()
	if len(sink.beforeModes) != 0 {
		mode, sink.beforeModes = sink.beforeModes[0], sink.beforeModes[1:]
	}
	return mode
}

func (sink *recoveryDirectoryRestoreSink) VisitDirectory(
	path mutationfs.TreeRelativePath,
	mode fs.FileMode,
) error {
	if err := sink.hasher.AddDirectory(path.Path()); err != nil {
		return err
	}
	if err := sink.stage.BeginDirectory(path.Path(), sink.restoreMode(mode)); err != nil {
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
	target, err := sink.stage.OpenFile(path.Path(), sink.restoreMode(mode), size)
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

func (sink *recoveryDirectoryRestoreSink) work() (workmeasure.ArtifactWork, error) {
	return workmeasure.NewArtifactWork(sink.entries, sink.bytes)
}

func (sink *recoveryDirectoryRestoreSink) hash() (artifact.ContentHash, error) {
	return sink.hasher.Sum()
}
