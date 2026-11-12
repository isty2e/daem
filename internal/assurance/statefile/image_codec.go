package statefile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	pathreferencecodec "github.com/isty2e/daem/internal/encoding/pathreference"
)

const (
	locatedSnapshotVersion      = 11
	stateImageFactSchemaVersion = snapshotVersion
)

type stateImageRecord struct {
	snapshotDTO
	StatefileReference pathreferencecodec.Record `json:"statefile_reference"`
}

// MarshalImage encodes version11 without changing the default State format.
func MarshalImage(image StateImage) ([]byte, error) {
	codec, err := currentSnapshotCodec(context.Background())
	if err != nil {
		return nil, err
	}
	return marshalImage(image, codec)
}

func marshalImage(image StateImage, codec snapshotCodec) ([]byte, error) {
	if err := image.Validate(); err != nil {
		return nil, err
	}
	if err := codec.validateSnapshotForPersistence(image.Snapshot()); err != nil {
		return nil, err
	}
	reference, err := pathreferencecodec.Encode(image.Reference())
	if err != nil {
		return nil, err
	}
	record := stateImageRecord{snapshotDTO: persistedSnapshotAtVersion(image.Snapshot(), locatedSnapshotVersion), StatefileReference: reference}
	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maximumStatefileBytes {
		return nil, fmt.Errorf("statefile exceeds %d bytes", maximumStatefileBytes)
	}
	return content, nil
}

// UnmarshalImage admits version11 only; missing references never become key lookups.
func UnmarshalImage(content []byte) (StateImage, error) {
	codec, err := currentSnapshotCodec(context.Background())
	if err != nil {
		return StateImage{}, err
	}
	return unmarshalImage(content, codec)
}

func unmarshalImage(content []byte, codec snapshotCodec) (StateImage, error) {
	record, err := decodeStateImageRecord(content)
	if err != nil {
		return StateImage{}, err
	}
	reference, err := pathreferencecodec.Decode(record.StatefileReference)
	if err != nil {
		return StateImage{}, fmt.Errorf("statefile reference: %w", err)
	}
	// The envelope adds a reference; snapshot facts retain version 10 semantics.
	snapshot, err := record.snapshotDTO.canonical(stateImageFactSchemaVersion, codec)
	if err != nil {
		return StateImage{}, err
	}
	return NewStateImage(snapshot, reference)
}

func decodeStateImageRecord(content []byte) (stateImageRecord, error) {
	version, err := statefileDocumentVersion(content)
	if err != nil {
		return stateImageRecord{}, err
	}
	if version != locatedSnapshotVersion {
		return stateImageRecord{}, fmt.Errorf("statefile image requires version %d, got %d", locatedSnapshotVersion, version)
	}
	var record stateImageRecord
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return stateImageRecord{}, err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == nil {
		return stateImageRecord{}, fmt.Errorf("statefile contains multiple JSON values")
	} else if err != io.EOF {
		return stateImageRecord{}, err
	}
	return record, nil
}

func stateImageReference(content []byte) (pathauthority.Reference, error) {
	version, err := statefileDocumentVersion(content)
	if err != nil {
		return pathauthority.Reference{}, err
	}
	if version != locatedSnapshotVersion {
		return pathauthority.Reference{}, nil
	}
	record, err := decodeStateImageRecord(content)
	if err != nil {
		return pathauthority.Reference{}, err
	}
	return pathreferencecodec.Decode(record.StatefileReference)
}
