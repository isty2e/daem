package transaction

import (
	"context"
	"errors"
	"fmt"
	"os"

	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

// FileSetInput names the exact files and private evidence root for one transaction.
type FileSetInput struct {
	StateDir string
	Targets  []FileTarget
}

type operations struct {
	writeFile func(context.Context, string, []byte, os.FileMode) error
}

// FileSetAuthorityPath returns the stable evidence root a caller must lease before
// checking, recovering, or committing a transaction.
func FileSetAuthorityPath(stateDir string) (string, error) {
	canonical, err := canonicalStateDir(stateDir)
	if err != nil {
		return "", err
	}
	return transactionDir(canonical), nil
}

// CommitFileSet publishes all target after-images under recoverable evidence. The
// caller must hold the complete target and AuthorityPath mutation lease set.
func CommitFileSet(ctx context.Context, input FileSetInput) error {
	return commitWithOperations(ctx, input, operations{writeFile: commitFile})
}

func commitWithOperations(ctx context.Context, input FileSetInput, ops operations) error {
	if ctx == nil {
		return fmt.Errorf("file-set transaction context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	stateDir, err := canonicalStateDir(input.StateDir)
	if err != nil {
		return err
	}
	targets, err := canonicalTargets(input.Targets)
	if err != nil {
		return err
	}
	if ops.writeFile == nil {
		ops.writeFile = commitFile
	}
	allowed := make([]string, 0, len(targets))
	for _, target := range targets {
		allowed = append(allowed, target.path)
	}
	if err := recoverWithOperations(ctx, stateDir, allowed, ops); err != nil {
		return err
	}
	marker, err := prepareMarker(ctx, stateDir, targets)
	if err != nil {
		return err
	}
	activeMarkerPath := markerPath(stateDir)
	if err := ctx.Err(); err != nil {
		if cleanupErr := removeTransactionDir(context.WithoutCancel(ctx), stateDir); cleanupErr != nil {
			return fmt.Errorf("%w; remove file-set transaction marker: %v", err, cleanupErr)
		}
		return err
	}

	for index, target := range targets {
		if !target.write {
			continue
		}
		if err := ops.writeFile(ctx, target.path, target.content, fileMode(marker.Targets[index].Before)); err != nil {
			return rollbackFailure(ctx, fmt.Errorf("write target %q: %w", target.path, err), marker, stateDir, activeMarkerPath, ops)
		}
		if err := ctx.Err(); err != nil {
			return rollbackFailure(ctx, err, marker, stateDir, activeMarkerPath, ops)
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w; file-set after-images committed; transaction marker remains at %s", err, activeMarkerPath)
	}
	if err := removeTransactionDir(ctx, stateDir); err != nil {
		return fmt.Errorf("remove file-set transaction marker: %w", err)
	}
	return nil
}

// RecoverFileSet restores or finalizes an interrupted transaction only when every
// persisted target belongs to the caller-supplied allowed path set.
func RecoverFileSet(ctx context.Context, stateDir string, allowedPaths []string) error {
	canonical, err := canonicalStateDir(stateDir)
	if err != nil {
		return err
	}
	return recoverWithOperations(ctx, canonical, allowedPaths, operations{writeFile: commitFile})
}

// RequireClearFileSet fails closed when transaction evidence exists. Mutating
// callers hold AuthorityPath; read-only callers use this as a persisted-evidence gate.
func RequireClearFileSet(ctx context.Context, stateDir string) error {
	if ctx == nil {
		return fmt.Errorf("file-set transaction context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	canonical, err := canonicalStateDir(stateDir)
	if err != nil {
		return err
	}
	evidencePath := transactionDir(canonical)
	info, err := os.Lstat(evidencePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect file-set transaction evidence: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("file-set transaction evidence at %s is not a directory", evidencePath)
	}
	activeMarkerPath := markerPath(canonical)
	if _, err := loadMarker(ctx, activeMarkerPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf(
				"file-set transaction evidence at %s is incomplete: marker is missing",
				evidencePath,
			)
		}
		return err
	}
	return fmt.Errorf("interrupted file-set transaction requires recovery at %s", activeMarkerPath)
}

func recoverWithOperations(
	ctx context.Context,
	stateDir string,
	allowedPaths []string,
	ops operations,
) error {
	if ctx == nil {
		return fmt.Errorf("file-set transaction context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if ops.writeFile == nil {
		ops.writeFile = commitFile
	}
	marker, err := loadMarker(ctx, markerPath(stateDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	allowed, err := canonicalAllowedPaths(allowedPaths)
	if err != nil {
		return err
	}
	for _, target := range marker.Targets {
		if _, ok := allowed[target.Path]; !ok {
			return fmt.Errorf("interrupted file-set transaction target %q is outside current recovery authority", target.Path)
		}
	}

	classification, err := classifyTargets(ctx, marker)
	if err != nil {
		return err
	}
	if classification.cleanAfter {
		return removeTransactionDir(ctx, stateDir)
	}
	if classification.recoverable {
		if err := restoreTransaction(context.WithoutCancel(ctx), marker, ops); err != nil {
			return fmt.Errorf("restore interrupted file-set transaction: %w", err)
		}
		return removeTransactionDir(context.WithoutCancel(ctx), stateDir)
	}
	return fmt.Errorf("interrupted file-set transaction at %s cannot be recovered automatically", markerPath(stateDir))
}

type targetClassification struct {
	cleanAfter  bool
	recoverable bool
}

func classifyTargets(ctx context.Context, marker transactionMarker) (targetClassification, error) {
	result := targetClassification{cleanAfter: true, recoverable: true}
	for index, target := range marker.Targets {
		before, err := fileMatchesState(ctx, target.Path, target.Before)
		if err != nil {
			return targetClassification{}, fmt.Errorf("inspect target[%d] before-state: %w", index, err)
		}
		after := before
		if target.Write {
			after, err = fileMatchesExpected(ctx, target.Path, target.AfterHash, fileMode(target.Before))
			if err != nil {
				return targetClassification{}, fmt.Errorf("inspect target[%d] after-state: %w", index, err)
			}
		}
		result.cleanAfter = result.cleanAfter && after
		result.recoverable = result.recoverable && (before || after)
	}
	return result, nil
}

func restoreTransaction(ctx context.Context, marker transactionMarker, ops operations) error {
	failures := make([]error, 0)
	for _, target := range marker.Targets {
		if err := restoreFile(ctx, target.Path, target.Before, ops); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func restoreFile(ctx context.Context, path string, state fileState, ops operations) error {
	if !state.Exists {
		if err := removeFile(ctx, path); err != nil {
			return fmt.Errorf("remove %q: %w", path, err)
		}
		return nil
	}
	content, _, err := readTransactionFile(ctx, state.BackupPath)
	if err != nil {
		return fmt.Errorf("read backup %q: %w", state.BackupPath, err)
	}
	if hash := hashBytes(content); hash != state.Hash {
		return fmt.Errorf("backup %q hash %q does not match marker hash %q", state.BackupPath, hash, state.Hash)
	}
	if err := ops.writeFile(ctx, path, content, fileMode(state)); err != nil {
		return fmt.Errorf("restore %q: %w", path, err)
	}
	return nil
}

func fileMode(state fileState) os.FileMode {
	if state.Exists && state.Mode != 0 {
		return os.FileMode(state.Mode)
	}
	return 0o600
}

func fileMatchesState(ctx context.Context, path string, state fileState) (bool, error) {
	if !state.Exists {
		_, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	return fileMatchesExpected(ctx, path, state.Hash, fileMode(state))
}

func fileMatchesExpected(ctx context.Context, path string, hash string, mode os.FileMode) (bool, error) {
	content, observedMode, err := readTransactionFile(ctx, path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return hashBytes(content) == hash && observedMode.Perm() == mode.Perm(), nil
}

func rollbackFailure(
	ctx context.Context,
	primary error,
	marker transactionMarker,
	stateDir string,
	activeMarkerPath string,
	ops operations,
) error {
	rollbackContext := context.WithoutCancel(ctx)
	classification, classificationErr := classifyTargets(rollbackContext, marker)
	if classificationErr != nil || !classification.recoverable {
		if classificationErr != nil {
			return fmt.Errorf("%w; rollback classification failed: %v; transaction marker remains at %s", primary, classificationErr, activeMarkerPath)
		}
		return fmt.Errorf("%w; rollback refused because a target is neither before nor after; transaction marker remains at %s", primary, activeMarkerPath)
	}
	if rollbackErr := restoreTransaction(rollbackContext, marker, ops); rollbackErr != nil {
		return fmt.Errorf("%w; rollback failed: %v; transaction marker remains at %s", primary, rollbackErr, activeMarkerPath)
	}
	if cleanupErr := removeTransactionDir(rollbackContext, stateDir); cleanupErr != nil {
		return fmt.Errorf("%w; rolled back file set; remove transaction marker: %v", primary, cleanupErr)
	}
	return fmt.Errorf("%w; rolled back file set", primary)
}

func removeTransactionDir(ctx context.Context, stateDir string) error {
	path := transactionDir(stateDir)
	expected, err := storagecommit.CaptureEntryIdentity(ctx, path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	request, err := storagecommit.NewLogicalRemoval(path, expected)
	if err != nil {
		return err
	}
	return storagecommit.CommitLogicalRemoval(ctx, request)
}
