package transaction

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
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
// persisted target belongs to the caller-supplied allowed path set. Success
// requires a clean fence: no published marker and no abandoned private
// file-set residue.
func RecoverFileSet(ctx context.Context, stateDir string, allowedPaths []string) error {
	canonical, err := canonicalStateDir(stateDir)
	if err != nil {
		return err
	}
	return recoverWithOperations(ctx, canonical, allowedPaths, operations{writeFile: commitFile})
}

func requireClearFileSetAtCanonicalPath(
	ctx context.Context,
	stateDir string,
	maximumPhysicalDepth int,
	physicalWorkBudget PhysicalWorkBudget,
) error {
	if ctx == nil {
		return fmt.Errorf("file-set transaction context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	evidencePath := transactionDir(stateDir)
	if err := admitFileSetFenceObservation(
		evidencePath,
		maximumPhysicalDepth,
		physicalWorkBudget,
		1,
		0,
	); err != nil {
		return err
	}
	info, err := os.Lstat(evidencePath)
	if errors.Is(err, os.ErrNotExist) {
		return rejectAbandonedFileSetResidueWithBudget(
			ctx,
			stateDir,
			maximumPhysicalDepth,
			physicalWorkBudget,
		)
	}
	if err != nil {
		return wrapFileSetAccessUnprovable(fmt.Errorf("inspect file-set transaction evidence: %w", err))
	}
	if !info.IsDir() {
		return wrapFileSetEvidenceInvalid(fmt.Errorf(
			"file-set transaction evidence at %s is not a directory",
			evidencePath,
		))
	}
	activeMarkerPath := markerPath(stateDir)
	if err := admitFileSetFenceObservation(
		activeMarkerPath,
		maximumPhysicalDepth,
		physicalWorkBudget,
		1,
		maximumMarkerBytes,
	); err != nil {
		return err
	}
	if _, err := loadMarkerAtCanonicalPath(ctx, activeMarkerPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return wrapFileSetEvidenceInvalid(fmt.Errorf(
				"file-set transaction evidence at %s is incomplete: marker is missing",
				evidencePath,
			))
		}
		return wrapFileSetEvidenceInvalid(err)
	}
	return fmt.Errorf("%w at %s", ErrInterruptedFileSetTransaction, activeMarkerPath)
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
			return rejectAbandonedFileSetResidue(ctx, stateDir)
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
		if err := removeTransactionDir(ctx, stateDir); err != nil {
			return err
		}
		return rejectAbandonedFileSetResidue(ctx, stateDir)
	}
	if classification.recoverable {
		if err := restoreTransaction(context.WithoutCancel(ctx), marker, ops); err != nil {
			return fmt.Errorf("restore interrupted file-set transaction: %w", err)
		}
		if err := removeTransactionDir(context.WithoutCancel(ctx), stateDir); err != nil {
			return err
		}
		return rejectAbandonedFileSetResidue(ctx, stateDir)
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
	if err := preflightRestorableBackups(ctx, marker); err != nil {
		return err
	}
	failures := make([]error, 0)
	for _, target := range marker.Targets {
		if err := restoreFile(ctx, target.Path, target.Before, ops); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func preflightRestorableBackups(ctx context.Context, marker transactionMarker) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := admitFileSetTargetCount(len(marker.Targets)); err != nil {
		return err
	}
	var stagedBeforeBytes int64
	for _, target := range marker.Targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !target.Before.Exists {
			continue
		}
		content, _, err := readTransactionFile(ctx, target.Before.BackupPath)
		if err != nil {
			return fmt.Errorf("preflight backup %q: %w", target.Before.BackupPath, err)
		}
		if err := admitStagedBeforeImageBytes(stagedBeforeBytes, len(content)); err != nil {
			return err
		}
		if hash := hashBytes(content); hash != target.Before.Hash {
			return fmt.Errorf(
				"backup %q hash %q does not match marker hash %q",
				target.Before.BackupPath,
				hash,
				target.Before.Hash,
			)
		}
		stagedBeforeBytes += int64(len(content))
	}
	return nil
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

const (
	fileSetTemporaryPrefix        = ".daem-tmp-"
	fileSetTombstonePrefix        = ".daem-tombstone-"
	fileSetCleanupPrefix          = ".daem-cleanup-"
	fileSetLegacyStagePrefix      = ".metadata-stage-"
	maximumStateDirFenceEntries   = 4096
	maximumStateDirFenceNameBytes = 4096
	stateDirFenceEnumerationBatch = 64
)

// FileSetFenceKind is the closed semantic classification of the declaration
// file-set boundary.
type FileSetFenceKind string

// FileSetFenceObservation preserves whether the file-set axis was observed,
// whether that observation was classified, and its closed kind when known.
type FileSetFenceObservation struct {
	observed bool
	known    bool
	kind     FileSetFenceKind
}

// ObserveFileSetFence classifies one completed file-set observation. Nil is a
// known clear observation; unrelated non-nil errors remain observed unknown.
func ObserveFileSetFence(err error) FileSetFenceObservation {
	if err == nil {
		return FileSetFenceObservation{observed: true, known: true}
	}
	kind := FileSetFenceKindOf(err)
	return FileSetFenceObservation{
		observed: true,
		known:    kind != FileSetFenceClear,
		kind:     kind,
	}
}

// KnownFileSetFence constructs one explicitly observed closed classification.
func KnownFileSetFence(kind FileSetFenceKind) FileSetFenceObservation {
	return FileSetFenceObservation{observed: true, known: true, kind: kind}
}

// UnobservedFileSetFence reports an intentionally omitted file-set axis.
func UnobservedFileSetFence() FileSetFenceObservation { return FileSetFenceObservation{} }

// Observed reports whether the file-set axis participated in this fact.
func (observation FileSetFenceObservation) Observed() bool { return observation.observed }

// Known reports whether an observed file-set result has a closed classification.
func (observation FileSetFenceObservation) Known() bool {
	return observation.observed && observation.known
}

// Kind returns the closed classification. Unknown and unobserved observations
// return FileSetFenceClear and must be distinguished with Known and Observed.
func (observation FileSetFenceObservation) Kind() FileSetFenceKind { return observation.kind }

const (
	FileSetFenceClear                FileSetFenceKind = ""
	FileSetFencePublishedTransaction FileSetFenceKind = "published_transaction"
	FileSetFenceInvalidEvidence      FileSetFenceKind = "invalid_evidence"
	FileSetFenceAbandonedResidue     FileSetFenceKind = "abandoned_residue"
	FileSetFenceCensusLimit          FileSetFenceKind = "census_limit"
	FileSetFenceAccessUnprovable     FileSetFenceKind = "access_unprovable"
)

// ErrInterruptedFileSetTransaction reports one valid published marker whose
// transaction remains a continuing fence until its owning workflow recovers it.
var ErrInterruptedFileSetTransaction = errors.New("interrupted file-set transaction requires recovery")

// ErrFileSetEvidenceInvalid reports published evidence whose authority cannot
// be reconstructed safely enough for journal recovery to proceed.
var ErrFileSetEvidenceInvalid = errors.New("file-set transaction evidence is incomplete or invalid")

// ErrAbandonedFileSetResidue reports markerless file-set-class residue under the
// state directory. A name prefix is not deletion authority, so recovery cannot
// clear the fence.
var ErrAbandonedFileSetResidue = errors.New("abandoned file-set transaction residue remains")

// ErrFileSetFenceUnprovable is the compatibility parent for the distinct census
// and StateDir access/identity failures below.
var ErrFileSetFenceUnprovable = errors.New(
	"file-set fence cannot be proven clean; restore access to the state directory or preserve it for analysis; do not retry an interrupted write or delete reserved names by prefix",
)

// ErrFileSetFenceCensusLimit reports bounded census exhaustion after StateDir
// access and directory identity were established.
var ErrFileSetFenceCensusLimit = errors.New("file-set fence census limit exceeded")

// ErrFileSetAccessUnprovable reports that StateDir path identity or enumerable
// directory access could not be established.
var ErrFileSetAccessUnprovable = errors.New("file-set state directory access or identity cannot be proven")

type fileSetFenceError struct {
	kind     FileSetFenceKind
	sentinel error
	cause    error
}

func (err fileSetFenceError) Error() string {
	return fmt.Sprintf("%s: %v", err.sentinel, err.cause)
}

func (err fileSetFenceError) Unwrap() []error {
	return []error{ErrFileSetFenceUnprovable, err.sentinel, err.cause}
}

func wrapFileSetFenceError(kind FileSetFenceKind, sentinel error, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if FileSetFenceKindOf(err) == kind {
		return err
	}
	return fileSetFenceError{kind: kind, sentinel: sentinel, cause: err}
}

func wrapFileSetAccessUnprovable(err error) error {
	return wrapFileSetFenceError(FileSetFenceAccessUnprovable, ErrFileSetAccessUnprovable, err)
}

func wrapFileSetCensusLimit(err error) error {
	return wrapFileSetFenceError(FileSetFenceCensusLimit, ErrFileSetFenceCensusLimit, err)
}

func wrapFileSetEvidenceInvalid(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, ErrFileSetEvidenceInvalid) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrFileSetEvidenceInvalid, err)
}

