package fileset

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

// ErrFileSetReferenceNamespaceMismatch reports a valid reference marker that
// does not match the namespace selected by the caller.
var ErrFileSetReferenceNamespaceMismatch = errors.New("file-set reference namespace mismatch")

type reconstructedReferenceEvidence struct {
	namespace    string
	beforeExists bool
	before       []byte
	after        []byte
	afterHash    string
}

func reconstructReferenceEvidenceWithBudget(
	ctx context.Context,
	markerPath string,
	marker transactionMarker,
	maximumPhysicalDepth int,
	physicalWorkBudget PhysicalWorkBudget,
) (*reconstructedReferenceEvidence, error) {
	if maximumPhysicalDepth <= 0 || physicalWorkBudget == nil {
		return nil, fmt.Errorf("generation-4 evidence reconstruction requires a path depth and shared work budget")
	}
	if err := preflightReconstructedEvidenceSizes(ctx, markerPath, marker, maximumPhysicalDepth, physicalWorkBudget); err != nil {
		return nil, err
	}
	var stagedBytes int64
	var evidence *reconstructedReferenceEvidence
	if marker.Reference != nil {
		evidence = &reconstructedReferenceEvidence{
			namespace: marker.Reference.Target,
			afterHash: marker.Reference.AfterImage.Digest,
		}
	}
	for _, target := range marker.Targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !target.Before.Exists {
			if evidence != nil && target.Path == evidence.namespace {
				evidence.beforeExists = false
			}
			continue
		}
		content, nextTotal, err := readEvidenceImageWithBudget(
			ctx,
			target.Before.BackupPath,
			fmt.Sprintf("target before-image %q", target.Path),
			stagedBytes,
			target.Before.Hash,
			-1,
			maximumPhysicalDepth,
			physicalWorkBudget,
		)
		if err != nil {
			return nil, err
		}
		stagedBytes = nextTotal
		if evidence != nil && target.Path == evidence.namespace {
			evidence.beforeExists = true
			evidence.before = content
		}
	}
	if evidence == nil {
		return nil, nil
	}
	content, _, err := readEvidenceImageWithBudget(
		ctx,
		filepath.Join(filepath.Dir(markerPath), marker.Reference.AfterImage.Path),
		"reference after-image",
		stagedBytes,
		marker.Reference.AfterImage.Digest,
		marker.Reference.AfterImage.Size,
		maximumPhysicalDepth,
		physicalWorkBudget,
	)
	if err != nil {
		return nil, err
	}
	evidence.after = content
	return evidence, nil
}

func preflightReconstructedEvidenceSizes(
	ctx context.Context,
	markerPath string,
	marker transactionMarker,
	maximumPhysicalDepth int,
	physicalWorkBudget PhysicalWorkBudget,
) error {
	var stagedBytes int64
	for _, target := range marker.Targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !target.Before.Exists {
			continue
		}
		size, err := inspectEvidenceImageSize(
			target.Before.BackupPath,
			fmt.Sprintf("target before-image %q", target.Path),
			-1,
			maximumPhysicalDepth,
			physicalWorkBudget,
		)
		if err != nil {
			return err
		}
		if err := admitStagedEvidenceBytes(stagedBytes, int(size), "file-set transaction evidence"); err != nil {
			return err
		}
		stagedBytes += size
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if marker.Reference != nil {
		size, err := inspectEvidenceImageSize(
			filepath.Join(filepath.Dir(markerPath), marker.Reference.AfterImage.Path),
			"reference after-image",
			marker.Reference.AfterImage.Size,
			maximumPhysicalDepth,
			physicalWorkBudget,
		)
		if err != nil {
			return err
		}
		if err := admitStagedEvidenceBytes(stagedBytes, int(size), "file-set transaction evidence"); err != nil {
			return err
		}
	}
	return nil
}

