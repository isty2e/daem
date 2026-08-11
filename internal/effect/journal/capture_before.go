package journal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func captureRecoveryBeforePath(
	ctx context.Context,
	operationDir string,
	backupIndex int,
	action pathMutation,
	filesystem mutationfs.Reader,
	resolver func(destination output.Destination) (string, error),
	manifestAuthority *manifestAuthoritySession,
	rootedCapability RootedCapabilityResolver,
	contentPathBaselines *recoveryContentPathBaselineCache,
) (recovery.BeforePathState, int, error) {
	if action.ContentPath != "" {
		if action.Scope == target.ScopeProject {
			return captureProjectContentPathRecoveryBeforePath(
				ctx,
				operationDir,
				backupIndex,
				action,
				filesystem,
				manifestAuthority,
				contentPathBaselines,
			)
		}
		return captureContentPathRecoveryBeforePath(
			ctx,
			operationDir,
			backupIndex,
			action,
			resolver,
			manifestAuthority,
			rootedCapability,
			contentPathBaselines,
		)
	}
	if action.Kind == pathMutationCreate {
		if action.LiveExists {
			return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation for %q exists before create action", action.Destination)
		}
		if !action.LivePathExists {
			if action.Scope == target.ScopeProject {
				if err := validateAbsentProjectRecoveryPath(ctx, filesystem, action, manifestAuthority); err != nil {
					return recovery.BeforePathState{}, backupIndex, err
				}
			} else if capability, present, err := acquireRootedJournalCapability(
				action.Destination,
				resolver,
				rootedCapability,
			); err != nil {
				return recovery.BeforePathState{}, backupIndex, err
			} else if present {
				if _, captureErr := filesystem.CaptureRootedEntryIdentity(ctx, capability); captureErr == nil {
					_ = capability.Close()
					return recovery.BeforePathState{}, backupIndex, fmt.Errorf("destination %q appeared after observation", action.Destination)
				} else if !errors.Is(captureErr, os.ErrNotExist) {
					return recovery.BeforePathState{}, backupIndex, errors.Join(captureErr, capability.Close())
				}
				if closeErr := capability.Close(); closeErr != nil {
					return recovery.BeforePathState{}, backupIndex, closeErr
				}
			}
			return recovery.BeforePathState{Existed: false}, backupIndex, nil
		}
		if action.Scope == target.ScopeProject {
			return captureProjectExistingRecoveryBeforePath(
				ctx,
				filesystem,
				operationDir,
				backupIndex,
				action,
				manifestAuthority,
			)
		}
		if capability, present, err := acquireRootedJournalCapability(
			action.Destination,
			resolver,
			rootedCapability,
		); err != nil {
			return recovery.BeforePathState{}, backupIndex, err
		} else if present {
			return captureAndCloseRootedRecoveryBefore(
				ctx, filesystem, operationDir, backupIndex, action, capability,
			)
		}
		return captureExistingRecoveryBeforePath(
			ctx, filesystem, operationDir, backupIndex, action, resolver,
		)
	}
	if !action.LiveExists {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation for %q is missing before %s action", action.Destination, action.Kind)
	}
	if action.LiveHash == "" {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation %q content hash is required", action.Destination)
	}
	if !action.LivePathExists {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation path %q is missing before %s action", action.Destination, action.Kind)
	}
	if action.LivePathHash == "" {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation %q path content hash is required", action.Destination)
	}

	if action.Scope == target.ScopeProject {
		return captureProjectExistingRecoveryBeforePath(
			ctx,
			filesystem,
			operationDir,
			backupIndex,
			action,
			manifestAuthority,
		)
	}
	if capability, present, err := acquireRootedJournalCapability(
		action.Destination,
		resolver,
		rootedCapability,
	); err != nil {
		return recovery.BeforePathState{}, backupIndex, err
	} else if present {
		return captureAndCloseRootedRecoveryBefore(
			ctx, filesystem, operationDir, backupIndex, action, capability,
		)
	}
	return captureExistingRecoveryBeforePath(
		ctx, filesystem, operationDir, backupIndex, action, resolver,
	)
}