// FileSetFenceKindOf returns the most specific closed file-set classification
// preserved by err. Nil and unrelated errors both return FileSetFenceClear.
func FileSetFenceKindOf(err error) FileSetFenceKind {
	if err == nil {
		return FileSetFenceClear
	}
	var classified fileSetFenceError
	if errors.As(err, &classified) {
		return classified.kind
	}
	switch {
	case errors.Is(err, ErrInterruptedFileSetTransaction):
		return FileSetFencePublishedTransaction
	case errors.Is(err, ErrFileSetEvidenceInvalid):
		return FileSetFenceInvalidEvidence
	case errors.Is(err, ErrAbandonedFileSetResidue):
		return FileSetFenceAbandonedResidue
	case errors.Is(err, ErrFileSetFenceCensusLimit):
		return FileSetFenceCensusLimit
	case errors.Is(err, ErrFileSetAccessUnprovable),
		errors.Is(err, ErrFileSetFenceUnprovable):
		return FileSetFenceAccessUnprovable
	default:
		return FileSetFenceClear
	}
}

type abandonedFileSetResidueError struct {
	paths []string
}

func (err abandonedFileSetResidueError) Error() string {
	if len(err.paths) == 1 {
		return fmt.Sprintf(
			"%s at %s; missing published marker is not a clean fence; current daem cannot remove reserved residue by name",
			ErrAbandonedFileSetResidue,
			err.paths[0],
		)
	}
	return fmt.Sprintf(
		"%s (%d entries, first %s); missing published marker is not a clean fence; current daem cannot remove reserved residue by name",
		ErrAbandonedFileSetResidue,
		len(err.paths),
		err.paths[0],
	)
}