func inspectEvidenceImageSize(
	path string,
	name string,
	expectedSize int64,
	maximumPhysicalDepth int,
	physicalWorkBudget PhysicalWorkBudget,
) (int64, error) {
	if physicalWorkBudget != nil {
		pathWork, err := fileSetAbsolutePathWork(path, maximumPhysicalDepth)
		if err != nil {
			return 0, wrapFileSetAccessUnprovable(fmt.Errorf("measure %s path work: %w", name, err))
		}
		if err := admitFileSetFenceWork(physicalWorkBudget, fileSetPhysicalWork{
			pathComponents: pathWork,
			entries:        1,
		}); err != nil {
			return 0, err
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return 0, fmt.Errorf("inspect %s: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", name)
	}
	size := info.Size()
	if size < 0 || size > maximumTargetBytes {
		return 0, fmt.Errorf("%s contains %d bytes, maximum %d", name, size, maximumTargetBytes)
	}
	if expectedSize >= 0 && size != expectedSize {
		return 0, fmt.Errorf("%s size %d does not match marker size %d", name, size, expectedSize)
	}
	return size, nil
}

// readEvidenceImageWithBudget checks the aggregate envelope using the observed
// size before allocating the image, then verifies the stable no-follow snapshot.
// expectedSize is negative when the marker carries only a digest for the file.
func readEvidenceImageWithBudget(
	ctx context.Context,
	path string,
	name string,
	stagedBytes int64,
	expectedHash string,
	expectedSize int64,
	maximumPhysicalDepth int,
	physicalWorkBudget PhysicalWorkBudget,
) ([]byte, int64, error) {
	if physicalWorkBudget != nil {
		pathWork, err := fileSetAbsolutePathWork(path, maximumPhysicalDepth)
		if err != nil {
			return nil, stagedBytes, wrapFileSetAccessUnprovable(fmt.Errorf("measure %s path work: %w", name, err))
		}
		if err := admitFileSetFenceWork(physicalWorkBudget, fileSetPhysicalWork{
			pathComponents: pathWork,
			entries:        1,
		}); err != nil {
			return nil, stagedBytes, err
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, stagedBytes, fmt.Errorf("inspect %s: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, stagedBytes, fmt.Errorf("%s is not a regular file", name)
	}
	size := info.Size()
	if size < 0 || size > maximumTargetBytes {
		return nil, stagedBytes, fmt.Errorf(
			"%s contains %d bytes, maximum %d",
			name,
			size,
			maximumTargetBytes,
		)
	}
	if expectedSize >= 0 && size != expectedSize {
		return nil, stagedBytes, fmt.Errorf("%s size %d does not match marker size %d", name, size, expectedSize)
	}
	if err := admitStagedEvidenceBytes(stagedBytes, int(size), "file-set transaction evidence"); err != nil {
		return nil, stagedBytes, err
	}
	if physicalWorkBudget != nil {
		if err := admitFileSetFenceWork(physicalWorkBudget, fileSetPhysicalWork{bytes: size}); err != nil {
			return nil, stagedBytes, err
		}
	}

	content, mode, err := readTransactionFileUpTo(ctx, path, size)
	if err != nil {
		return nil, stagedBytes, fmt.Errorf("read %s: %w", name, err)
	}
	if mode.Perm() != transactionEvidenceMode {
		return nil, stagedBytes, fmt.Errorf("%s permissions are %04o, want %04o", name, mode.Perm(), transactionEvidenceMode)
	}
	if int64(len(content)) != size {
		return nil, stagedBytes, fmt.Errorf("%s changed size while its evidence was read", name)
	}
	if expectedSize >= 0 && int64(len(content)) != expectedSize {
		return nil, stagedBytes, fmt.Errorf("%s size does not match marker size %d", name, expectedSize)
	}
	if hash := hashBytes(content); hash != expectedHash {
		return nil, stagedBytes, fmt.Errorf("%s digest %q does not match marker digest %q", name, hash, expectedHash)
	}
	return content, stagedBytes + int64(len(content)), nil
}

// ReadFileSetReferenceEvidence4 reconstructs a read-only view for exactly the
// caller-selected canonical reference namespace from a generation-4 marker.
func ReadFileSetReferenceEvidence4(
	ctx context.Context,
	stateDir string,
	referenceNamespace string,
) (ReferenceEvidenceView, error) {
	if ctx == nil {
		return ReferenceEvidenceView{}, fmt.Errorf("file-set reference evidence context is required")
	}
	if err := ctx.Err(); err != nil {
		return ReferenceEvidenceView{}, err
	}
	maximumPhysicalDepth := mutationfs.MaximumPhysicalPathDepth
	physicalWorkBudget := &fenceObservationBudget{}
	canonicalNamespace, err := canonicalReferenceNamespaceBounded(
		referenceNamespace,
		maximumPhysicalDepth,
		physicalWorkBudget,
	)
	if err != nil {
		return ReferenceEvidenceView{}, err
	}
	canonicalState, err := canonicalStateDirBounded(stateDir, maximumPhysicalDepth, physicalWorkBudget)
	if err != nil {
		return ReferenceEvidenceView{}, err
	}
	marker, markerBytes, evidence, err := loadMarker4WithBudget(
		ctx,
		markerPath(canonicalState),
		maximumPhysicalDepth,
		physicalWorkBudget,
	)
	if err != nil {
		return ReferenceEvidenceView{}, wrapFileSetEvidenceInvalid(err)
	}
	if marker.Reference == nil || evidence == nil {
		return ReferenceEvidenceView{}, wrapFileSetEvidenceInvalid(fmt.Errorf("interrupted file-set has no reference evidence"))
	}
	if canonicalNamespace != evidence.namespace {
		return ReferenceEvidenceView{}, fmt.Errorf(
			"%w: recorded target %q does not match selected reference namespace %q",
			ErrFileSetReferenceNamespaceMismatch,
			evidence.namespace,
			canonicalNamespace,
		)
	}
	if err := ctx.Err(); err != nil {
		return ReferenceEvidenceView{}, err
	}
	return ReferenceEvidenceView{
		namespace:      evidence.namespace,
		markerRevision: hashBytes(markerBytes),
		beforeExists:   evidence.beforeExists,
		before:         append([]byte(nil), evidence.before...),
		after:          append([]byte(nil), evidence.after...),
		afterHash:      evidence.afterHash,
	}, nil
}

func canonicalReferenceNamespaceBounded(
	path string,
	maximumPhysicalDepth int,
	physicalWorkBudget PhysicalWorkBudget,
) (string, error) {
	if path == "" {
		return "", fmt.Errorf("file-set reference namespace is required")
	}
	if maximumPhysicalDepth <= 0 || physicalWorkBudget == nil {
		return "", fmt.Errorf("bounded reference namespace requires a path depth and shared work budget")
	}
	canonical, err := mutation.CanonicalDirectoryEntryPathBounded(path, maximumPhysicalDepth, physicalWorkBudget)
	if err != nil {
		return "", fmt.Errorf("canonicalize file-set reference namespace: %w", err)
	}
	return canonical, nil
}
