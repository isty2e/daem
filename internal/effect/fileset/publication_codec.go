package fileset

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

type publicationImageRecord struct {
	Exists *bool   `json:"exists"`
	Size   *int64  `json:"size"`
	Hash   *string `json:"hash"`
	Mode   *uint32 `json:"mode"`
}

type publicationTargetRecord struct {
	Path   string                  `json:"path"`
	Before *publicationImageRecord `json:"before"`
	After  *publicationImageRecord `json:"after"`
}

type publicationIntentRecord struct {
	Version   int                       `json:"version"`
	Selection []byte                    `json:"selection"`
	Targets   []publicationTargetRecord `json:"targets"`
}

func encodePublicationIntent(intent PublicationIntent) ([]byte, error) {
	if err := intent.Validate(); err != nil {
		return nil, err
	}
	record := publicationIntentRecord{
		Version:   contractversion.MetadataTransactionIntent,
		Selection: intent.selection,
		Targets:   make([]publicationTargetRecord, len(intent.targets)),
	}
	for index, target := range intent.targets {
		record.Targets[index] = publicationTargetRecord{
			Path: target.path, Before: publicationRecordForImage(target.before), After: publicationRecordForImage(target.after),
		}
	}
	content, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode publication intent: %w", err)
	}
	if int64(len(content)) > maximumTargetBytes {
		return nil, fmt.Errorf("publication intent exceeds retained image limit")
	}
	return content, nil
}

func publicationRecordForImage(image PublicationImage) *publicationImageRecord {
	exists, size, digest, mode := image.exists, image.size, image.hash, uint32(image.mode)
	return &publicationImageRecord{Exists: &exists, Size: &size, Hash: &digest, Mode: &mode}
}

func decodePublicationIntent(ctx context.Context, content []byte) (PublicationIntent, error) {
	if ctx == nil {
		return PublicationIntent{}, fmt.Errorf("publication intent context is required")
	}
	if err := ctx.Err(); err != nil {
		return PublicationIntent{}, err
	}
	if int64(len(content)) > maximumTargetBytes {
		return PublicationIntent{}, fmt.Errorf("publication intent exceeds retained image limit")
	}
	envelope, err := jsonstrict.DecodeVersionEnvelope(content, "publication intent", maximumMarkerJSONDepth, contractversion.MetadataTransactionIntent)
	if canceled := ctx.Err(); canceled != nil {
		return PublicationIntent{}, canceled
	}
	if err != nil {
		return PublicationIntent{}, err
	}
	if envelope.Disposition != jsonstrict.VersionCurrent {
		return PublicationIntent{}, fmt.Errorf("publication intent version %d requires its matching daem writer", envelope.Version)
	}
	var record publicationIntentRecord
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&record)
	if canceled := ctx.Err(); canceled != nil {
		return PublicationIntent{}, canceled
	}
	if err != nil {
		return PublicationIntent{}, fmt.Errorf("decode publication intent: %w", err)
	}

	targets := make([]PublicationTarget, len(record.Targets))
	for index, target := range record.Targets {
		if err := ctx.Err(); err != nil {
			return PublicationIntent{}, err
		}
		before, err := publicationImageFromRecord(target.Before)
		if err != nil {
			return PublicationIntent{}, fmt.Errorf("publication target[%d] Before: %w", index, err)
		}
		after, err := publicationImageFromRecord(target.After)
		if err != nil {
			return PublicationIntent{}, fmt.Errorf("publication target[%d] After: %w", index, err)
		}
		targets[index], err = NewPublicationTarget(target.Path, before, after)
		if err != nil {
			return PublicationIntent{}, fmt.Errorf("publication target[%d]: %w", index, err)
		}
	}
	intent, err := NewPublicationIntent(record.Selection, targets)
	if canceled := ctx.Err(); canceled != nil {
		return PublicationIntent{}, canceled
	}
	return intent, err
}

func publicationImageFromRecord(record *publicationImageRecord) (PublicationImage, error) {
	if record == nil || record.Exists == nil || record.Size == nil || record.Hash == nil || record.Mode == nil {
		return PublicationImage{}, fmt.Errorf("publication image requires existence, size, hash and mode")
	}
	return publicationImageFromFacts(*record.Exists, *record.Size, *record.Hash, fs.FileMode(*record.Mode))
}