func (err abandonedFileSetResidueError) Unwrap() error {
	return ErrAbandonedFileSetResidue
}

func rejectAbandonedFileSetResidue(ctx context.Context, stateDir string) error {
	return rejectAbandonedFileSetResidueWithBudget(
		ctx,
		stateDir,
		mutationfs.MaximumPhysicalPathDepth,
		&fenceObservationBudget{},
	)
}

func rejectAbandonedFileSetResidueWithBudget(
	ctx context.Context,
	stateDir string,
	maximumPhysicalDepth int,
	physicalWorkBudget PhysicalWorkBudget,
) error {
	residue, err := inspectAbandonedFileSetResidueLimitedWithBudget(
		ctx,
		stateDir,
		maximumStateDirFenceEntries,
		maximumPhysicalDepth,
		physicalWorkBudget,
	)
	if err != nil {
		return err
	}
	if len(residue) == 0 {
		return nil
	}
	return abandonedFileSetResidueError{paths: residue}
}

func inspectAbandonedFileSetResidue(ctx context.Context, stateDir string) ([]string, error) {
	return inspectAbandonedFileSetResidueLimited(ctx, stateDir, maximumStateDirFenceEntries)
}

func inspectAbandonedFileSetResidueLimited(
	ctx context.Context,
	stateDir string,
	limit int,
) ([]string, error) {
	return inspectAbandonedFileSetResidueLimitedWithBudget(
		ctx,
		stateDir,
		limit,
		mutationfs.MaximumPhysicalPathDepth,
		&fenceObservationBudget{},
	)
}

