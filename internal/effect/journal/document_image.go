package journal

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
)

type recoveryDocumentImage struct {
	Kind     string                    `json:"kind"`
	Mode     *recovery.PermissionMode  `json:"mode,omitempty"`
	Artifact *recoveryDocumentArtifact `json:"artifact,omitempty"`
}

type recoveryDocumentArtifact struct {
	Path   string               `json:"path"`
	Size   *int64               `json:"size"`
	Digest artifact.ContentHash `json:"digest"`
}

// Loaded bytes and recorded host mode are not active-journal authority or
// permission to restore unselected bytes onto a host document.
type loadedDocumentImage struct {
	document    aggregate.Document
	mode        *recovery.PermissionMode
	initialized bool
}

type loadedDocumentImages struct {
	original loadedDocumentImage
	expected loadedDocumentImage
}

func loadDocumentImages(ctx context.Context, original, expected recoveryDocumentImage, root *rootedpath.CapturedRoot, filesystem mutationfs.RootedReader, budget *recovery.PhysicalWorkBudget, maximumBytes int64) (loadedDocumentImages, error) {
	before, err := loadDocumentImage(ctx, original, root, filesystem, budget, maximumBytes)
	if err != nil {
		return loadedDocumentImages{}, err
	}
	after, err := loadDocumentImage(ctx, expected, root, filesystem, budget, maximumBytes)
	if err != nil {
		return loadedDocumentImages{}, err
	}
	return loadedDocumentImages{original: before, expected: after}, nil
}

func (image loadedDocumentImage) validate() error {
	if !image.initialized {
		return fmt.Errorf("loaded document image is required")
	}
	if err := image.document.Validate(); err != nil {
		return err
	}
	_, err := recovery.NewPartialDocumentState(image.document.Exists(), image.mode, nil)
	return err
}

func (record *recoveryDocumentImage) UnmarshalJSON(content []byte) error {
	if record == nil {
		return fmt.Errorf("document image destination is nil")
	}
	if err := requireDocumentEvidenceFields(content, "kind"); err != nil {
		return err
	}
	type wire recoveryDocumentImage
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	candidate := recoveryDocumentImage(decoded)
	if err := candidate.validate(); err != nil {
		return err
	}
	*record = candidate
	return nil
}

func (record *recoveryDocumentArtifact) UnmarshalJSON(content []byte) error {
	if record == nil {
		return fmt.Errorf("document artifact destination is nil")
	}
	if err := requireDocumentEvidenceFields(content, "path", "size", "digest"); err != nil {
		return err
	}
	type wire recoveryDocumentArtifact
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	candidate := recoveryDocumentArtifact(decoded)
	if err := candidate.validate(); err != nil {
		return err
	}
	*record = candidate
	return nil
}

func (record recoveryDocumentImage) validate() error {
	switch record.Kind {
	case "absent":
		if record.Mode != nil || record.Artifact != nil {
			return fmt.Errorf("absent document image carries file evidence")
		}
		return nil
	case "file":
		if record.Mode == nil || record.Artifact == nil {
			return fmt.Errorf("file document image requires mode and artifact")
		}
		if _, err := recovery.NewPartialDocumentState(true, record.Mode, nil); err != nil {
			return err
		}
		return record.Artifact.validate()
	default:
		return fmt.Errorf("unsupported document image kind")
	}
}

func (record recoveryDocumentArtifact) validate() error {
	if _, err := rootedpath.NewRelativeDestination(record.Path); err != nil {
		return fmt.Errorf("document artifact relative path: %w", err)
	}
	if record.Size == nil || *record.Size < 0 || *record.Size > recovery.MaximumRecoveryBackupFileBytes {
		return fmt.Errorf("document artifact size is outside the backup envelope")
	}
	return record.Digest.Validate()
}

func persistedDocumentImage(document aggregate.Document, mode *recovery.PermissionMode, relativePath string) (recoveryDocumentImage, error) {
	if err := document.Validate(); err != nil {
		return recoveryDocumentImage{}, err
	}
	if _, err := recovery.NewPartialDocumentState(document.Exists(), mode, nil); err != nil {
		return recoveryDocumentImage{}, err
	}
	if !document.Exists() {
		if relativePath != "" {
			return recoveryDocumentImage{}, fmt.Errorf("absent document image has an artifact path")
		}
		return recoveryDocumentImage{Kind: "absent"}, nil
	}
	size := document.ContentSize()
	if size > recovery.MaximumRecoveryBackupFileBytes {
		return recoveryDocumentImage{}, fmt.Errorf("document image exceeds the backup byte envelope")
	}
	record := recoveryDocumentImage{
		Kind: "file", Mode: clonePermissionMode(mode),
		Artifact: &recoveryDocumentArtifact{Path: relativePath, Size: &size, Digest: artifact.HashFileContent(document.Content())},
	}
	if err := record.validate(); err != nil {
		return recoveryDocumentImage{}, err
	}
	return record, nil
}

func loadDocumentImage(ctx context.Context, record recoveryDocumentImage, root *rootedpath.CapturedRoot, filesystem mutationfs.RootedReader, budget *recovery.PhysicalWorkBudget, maximumBytes int64) (loadedDocumentImage, error) {
	if ctx == nil {
		return loadedDocumentImage{}, fmt.Errorf("document image context is required")
	}
	if err := ctx.Err(); err != nil {
		return loadedDocumentImage{}, err
	}
	if err := record.validate(); err != nil {
		return loadedDocumentImage{}, err
	}
	if maximumBytes <= 0 || maximumBytes > recovery.MaximumRecoveryBackupFileBytes {
		return loadedDocumentImage{}, fmt.Errorf("document image byte limit is outside the backup envelope")
	}
	if record.Kind == "absent" {
		return loadedDocumentImage{document: aggregate.AbsentDocument(), initialized: true}, nil
	}
	if root == nil || filesystem == nil || budget == nil {
		return loadedDocumentImage{}, fmt.Errorf("document image root, filesystem and shared budget are required")
	}
	size := *record.Artifact.Size
	if size > maximumBytes || size > budget.RemainingBytes() {
		return loadedDocumentImage{}, fmt.Errorf("document image exceeds the remaining byte envelope")
	}
	rootAuthority, err := root.AuthorityBounded(budget)
	if err != nil {
		return loadedDocumentImage{}, err
	}
	relative, err := rootedpath.NewRelativeDestination(record.Artifact.Path)
	if err != nil {
		return loadedDocumentImage{}, err
	}
	destination, err := rootAuthority.Bind(relative)
	if err != nil {
		return loadedDocumentImage{}, err
	}
	capability, err := root.AcquireBounded(destination, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return loadedDocumentImage{}, err
	}
	content, mode, _, readErr := filesystem.ReadRootedRegularFileUpTo(ctx, capability, size)
	closeErr := capability.Close()
	charged := int64(len(content))
	if readErr != nil {
		charged = size
	}
	work, workErr := recovery.NewArtifactWork(0, charged)
	if workErr == nil {
		workErr = budget.AdmitTree(work)
	}
	if err := errors.Join(readErr, closeErr, workErr); err != nil {
		return loadedDocumentImage{}, err
	}
	if int64(len(content)) != size || mode.Perm() != recoveryJournalMode || artifact.HashFileContent(content) != record.Artifact.Digest {
		return loadedDocumentImage{}, fmt.Errorf("document image size, private mode or digest changed")
	}
	if err := ctx.Err(); err != nil {
		return loadedDocumentImage{}, err
	}
	return loadedDocumentImage{document: aggregate.ExistingDocument(content), mode: clonePermissionMode(record.Mode), initialized: true}, nil
}
