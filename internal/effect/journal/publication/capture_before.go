package publication

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

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
	action journalrecord.CaptureMutation,
	filesystem mutationfs.Reader,
	resolver func(destination output.Destination) (string, error),
	manifestAuthority *journalnamespace.ManifestAuthority,
	rootedCapability journalnamespace.CapabilityResolver,
	contentPathBaselines *recoveryContentPathBaselineCache,
	work *ordinaryCaptureWork,
) (recovery.BeforePathState, int, error) {
	if err := action.Validate(); err != nil {
		return recovery.BeforePathState{}, backupIndex, err
	}
	if action.ContentPath() != "" {
		if action.Scope() == target.ScopeProject {
			return captureProjectContentPathRecoveryBeforePath(
				ctx,
				operationDir,
				backupIndex,
				action,
				filesystem,
				manifestAuthority,
				contentPathBaselines,
				work,
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
			work,
		)
	}
	if action.Kind() == journalrecord.CaptureCreate {
		if action.LiveExists() {
			return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation for %q exists before create action", action.Destination())
		}
		if !action.LivePathExists() {
			if action.Scope() == target.ScopeProject {
				if err := validateAbsentProjectRecoveryPath(ctx, filesystem, action, manifestAuthority); err != nil {
					return recovery.BeforePathState{}, backupIndex, err
				}
			} else if capability, present, err := acquireRootedJournalCapability(
				action.Destination(),
				resolver,
				rootedCapability,
			); err != nil {
				return recovery.BeforePathState{}, backupIndex, err
			} else if present {
				if _, captureErr := filesystem.CaptureRootedEntryIdentity(ctx, capability); captureErr == nil {
					_ = capability.Close()
					return recovery.BeforePathState{}, backupIndex, fmt.Errorf("destination %q appeared after observation", action.Destination())
				} else if !errors.Is(captureErr, os.ErrNotExist) {
					return recovery.BeforePathState{}, backupIndex, errors.Join(captureErr, capability.Close())
				}
				if closeErr := capability.Close(); closeErr != nil {
					return recovery.BeforePathState{}, backupIndex, closeErr
				}
			}
			return recovery.BeforePathState{Existed: false}, backupIndex, nil
		}
		if action.Scope() == target.ScopeProject {
			return captureProjectExistingRecoveryBeforePath(
				ctx,
				filesystem,
				operationDir,
				backupIndex,
				action,
				manifestAuthority,
				work,
			)
		}
		if capability, present, err := acquireRootedJournalCapability(
			action.Destination(),
			resolver,
			rootedCapability,
		); err != nil {
			return recovery.BeforePathState{}, backupIndex, err
		} else if present {
			return captureAndCloseRootedRecoveryBefore(
				ctx, filesystem, operationDir, backupIndex, action, capability, work,
			)
		}
		return captureExistingRecoveryBeforePath(
			ctx, filesystem, operationDir, backupIndex, action, resolver, work,
		)
	}
	if !action.LiveExists() {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation for %q is missing before %s action", action.Destination(), action.Kind())
	}
	if action.LiveHash() == "" {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation %q content hash is required", action.Destination())
	}
	if !action.LivePathExists() {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation path %q is missing before %s action", action.Destination(), action.Kind())
	}
	if action.LivePathHash() == "" {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation %q path content hash is required", action.Destination())
	}

	if action.Scope() == target.ScopeProject {
		return captureProjectExistingRecoveryBeforePath(
			ctx,
			filesystem,
			operationDir,
			backupIndex,
			action,
			manifestAuthority,
			work,
		)
	}
	if capability, present, err := acquireRootedJournalCapability(
		action.Destination(),
		resolver,
		rootedCapability,
	); err != nil {
		return recovery.BeforePathState{}, backupIndex, err
	} else if present {
		return captureAndCloseRootedRecoveryBefore(
			ctx, filesystem, operationDir, backupIndex, action, capability, work,
		)
	}
	return captureExistingRecoveryBeforePath(
		ctx, filesystem, operationDir, backupIndex, action, resolver, work,
	)
}

func acquireRootedJournalCapability(
	destination output.Destination,
	resolver func(output.Destination) (string, error),
	acquire journalnamespace.CapabilityResolver,
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
	capability, present, err := journalnamespace.AcquireMatchingCapability(destination, resolvedPath, acquire, nil)
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
	action journalrecord.CaptureMutation,
	capability rootedpath.CommitCapability,
	work *ordinaryCaptureWork,
) (recovery.BeforePathState, int, error) {
	before, nextBackupIndex, captureErr := captureRootedExistingWithCapability(
		ctx, filesystem, operationDir, backupIndex, action, capability, work,
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
	action journalrecord.CaptureMutation,
	resolver func(destination output.Destination) (string, error),
	manifestAuthority *journalnamespace.ManifestAuthority,
	rootedCapability journalnamespace.CapabilityResolver,
	contentPathBaselines *recoveryContentPathBaselineCache,
	work *ordinaryCaptureWork,
) (recovery.BeforePathState, int, error) {
	if action.Kind() == journalrecord.CaptureCreate {
		if action.LiveExists() {
			return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation for %q content path %q exists before create action", action.Destination(), action.ContentPath())
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
	if !action.LiveExists() {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation for %q content path %q is missing before %s action", action.Destination(), action.ContentPath(), action.Kind())
	}
	if action.LiveHash() == "" {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation %q content path %q content hash is required", action.Destination(), action.ContentPath())
	}
	if !action.LivePathExists() {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation path %q is missing before %s action", action.Destination(), action.Kind())
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
	projection, present, err := baseline.projection(action.ContentPath())
	if err != nil {
		return recovery.BeforePathState{}, backupIndex, err
	}
	if !present {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation for %q content path %q reported existing projection but extraction found none", action.Destination(), action.ContentPath())
	}
	if hash := artifact.HashFileContent(projection); hash != action.LiveHash() {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("live observation %q content path %q hash %q does not match extracted projection hash %q", action.Destination(), action.ContentPath(), action.LiveHash(), hash)
	}

	backupIndex++
	backupPath := filepath.ToSlash(filepath.Join("files", fmt.Sprintf("%06d", backupIndex)))
	absoluteBackupPath := filepath.Join(operationDir, filepath.FromSlash(backupPath))
	if err := work.writeFile(absoluteBackupPath, projection, 0o600); err != nil {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("copy recovery projection backup for %q content path %q: %w", action.Destination(), action.ContentPath(), err)
	}

	return recovery.BeforePathState{
		Existed:       true,
		PathExisted:   true,
		ParentExisted: true,
		PathMode:      recovery.NewPermissionMode(baseline.mode),
		Kind:          recovery.PathKindFile,
		ContentHash:   string(action.LiveHash()),
		BackupPath:    backupPath,
	}, backupIndex, nil
}

func captureAbsentContentPathRecoveryBefore(
	ctx context.Context,
	action journalrecord.CaptureMutation,
	resolver func(destination output.Destination) (string, error),
	manifestAuthority *journalnamespace.ManifestAuthority,
	rootedCapability journalnamespace.CapabilityResolver,
	contentPathBaselines *recoveryContentPathBaselineCache,
) (recovery.BeforePathState, error) {
	if !action.LivePathExists() {
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
	parentExisted, err := baseline.parentExisted(action.ContentPath())
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
	filesystem mutationfs.RootedReader,
	operationDir string,
	backupIndex int,
	action journalrecord.CaptureMutation,
	resolver func(destination output.Destination) (string, error),
	work *ordinaryCaptureWork,
) (recovery.BeforePathState, int, error) {
	hostPath, err := resolver(action.Destination())
	if err != nil {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("resolve destination %q: %w", action.Destination(), err)
	}
	entry, err := mutation.CanonicalDirectoryEntryPath(hostPath)
	if err != nil {
		return recovery.BeforePathState{}, backupIndex, err
	}
	backupDir, err := mutation.CanonicalDirectoryEntryPath(operationDir)
	if err != nil {
		return recovery.BeforePathState{}, backupIndex, err
	}
	if directoryContains(entry, backupDir) {
		return recovery.BeforePathState{}, backupIndex, fmt.Errorf("backup directory is inside source %q", action.Destination())
	}
	root, destination, err := rootedpath.CaptureDestinationNoFollow(entry)
	if err != nil {
		return recovery.BeforePathState{}, backupIndex, err
	}
	capability, err := root.Acquire(destination)
	if err != nil {
		return recovery.BeforePathState{}, backupIndex, errors.Join(err, root.Close())
	}
	before, next, err := captureAndCloseRootedRecoveryBefore(ctx, filesystem, operationDir, backupIndex, action, capability, work)
	return before, next, errors.Join(err, root.Close())
}

func directoryContains(parent string, child string) bool {
	relativePath, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relativePath == "." || (relativePath != ".." && !strings.HasPrefix(relativePath, ".."+string(filepath.Separator)))
}