func inspectAbandonedFileSetResidueLimitedWithBudget(
	ctx context.Context,
	stateDir string,
	limit int,
	maximumPhysicalDepth int,
	physicalWorkBudget PhysicalWorkBudget,
) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 {
		return nil, fmt.Errorf("file-set state dir entry limit must be positive")
	}
	if physicalWorkBudget == nil {
		return nil, fmt.Errorf("file-set state dir physical work budget is required")
	}
	if err := admitFileSetFenceObservation(
		stateDir,
		maximumPhysicalDepth,
		physicalWorkBudget,
		1,
		0,
	); err != nil {
		return nil, err
	}
	info, err := os.Lstat(stateDir)
	if errors.Is(err, os.ErrNotExist) {
		return finishFileSetResidueInspection(ctx, nil)
	}
	if err != nil {
		return nil, wrapFileSetAccessUnprovable(fmt.Errorf("inspect file-set state dir: %w", err))
	}
	if !info.IsDir() {
		return nil, wrapFileSetAccessUnprovable(fmt.Errorf("file-set state dir %s is not a directory", stateDir))
	}
	if err := admitFileSetFenceObservation(
		stateDir,
		maximumPhysicalDepth,
		physicalWorkBudget,
		1,
		0,
	); err != nil {
		return nil, err
	}
	directory, err := os.Open(stateDir)
	if err != nil {
		return nil, wrapFileSetAccessUnprovable(fmt.Errorf("open file-set state dir: %w", err))
	}
	defer directory.Close()

	residue := make([]string, 0)
	seen := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		names, readErr := directory.Readdirnames(stateDirFenceEnumerationBatch)
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if len(name) > maximumStateDirFenceNameBytes {
				return nil, wrapFileSetCensusLimit(fmt.Errorf(
					"file-set state dir entry name contains %d bytes, maximum %d",
					len(name),
					maximumStateDirFenceNameBytes,
				))
			}
			if err := admitFileSetFenceWork(
				physicalWorkBudget,
				fileSetPhysicalWork{entries: 1, bytes: int64(len(name))},
			); err != nil {
				return nil, err
			}
			seen++
			if seen > limit {
				return nil, wrapFileSetCensusLimit(fmt.Errorf(
					"file-set state dir %s exceeds %d entries; cannot prove the fence is clean",
					stateDir,
					limit,
				))
			}
			if name == transactionDirName || !fileSetPrivateName(name) {
				continue
			}
			path := filepath.Join(stateDir, name)
			if err := admitFileSetFenceObservation(
				path,
				maximumPhysicalDepth,
				physicalWorkBudget,
				1,
				0,
			); err != nil {
				return nil, err
			}
			entry, lstatErr := os.Lstat(path)
			if errors.Is(lstatErr, os.ErrNotExist) {
				continue
			}
			if lstatErr != nil {
				return nil, wrapFileSetAccessUnprovable(fmt.Errorf("inspect file-set state dir entry %s: %w", path, lstatErr))
			}
			if fileSetFenceResidue(name, entry.Mode()) {
				residue = append(residue, path)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return finishFileSetResidueInspection(ctx, residue)
			}
			return nil, wrapFileSetAccessUnprovable(fmt.Errorf("enumerate file-set state dir: %w", readErr))
		}
		if len(names) == 0 {
			return finishFileSetResidueInspection(ctx, residue)
		}
	}
}

