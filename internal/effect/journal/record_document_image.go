package journal

import (
	"context"
	"fmt"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/encoding/configdocument"
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

type recordedDocumentImage struct {
	kind      documentImageKind
	reference string
	size      int64
	digest    artifact.ContentHash
	hostMode  recovery.PermissionMode
}

type documentImageKind uint8

const (
	documentImageAbsent documentImageKind = iota + 1
	documentImageFile
)

func documentImageReference(record recoveryDocumentImage) (recordedDocumentImage, error) {
	if err := record.validate(); err != nil {
		return recordedDocumentImage{}, err
	}
	if record.Kind == "absent" {
		return recordedDocumentImage{kind: documentImageAbsent}, nil
	}
	return recordedDocumentImage{kind: documentImageFile, reference: record.Artifact.Path, size: *record.Artifact.Size, digest: record.Artifact.Digest, hostMode: *record.Mode}, nil
}

func (image recordedDocumentImage) validate() error {
	if image.kind != documentImageAbsent && image.kind != documentImageFile {
		return fmt.Errorf("recorded document image is required")
	}
	return nil
}

func recordedDocumentImageReferences(comparison journaldocument.Comparison, index int) (string, recordedDocumentImage, recordedDocumentImage, error) {
	directory, original, expected, err := documentImageReferences(comparison, index)
	if err != nil {
		return "", recordedDocumentImage{}, recordedDocumentImage{}, err
	}
	before, err := documentImageReference(original)
	if err != nil {
		return "", recordedDocumentImage{}, recordedDocumentImage{}, err
	}
	after, err := documentImageReference(expected)
	if err != nil {
		return "", recordedDocumentImage{}, recordedDocumentImage{}, err
	}
	return directory, before, after, nil
}

func (image recordedDocumentImage) readPlan(maximumBytes int64) (recordedDocumentImage, error) {
	if err := image.validate(); err != nil {
		return recordedDocumentImage{}, err
	}
	if maximumBytes <= 0 || maximumBytes > recovery.MaximumRecoveryBackupFileBytes {
		return recordedDocumentImage{}, fmt.Errorf("document image byte limit is outside the backup envelope")
	}
	return image, nil
}

func (image recordedDocumentImage) fileReference() (string, error) {
	if err := image.validate(); err != nil {
		return "", err
	}
	if !image.Exists() {
		return "", fmt.Errorf("document image has no private file reference")
	}
	return image.reference, nil
}

func (record recoveryDocumentImage) preparedImage(ctx context.Context, document aggregate.Document, mode *recovery.PermissionMode, limits configdocument.Limits) (loadedDocumentImage, error) {
	if err := journaldocument.ValidateImage(ctx, document, limits); err != nil {
		return loadedDocumentImage{}, err
	}
	image := loadedDocumentImage{document: document, mode: clonePermissionMode(mode), initialized: true}
	if err := image.validate(); err != nil {
		return loadedDocumentImage{}, err
	}
	if err := record.validate(); err != nil {
		return loadedDocumentImage{}, err
	}
	if document.Exists() != (record.Kind == "file") {
		return loadedDocumentImage{}, fmt.Errorf("prepared document presence differs from its recorded witness")
	}
	if document.Exists() && (*image.mode != *record.Mode || document.ContentSize() != *record.Artifact.Size || artifact.HashFileContent(document.Content()) != record.Artifact.Digest) {
		return loadedDocumentImage{}, fmt.Errorf("prepared document mode, size or digest differs from its recorded witness")
	}
	return image, nil
}

func (record recoveryDocumentImage) identity() (journaldocument.ImageIdentity, error) {
	digest := artifact.ContentHash("")
	if record.Kind == "file" {
		digest = record.Artifact.Digest
	}
	return journaldocument.NewImageIdentity(record.Kind == "file", digest, record.Mode)
}

func (image recordedDocumentImage) Exists() bool { return image.kind == documentImageFile }

func (image recordedDocumentImage) Reference() string { return image.reference }

func (image recordedDocumentImage) Size() int64 { return image.size }

func (image recordedDocumentImage) absentImage() (loadedDocumentImage, error) {
	if image.kind != documentImageAbsent {
		return loadedDocumentImage{}, fmt.Errorf("absent recorded document image is required")
	}
	return loadedDocumentImage{document: aggregate.AbsentDocument(), initialized: true}, nil
}

func (image recordedDocumentImage) loadedImage(content []byte, privateMode recovery.PermissionMode) (loadedDocumentImage, error) {
	if image.kind != documentImageFile {
		return loadedDocumentImage{}, fmt.Errorf("file recorded document image is required")
	}
	if int64(len(content)) != image.size || privateMode != recovery.PermissionMode(recoveryJournalMode) || artifact.HashFileContent(content) != image.digest {
		return loadedDocumentImage{}, fmt.Errorf("document image size, private mode or digest changed")
	}
	mode := image.hostMode
	return loadedDocumentImage{document: aggregate.ExistingDocument(content), mode: &mode, initialized: true}, nil
}

func loadedDocumentImagePair(original, expected loadedDocumentImage) (loadedDocumentImages, error) {
	if err := original.validate(); err != nil {
		return loadedDocumentImages{}, err
	}
	if err := expected.validate(); err != nil {
		return loadedDocumentImages{}, err
	}
	return loadedDocumentImages{original: original, expected: expected}, nil
}

func (image loadedDocumentImage) Document() aggregate.Document { return image.document }

func (image loadedDocumentImage) Mode() *recovery.PermissionMode {
	return clonePermissionMode(image.mode)
}

func (images loadedDocumentImages) Original() loadedDocumentImage { return images.original }

func (images loadedDocumentImages) Expected() loadedDocumentImage { return images.expected }

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

func documentImageReferences(comparison journaldocument.Comparison, groupIndex int) (string, recoveryDocumentImage, recoveryDocumentImage, error) {
	if groupIndex < 0 {
		return "", recoveryDocumentImage{}, recoveryDocumentImage{}, fmt.Errorf("document image ordinal must be nonnegative")
	}
	directory := fmt.Sprintf("document-%06d", groupIndex)
	original, err := stagedDocumentImageRecord(comparison.BeforeDocument(), comparison.BeforeState(), directory+"/original")
	if err != nil {
		return "", recoveryDocumentImage{}, recoveryDocumentImage{}, err
	}
	expected, err := stagedDocumentImageRecord(comparison.ExpectedDocument(), comparison.ExpectedState(), directory+"/expected")
	if err != nil {
		return "", recoveryDocumentImage{}, recoveryDocumentImage{}, err
	}
	return directory, original, expected, nil
}

func stagedDocumentImageRecord(document aggregate.Document, state recovery.PartialDocumentState, path string) (recoveryDocumentImage, error) {
	var mode *recovery.PermissionMode
	if value, present := state.RegularFileMode(); present {
		mode = &value
	}
	if !document.Exists() {
		path = ""
	}
	return persistedDocumentImage(document, mode, path)
}
