package record

import (
	"context"
	"fmt"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
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
type LoadedImage struct {
	document    aggregate.Document
	mode        *recovery.PermissionMode
	source      DocumentImage
	initialized bool
}

type LoadedImages struct {
	original LoadedImage
	expected LoadedImage
}

type DocumentImage struct {
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

func documentImageReference(record recoveryDocumentImage) (DocumentImage, error) {
	if err := record.validate(); err != nil {
		return DocumentImage{}, err
	}
	if record.Kind == "absent" {
		return DocumentImage{kind: documentImageAbsent}, nil
	}
	return DocumentImage{kind: documentImageFile, reference: record.Artifact.Path, size: *record.Artifact.Size, digest: record.Artifact.Digest, hostMode: *record.Mode}, nil
}

func (image DocumentImage) validate() error {
	if image.kind != documentImageAbsent && image.kind != documentImageFile {
		return fmt.Errorf("recorded document image is required")
	}
	return nil
}

func StagedDocumentImageReferences(comparison journaldocument.Comparison, index int) (string, DocumentImage, DocumentImage, error) {
	directory, original, expected, err := documentImageReferences(comparison, index)
	if err != nil {
		return "", DocumentImage{}, DocumentImage{}, err
	}
	before, err := documentImageReference(original)
	if err != nil {
		return "", DocumentImage{}, DocumentImage{}, err
	}
	after, err := documentImageReference(expected)
	if err != nil {
		return "", DocumentImage{}, DocumentImage{}, err
	}
	return directory, before, after, nil
}

func (image DocumentImage) AdmitRead(maximumBytes int64) (DocumentImage, error) {
	if err := image.validate(); err != nil {
		return DocumentImage{}, err
	}
	if maximumBytes <= 0 || maximumBytes > recovery.MaximumRecoveryBackupFileBytes {
		return DocumentImage{}, fmt.Errorf("document image byte limit is outside the backup envelope")
	}
	return image, nil
}

func (image DocumentImage) FileReference() (string, error) {
	if err := image.validate(); err != nil {
		return "", err
	}
	if !image.Exists() {
		return "", fmt.Errorf("document image has no private file reference")
	}
	return image.reference, nil
}

func (record recoveryDocumentImage) preparedImage(ctx context.Context, document aggregate.Document, mode *recovery.PermissionMode, limits configdocument.Limits) (LoadedImage, error) {
	if err := compose.ValidateImage(ctx, document, limits); err != nil {
		return LoadedImage{}, err
	}
	image := LoadedImage{document: document, mode: clonePermissionMode(mode), initialized: true}
	if err := image.validate(); err != nil {
		return LoadedImage{}, err
	}
	if err := record.validate(); err != nil {
		return LoadedImage{}, err
	}
	if document.Exists() != (record.Kind == "file") {
		return LoadedImage{}, fmt.Errorf("prepared document presence differs from its recorded witness")
	}
	if document.Exists() && (*image.mode != *record.Mode || document.ContentSize() != *record.Artifact.Size || artifact.HashFileContent(document.Content()) != record.Artifact.Digest) {
		return LoadedImage{}, fmt.Errorf("prepared document mode, size or digest differs from its recorded witness")
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

func (image DocumentImage) Exists() bool { return image.kind == documentImageFile }

func (image DocumentImage) Reference() string { return image.reference }

func (image DocumentImage) Size() int64 { return image.size }

func (image DocumentImage) Absent() (LoadedImage, error) {
	if image.kind != documentImageAbsent {
		return LoadedImage{}, fmt.Errorf("absent recorded document image is required")
	}
	return LoadedImage{document: aggregate.AbsentDocument(), source: image, initialized: true}, nil
}

func (image DocumentImage) Load(content []byte, privateMode recovery.PermissionMode) (LoadedImage, error) {
	if image.kind != documentImageFile {
		return LoadedImage{}, fmt.Errorf("file recorded document image is required")
	}
	if int64(len(content)) != image.size || privateMode != recovery.PermissionMode(Mode) || artifact.HashFileContent(content) != image.digest {
		return LoadedImage{}, fmt.Errorf("document image size, private mode or digest changed")
	}
	mode := image.hostMode
	return LoadedImage{document: aggregate.ExistingDocument(content), mode: &mode, source: image, initialized: true}, nil
}

func loadedDocumentImagePair(original, expected LoadedImage) (LoadedImages, error) {
	if err := original.validate(); err != nil {
		return LoadedImages{}, err
	}
	if err := expected.validate(); err != nil {
		return LoadedImages{}, err
	}
	return LoadedImages{original: original, expected: expected}, nil
}

func (image LoadedImage) Document() aggregate.Document { return image.document }

func (image LoadedImage) Mode() *recovery.PermissionMode {
	return clonePermissionMode(image.mode)
}

func (images LoadedImages) Original() LoadedImage { return images.original }

func (images LoadedImages) Expected() LoadedImage { return images.expected }

func (image LoadedImage) validate() error {
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