func finishFileSetResidueInspection(ctx context.Context, residue []string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slices.Sort(residue)
	return residue, nil
}

func admitFileSetFenceObservation(
	path string,
	maximumPhysicalDepth int,
	physicalWorkBudget PhysicalWorkBudget,
	entries int,
	bytes int64,
) error {
	pathWork, err := fileSetAbsolutePathWork(path, maximumPhysicalDepth)
	if err != nil {
		return wrapFileSetAccessUnprovable(fmt.Errorf(
			"measure file-set fence path work: %w",
			err,
		))
	}
	return admitFileSetFenceWork(physicalWorkBudget, fileSetPhysicalWork{
		pathComponents: pathWork,
		entries:        entries,
		bytes:          bytes,
	})
}

func admitFileSetFenceWork(
	physicalWorkBudget PhysicalWorkBudget,
	work fileSetPhysicalWork,
) error {
	if physicalWorkBudget == nil {
		return fmt.Errorf("file-set state dir physical work budget is required")
	}
	if err := physicalWorkBudget.AdmitPhysicalWork(
		work.pathComponents,
		work.entries,
		work.bytes,
	); err != nil {
		return wrapFileSetAccessUnprovable(fmt.Errorf(
			"admit file-set fence physical work: %w",
			err,
		))
	}
	return nil
}

func maximumFileSetFenceCensusWork(
	stateDir string,
	maximumPhysicalDepth int,
) (fileSetPhysicalWork, error) {
	evidencePathWork, err := fileSetAbsolutePathWork(
		transactionDir(stateDir),
		maximumPhysicalDepth,
	)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	stateDirPathWork, err := fileSetAbsolutePathWork(stateDir, maximumPhysicalDepth)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	childPathWork, err := fileSetAbsolutePathWork(
		filepath.Join(stateDir, fileSetTemporaryPrefix+"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		maximumPhysicalDepth,
	)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	residuePathWork, err := fileSetWorkMultiply(
		childPathWork,
		maximumStateDirFenceEntries,
	)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	stateDirOpenWork, err := fileSetWorkMultiply(stateDirPathWork, 2)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	residuePathWork, err = fileSetWorkAdd(residuePathWork, stateDirOpenWork)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	residuePathWork, err = fileSetWorkAdd(residuePathWork, evidencePathWork)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	residueEntries, err := fileSetWorkAdd(
		maximumStateDirFenceEntries+1,
		maximumStateDirFenceEntries+3,
	)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	residueBytes := int64(maximumStateDirFenceEntries+1) * maximumStateDirFenceNameBytes

	markerPathWork, err := fileSetAbsolutePathWork(markerPath(stateDir), maximumPhysicalDepth)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	markerPathWork, err = fileSetWorkAdd(markerPathWork, evidencePathWork)
	if err != nil {
		return fileSetPhysicalWork{}, err
	}
	marker := fileSetPhysicalWork{
		pathComponents: markerPathWork,
		entries:        2,
		bytes:          maximumMarkerBytes,
	}
	residue := fileSetPhysicalWork{
		pathComponents: residuePathWork,
		entries:        residueEntries,
		bytes:          residueBytes,
	}
	return fileSetPhysicalWork{
		pathComponents: max(marker.pathComponents, residue.pathComponents),
		entries:        max(marker.entries, residue.entries),
		bytes:          max(marker.bytes, residue.bytes),
	}, nil
}

func fileSetPrivateName(name string) bool {
	return strings.HasPrefix(name, fileSetTemporaryPrefix) ||
		strings.HasPrefix(name, fileSetTombstonePrefix) ||
		strings.HasPrefix(name, fileSetCleanupPrefix) ||
		strings.HasPrefix(name, fileSetLegacyStagePrefix)
}

func fileSetFenceResidue(name string, mode os.FileMode) bool {
	return fileSetPrivateName(name) && !mode.IsRegular()
}