func acquireRootedJournalCapability(
	destination output.Destination,
	resolver func(output.Destination) (string, error),
	acquire RootedCapabilityResolver,
) (rootedpath.CommitCapability, bool, error) {
	if acquire == nil {
		return nil, false, nil
	}
	if resolver == nil {
		return nil, false, fmt.Errorf("recovery destination resolver is required")
	}
	resolvedPath, err := resolver(destination)
	if err != nil {
		return nil, false, fmt.Errorf("resolve destination %q: %w", destination, err)
	}
	capability, present, err := acquireMatchingRootedCapability(destination, resolvedPath, acquire, nil)
	if err != nil {
		return nil, false, err
	}
	if !present {
		return nil, false, fmt.Errorf("destination %q has no retained root authority", destination)
	}
	return capability, true, nil
}

func captureAndCloseRootedRecoveryBefore(
	ctx context.Context,
	filesystem mutationfs.RootedReader,
	operationDir string,
	backupIndex int,
	action pathMutation,
	capability rootedpath.CommitCapability,
) (recovery.BeforePathState, int, error) {
	before, nextBackupIndex, captureErr := captureRootedExistingWithCapability(
		ctx, filesystem, operationDir, backupIndex, action, capability,
	)
	closeErr := capability.Close()
	if captureErr != nil || closeErr != nil {
		return recovery.BeforePathState{}, nextBackupIndex, errors.Join(captureErr, closeErr)
	}
	return before, nextBackupIndex, nil
}

