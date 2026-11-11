package statefile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

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
	if err := image.Validate(); err != nil {
		return nil, err
	}
	codec, err := currentSnapshotCodec(context.Background())
	if err != nil {
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
	version, err := statefileDocumentVersion(content)
	if err != nil {
		return StateImage{}, err
	}
	if version != locatedSnapshotVersion {
		return StateImage{}, fmt.Errorf("statefile image requires version %d, got %d", locatedSnapshotVersion, version)
	}
	var record stateImageRecord
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return StateImage{}, err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == nil {
		return StateImage{}, fmt.Errorf("statefile contains multiple JSON values")
	} else if err != io.EOF {
		return StateImage{}, err
	}
	reference, err := pathreferencecodec.Decode(record.StatefileReference)
	if err != nil {
		return StateImage{}, fmt.Errorf("statefile reference: %w", err)
	}
	codec, err := currentSnapshotCodec(context.Background())
	if err != nil {
		return StateImage{}, err
	}
	// The envelope adds a reference; snapshot facts retain version 10 semantics.
	snapshot, err := record.snapshotDTO.canonical(stateImageFactSchemaVersion, codec)
	if err != nil {
		return StateImage{}, err
	}
	return NewStateImage(snapshot, reference)
}
