package fileset

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

const publicationIntentFile = "intent.json"

type publicationMarker struct {
	intentSize int64
	intentHash string
}

type publicationIntentDescriptorRecord struct {
	Size *int64  `json:"size"`
	Hash *string `json:"hash"`
}

type publicationMarkerRecord struct {
	Version int                                `json:"version"`
	Intent  *publicationIntentDescriptorRecord `json:"intent"`
}

func encodePublicationMarker(intentBytes []byte) ([]byte, error) {
	if err := validatePublicationIntentSize(int64(len(intentBytes))); err != nil {
		return nil, err
	}
	marker := publicationMarker{intentSize: int64(len(intentBytes)), intentHash: hashBytes(intentBytes)}
	if err := marker.validate(); err != nil {
		return nil, err
	}
	size, digest := marker.intentSize, marker.intentHash
	content, err := json.Marshal(publicationMarkerRecord{
		Version: contractversion.MetadataTransactionIntent,
		Intent:  &publicationIntentDescriptorRecord{Size: &size, Hash: &digest},
	})
	if err != nil {
		return nil, fmt.Errorf("encode publication marker: %w", err)
	}
	if len(content) > maximumMarkerBytes {
		return nil, fmt.Errorf("publication marker exceeds marker byte limit")
	}
	return content, nil
}

func (marker publicationMarker) validate() error {
	if err := validatePublicationIntentSize(marker.intentSize); err != nil {
		return err
	}
	if !isPublicationHash(marker.intentHash) {
		return fmt.Errorf("publication marker requires a canonical intent digest")
	}
	return nil
}

func validatePublicationIntentSize(size int64) error {
	if size <= 0 || size > maximumTargetBytes {
		return fmt.Errorf("publication intent size is outside retained image bounds")
	}
	return nil
}

func decodePublicationMarker(ctx context.Context, content []byte) (publicationMarker, error) {
	if ctx == nil {
		return publicationMarker{}, fmt.Errorf("publication marker context is required")
	}
	if err := ctx.Err(); err != nil {
		return publicationMarker{}, err
	}
	if len(content) > maximumMarkerBytes {
		return publicationMarker{}, fmt.Errorf("publication marker exceeds marker byte limit")
	}
	envelope, err := jsonstrict.DecodeVersionEnvelope(content, "publication marker", maximumMarkerJSONDepth, contractversion.MetadataTransactionIntent)
	if canceled := ctx.Err(); canceled != nil {
		return publicationMarker{}, canceled
	}
	if err != nil {
		return publicationMarker{}, err
	}
	if envelope.Disposition != jsonstrict.VersionCurrent {
		return publicationMarker{}, fmt.Errorf("publication marker version %d requires its matching daem writer", envelope.Version)
	}
	marker, err := decodePublicationMarkerRecord(content)
	if canceled := ctx.Err(); canceled != nil {
		return publicationMarker{}, canceled
	}
	return marker, err
}

// decodePublicationMarkerRecord consumes an already syntax/version-validated
// envelope, allowing the global observer to dispatch once on its version.
func decodePublicationMarkerRecord(content []byte) (publicationMarker, error) {
	var record publicationMarkerRecord
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return publicationMarker{}, fmt.Errorf("decode publication marker: %w", err)
	}
	if record.Version != contractversion.MetadataTransactionIntent || record.Intent == nil ||
		record.Intent.Size == nil || record.Intent.Hash == nil {
		return publicationMarker{}, fmt.Errorf("publication marker requires version and sized intent digest")
	}
	marker := publicationMarker{intentSize: *record.Intent.Size, intentHash: *record.Intent.Hash}
	if err := marker.validate(); err != nil {
		return publicationMarker{}, err
	}
	return marker, nil
}

func loadPublicationFenceMarker(ctx context.Context, path string) error {
	content, err := readMarkerContent(ctx, path)
	if err != nil {
		return err
	}
	envelope, err := jsonstrict.DecodeVersionEnvelope(content, "file-set transaction marker", maximumMarkerJSONDepth, contractversion.MetadataTransactionIntent)
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	if err != nil {
		return fmt.Errorf("parse file-set transaction marker %q: %w", path, err)
	}
	switch envelope.Version {
	case contractversion.MetadataTransaction:
		_, err = decodeValidatedMarkerContent(path, content, contractversion.MetadataTransaction, 0, nil)
	case contractversion.MetadataTransactionIntent:
		_, err = decodePublicationMarkerRecord(content)
	default:
		err = markerVersionError(envelope)
	}
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	return err
}

func (marker publicationMarker) decodeIntent(ctx context.Context, content []byte) (PublicationIntent, error) {
	if ctx == nil {
		return PublicationIntent{}, fmt.Errorf("publication intent context is required")
	}
	if err := ctx.Err(); err != nil {
		return PublicationIntent{}, err
	}
	if err := marker.validate(); err != nil {
		return PublicationIntent{}, err
	}
	if int64(len(content)) != marker.intentSize {
		return PublicationIntent{}, fmt.Errorf("publication intent size does not match marker")
	}
	digest := hashBytes(content)
	if err := ctx.Err(); err != nil {
		return PublicationIntent{}, err
	}
	if digest != marker.intentHash {
		return PublicationIntent{}, fmt.Errorf("publication intent digest does not match marker")
	}
	return decodePublicationIntent(ctx, content)
}