func captureContentPathRecoveryBeforePath(
	ctx context.Context,
	operationDir string,
	backupIndex int,
	action pathMutation,
	resolver func(destination output.Destination) (string, error),
	manifestAuthority *manifestAuthoritySession,
	rootedCapability RootedCapabilityResolver,
	contentPathBaselines *recoveryContentPathBaselineCache,
) (recovery.BeforePathState, int, error) {
	if action.Kind == pathMutationCreate {
		if action.LiveExists {
			return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation for %q content path %q exists before create action", action.Destination, action.ContentPath)
		}
		before, err := captureAbsentContentPathRecoveryBefore(
			ctx,
			action,
			resolver,
			manifestAuthority,
			rootedCapability,
			contentPathBaselines,
		)
		return before, backupIndex, err
	}
	if !action.LiveExists {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation for %q content path %q is missing before %s action", action.Destination, action.ContentPath, action.Kind)
	}
	if action.LiveHash == "" {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation %q content path %q content hash is required", action.Destination, action.ContentPath)
	}
	if !action.LivePathExists {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation path %q is missing before %s action", action.Destination, action.Kind)
	}

	baseline, err := contentPathBaselines.capture(
		ctx,
		action,
		resolver,
		manifestAuthority,
		rootedCapability,
	)
	if err != nil {
		return recovery.BeforePathState{}, backupIndex, err
	}
	projection, present, err := baseline.projection(action.ContentPath)
	if err != nil {
		return recovery.BeforePathState{}, backupIndex, err
	}
	if !present {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation for %q content path %q reported existing projection but extraction found none", action.Destination, action.ContentPath)
	}
	if hash := artifact.HashFileContent(projection); hash != action.LiveHash {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation %q content path %q hash %q does not match extracted projection hash %q", action.Destination, action.ContentPath, action.LiveHash, hash)
	}

	backupIndex++
	backupPath := filepath.ToSlash(filepath.Join("files", fmt.Sprintf("%06d", backupIndex)))
	absoluteBackupPath := filepath.Join(operationDir, filepath.FromSlash(backupPath))
	if err := os.MkdirAll(filepath.Dir(absoluteBackupPath), 0o700); err != nil {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("create recovery projection backup parent: %w", err)
	}
	if err := os.WriteFile(absoluteBackupPath, projection, 0o600); err != nil {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("copy recovery projection backup for %q content path %q: %w", action.Destination, action.ContentPath, err)
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

func captureAbsentContentPathRecoveryBefore(
	ctx context.Context,
	action pathMutation,
	resolver func(destination output.Destination) (string, error),
	manifestAuthority *manifestAuthoritySession,
	rootedCapability RootedCapabilityResolver,
	contentPathBaselines *recoveryContentPathBaselineCache,
) (recovery.BeforePathState, error) {
	if !action.LivePathExists {
		return recovery.BeforePathState{Existed: false}, nil
	}
	baseline, err := contentPathBaselines.capture(
		ctx,
		action,
		resolver,
		manifestAuthority,
		rootedCapability,
	)
	if err != nil {
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

func captureExistingRecoveryBeforePath(
	ctx context.Context,
	filesystem mutationfs.PathReader,
	operationDir string,
	backupIndex int,
	action pathMutation,
	resolver func(destination output.Destination) (string, error),
) (recovery.BeforePathState, int, error) {
	hostPath, err := resolver(action.Destination)
	if err != nil {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("resolve destination %q: %w", action.Destination, err)
	}
	info, err := os.Lstat(hostPath)
	if err != nil {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("stat destination %q: %w", action.Destination, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("recovery journal cannot capture symlink destination %q yet", action.Destination)
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("destination %q has unsupported file mode %s", action.Destination, info.Mode())
	}

	backupIndex++
	backupPath := filepath.ToSlash(filepath.Join("files", fmt.Sprintf("%06d", backupIndex)))
	absoluteBackupPath := filepath.Join(operationDir, filepath.FromSlash(backupPath))
	pathKind := recovery.PathKindFile
	pathMode := regularFileMode(info)
	if info.IsDir() {
		pathKind = recovery.PathKindDirectory
		if err := copyDirectory(hostPath, absoluteBackupPath); err != nil {
			return recovery.BeforePathState{}, backupIndex, fmt.Errorf("copy recovery backup for %q: %w", action.Destination, err)
		}
	} else {
		commitPath, err := mutation.CanonicalDirectoryEntryPath(hostPath)
		if err != nil {
			return recovery.BeforePathState{}, backupIndex, fmt.Errorf(
				"canonicalize recovery backup source for %q: %w",
				action.Destination,
				err,
			)
		}
		snapshot, err := filesystem.ReadRegularFileSnapshotUpTo(
			ctx,
			commitPath,
			recovery.MaximumRecoveryBackupFileBytes,
		)
		if err != nil {
			return recovery.BeforePathState{}, backupIndex, fmt.Errorf("read recovery backup for %q: %w", action.Destination, err)
		}
		content := snapshot.Content()
		capturedHash := artifact.HashFileContentWithExecutable(
			content,
			snapshot.Mode().Perm()&0o111 != 0,
		)
		if capturedHash != action.LivePathHash {
			return recovery.BeforePathState{}, backupIndex, fmt.Errorf(
				"captured path %q hash %q does not match live observation %q",
				action.Destination,
				capturedHash,
				action.LivePathHash,
			)
		}
		if err := writeRecoveryBackupFile(absoluteBackupPath, content, snapshot.Mode()); err != nil {
			return recovery.BeforePathState{}, backupIndex, fmt.Errorf("copy recovery backup for %q: %w", action.Destination, err)
		}
		pathMode = recovery.NewPermissionMode(snapshot.Mode())
	}

	return recovery.BeforePathState{
		Existed:     true,
		PathMode:    pathMode,
		Kind:        pathKind,
		ContentHash: string(action.LivePathHash),
		BackupPath:  backupPath,
	}, backupIndex, nil
}

func regularFileMode(info os.FileInfo) *recovery.PermissionMode {
	if !info.Mode().IsRegular() {
		return nil
	}
	return recovery.NewPermissionMode(info.Mode())
}

func copyDirectory(sourcePath string, destinationPath string) error {
	sourceRoot := filepath.Clean(sourcePath)
	destinationRoot := filepath.Clean(destinationPath)
	if directoryContains(sourceRoot, destinationRoot) {
		return fmt.Errorf("copy destination %q must not be inside source directory %q", destinationRoot, sourceRoot)
	}
	return filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("stat %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path %q is a symlink; symlinks are not supported", path)
		}

		relativePath, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return fmt.Errorf("compute relative path for %q: %w", path, err)
		}
		destination := filepath.Join(destinationRoot, relativePath)
		if entry.IsDir() {
			if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
				return fmt.Errorf("create directory %q: %w", destination, err)
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("path %q has unsupported file mode %s", path, info.Mode())
		}
		if err := copyFilePreservingMode(path, destination, info.Mode().Perm()); err != nil {
			return fmt.Errorf("copy file %q: %w", path, err)
		}

		return nil
	})
}

func directoryContains(parent string, child string) bool {
	relativePath, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relativePath == "." || (relativePath != ".." && !strings.HasPrefix(relativePath, ".."+string(filepath.Separator)))
}

func copyFilePreservingMode(sourcePath string, destinationPath string, fileMode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o700); err != nil {
		return err
	}
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destinationFile, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(destinationFile, sourceFile); err != nil {
		destinationFile.Close()
		return err
	}
	if err := destinationFile.Close(); err != nil {
		return err
	}

	return nil
}
