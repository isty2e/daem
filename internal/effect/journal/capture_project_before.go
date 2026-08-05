package journal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func validateAbsentProjectRecoveryPath(
	ctx context.Context,
	filesystem mutationfs.RootedReader,
	action pathMutation,
	manifestAuthority *manifestAuthoritySession,
) error {
	capability, err := manifestAuthority.acquire(action.Destination)
	if err != nil {
		return err
	}
	_, inspectErr := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	closeErr := capability.Close()
	if errors.Is(inspectErr, fs.ErrNotExist) {
		inspectErr = nil
	} else if inspectErr == nil {
		inspectErr = fmt.Errorf(
			"project destination %q appeared after the live observation",
			action.Destination,
		)
	}
	return errors.Join(inspectErr, closeErr)
}

func captureProjectContentPathRecoveryBeforePath(
	ctx context.Context,
	operationDir string,
	backupIndex int,
	action pathMutation,
	filesystem mutationfs.Reader,
	manifestAuthority *manifestAuthoritySession,
	contentPathBaselines *recoveryContentPathBaselineCache,
) (recovery.BeforePathState, int, error) {
	if action.Kind == pathMutationCreate {
		if action.LiveExists {
			return recovery.BeforePathState{}, backupIndex, fmt.Errorf(
				"live observation for %q content path %q exists before create action",
				action.Destination,
				action.ContentPath,
			)
		}
		before, err := captureAbsentProjectContentPathRecoveryBefore(
			ctx,
			action,
			filesystem,
			manifestAuthority,
			contentPathBaselines,
		)
		return before, backupIndex, err
	}
	if !action.LiveExists {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf(
			"live observation for %q content path %q is missing before %s action",
			action.Destination,
			action.ContentPath,
			action.Kind,
		)
	}
	if action.LiveHash == "" {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf(
			"live observation %q content path %q content hash is required",
			action.Destination,
			action.ContentPath,
		)
	}
	if !action.LivePathExists {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf(
			"live observation path %q is missing before %s action",
			action.Destination,
			action.Kind,
		)
	}

	baseline, err := contentPathBaselines.capture(ctx, action, nil, manifestAuthority, nil)
	if err != nil {
		return recovery.BeforePathState{}, backupIndex, err
	}
	if err := validateCapturedProjectPathHash(action, baseline.pathContentHash); err != nil {
		return recovery.BeforePathState{}, backupIndex, err
	}
	projection, present, err := baseline.projection(action.ContentPath)
	if err != nil {
		return recovery.BeforePathState{}, backupIndex, err
	}
	if !present {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf(
			"live observation for %q content path %q reported existing projection but extraction found none",
			action.Destination,
			action.ContentPath,
		)
	}
	if hash := artifact.HashFileContent(projection); hash != action.LiveHash {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf(
			"live observation %q content path %q hash %q does not match extracted projection hash %q",
			action.Destination,
			action.ContentPath,
			action.LiveHash,
			hash,
		)
	}

	backupIndex++
	backupPath := filepath.ToSlash(filepath.Join("files", fmt.Sprintf("%06d", backupIndex)))
	absoluteBackupPath := filepath.Join(operationDir, filepath.FromSlash(backupPath))
	if err := writeRecoveryBackupFile(absoluteBackupPath, projection, 0o600); err != nil {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf(
			"copy recovery projection backup for %q content path %q: %w",
			action.Destination,
			action.ContentPath,
			err,
		)
	}

	return recovery.BeforePathState{
		Existed:       true,
		PathExisted:   true,
		ParentExisted: true,
		PathMode:      recovery.NewPermissionMode(baseline.mode),
		Kind:          recovery.PathKindFile,
		ContentHash:   string(action.LiveHash),
		BackupPath:    backupPath,
	}, backupIndex, nil
}

func captureAbsentProjectContentPathRecoveryBefore(
	ctx context.Context,
	action pathMutation,
	filesystem mutationfs.Reader,
	manifestAuthority *manifestAuthoritySession,
	contentPathBaselines *recoveryContentPathBaselineCache,
) (recovery.BeforePathState, error) {
	if !action.LivePathExists {
		if err := validateAbsentProjectRecoveryPath(ctx, filesystem, action, manifestAuthority); err != nil {
			return recovery.BeforePathState{}, err
		}
		return recovery.BeforePathState{Existed: false}, nil
	}
	baseline, err := contentPathBaselines.capture(ctx, action, nil, manifestAuthority, nil)
	if err != nil {
		return recovery.BeforePathState{}, err
	}
	if err := validateCapturedProjectPathHash(action, baseline.pathContentHash); err != nil {
		return recovery.BeforePathState{}, err
	}
	parentExisted, err := baseline.parentExisted(action.ContentPath)
	if err != nil {
		return recovery.BeforePathState{}, err
	}
	return recovery.BeforePathState{
		Existed:       false,
		PathExisted:   true,
		ParentExisted: parentExisted,
		PathMode:      recovery.NewPermissionMode(baseline.mode),
	}, nil
}

func captureProjectExistingRecoveryBeforePath(
	ctx context.Context,
	filesystem mutationfs.RootedReader,
	operationDir string,
	backupIndex int,
	action pathMutation,
	manifestAuthority *manifestAuthoritySession,
) (recovery.BeforePathState, int, error) {
	capability, err := manifestAuthority.acquire(action.Destination)
	if err != nil {
		return recovery.BeforePathState{}, backupIndex, err
	}
	before, nextBackupIndex, captureErr := captureRootedExistingWithCapability(
		ctx,
		filesystem,
		operationDir,
		backupIndex,
		action,
		capability,
	)
	closeErr := capability.Close()
	if captureErr != nil || closeErr != nil {
		return recovery.BeforePathState{}, nextBackupIndex, errors.Join(captureErr, closeErr)
	}
	return before, nextBackupIndex, nil
}

func captureRootedExistingWithCapability(
	ctx context.Context,
	filesystem mutationfs.RootedReader,
	operationDir string,
	backupIndex int,
	action pathMutation,
	capability rootedpath.CommitCapability,
) (recovery.BeforePathState, int, error) {
	identity, err := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	if err != nil {
		return recovery.BeforePathState{}, backupIndex, err
	}
	backupIndex++
	backupPath := filepath.ToSlash(filepath.Join("files", fmt.Sprintf("%06d", backupIndex)))
	absoluteBackupPath := filepath.Join(operationDir, filepath.FromSlash(backupPath))

	before := recovery.BeforePathState{
		Existed:     true,
		ContentHash: string(action.LivePathHash),
		BackupPath:  backupPath,
	}
	switch identity.Kind() {
	case mutationfs.EntryKindFile:
		content, mode, _, err := filesystem.ReadRootedRegularFileUpTo(
			ctx,
			capability,
			MaximumRecoveryBackupFileBytes,
		)
		if err != nil {
			return recovery.BeforePathState{}, backupIndex, err
		}
		capturedHash := artifact.HashFileContentWithExecutable(
			content,
			mode.Perm()&0o111 != 0,
		)

		if capturedHash != action.LivePathHash {
			return recovery.BeforePathState{}, backupIndex, fmt.Errorf(
				"captured rooted path %q hash %q does not match live observation %q",
				action.Destination,
				capturedHash,
				action.LivePathHash,
			)
		}
		if err := writeRecoveryBackupFile(absoluteBackupPath, content, mode); err != nil {
			return recovery.BeforePathState{}, backupIndex, err
		}
		before.PathMode = recovery.NewPermissionMode(mode)
		before.Kind = recovery.PathKindFile
	case mutationfs.EntryKindDirectory:
		sink := newRootedTreeBackupSink(ctx, absoluteBackupPath)
		if _, err := filesystem.SnapshotRootedDirectory(
			ctx,
			capability,
			recoveryTreeTraversalLimits(),
			sink,
		); err != nil {
			return recovery.BeforePathState{}, backupIndex, err
		}
		capturedHash, err := sink.hash()
		if err != nil {
			return recovery.BeforePathState{}, backupIndex, err
		}
		if capturedHash != action.LivePathHash {
			return recovery.BeforePathState{}, backupIndex, fmt.Errorf(
				"captured rooted path %q hash %q does not match live observation %q",
				action.Destination,
				capturedHash,
				action.LivePathHash,
			)
		}
		before.Kind = recovery.PathKindDirectory
	default:
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf(
			"rooted destination %q has unsupported entry kind %q",
			action.Destination,
			identity.Kind(),
		)
	}
	return before, backupIndex, nil
}

func readProjectRecoveryRegularFile(
	ctx context.Context,
	filesystem mutationfs.RootedReader,
	destination output.Destination,
	manifestAuthority *manifestAuthoritySession,
	maximumBytes int64,
) ([]byte, fs.FileMode, error) {
	capability, err := manifestAuthority.acquire(destination)
	if err != nil {
		return nil, 0, err
	}
	content, mode, _, readErr := filesystem.ReadRootedRegularFileUpTo(
		ctx,
		capability,
		maximumBytes,
	)
	closeErr := capability.Close()
	if readErr != nil || closeErr != nil {
		return nil, 0, errors.Join(readErr, closeErr)
	}
	return content, mode, nil
}

func validateCapturedProjectPathHash(
	action pathMutation,
	captured artifact.ContentHash,
) error {
	if action.LivePathHash == "" {
		return fmt.Errorf("live observation for project path %q requires path content hash", action.Destination)
	}
	if captured != action.LivePathHash {
		return fmt.Errorf(
			"captured project path %q hash %q does not match live observation %q",
			action.Destination,
			captured,
			action.LivePathHash,
		)
	}
	return nil
}

func writeRecoveryBackupFile(path string, content []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, bytes.NewReader(content)); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(mode.Perm()); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
