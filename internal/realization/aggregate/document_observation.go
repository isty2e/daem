package aggregate

import (
	"fmt"

	"github.com/isty2e/daem/internal/supply/artifact"
)

// DocumentObservation binds selected codec state to the non-executable content
// hash of the same existing document. It grants no live-file or write authority.
type DocumentObservation struct {
	snapshot    Snapshot
	contentHash artifact.ContentHash
}

// ObserveDocument reads and hashes one immutable document through the pure codec
// contract; callers cannot supply a separate snapshot or comparison result.
func ObserveDocument(codec Codec, document Document, selection Selection) (DocumentObservation, error) {
	if codec == nil {
		return DocumentObservation{}, fmt.Errorf("document observation codec is required")
	}
	if err := document.Validate(); err != nil {
		return DocumentObservation{}, err
	}
	if !document.Exists() {
		return DocumentObservation{}, fmt.Errorf("document observation requires an existing document")
	}
	canonicalSelection, err := NewSelection(selection.Contracts())
	if err != nil {
		return DocumentObservation{}, err
	}
	if canonicalSelection.CodecContractID() != codec.ContractID() {
		return DocumentObservation{}, fmt.Errorf("document observation codec differs from selection")
	}
	maximumBytes := codec.MaximumDocumentBytes()
	if maximumBytes <= 0 || int64(len(document.content)) > maximumBytes {
		return DocumentObservation{}, fmt.Errorf("document observation exceeds codec byte policy")
	}
	snapshot, failure := codec.Read(document, canonicalSelection)
	if failure != nil {
		return DocumentObservation{}, failure
	}
	if !snapshot.DocumentExisted() {
		return DocumentObservation{}, fmt.Errorf("document observation snapshot has different presence")
	}
	canonicalSnapshot, err := NewSnapshot(true, canonicalSelection, snapshot.States())
	if err != nil {
		return DocumentObservation{}, fmt.Errorf("document observation snapshot: %w", err)
	}
	return DocumentObservation{
		snapshot: canonicalSnapshot, contentHash: artifact.HashFileContent(document.content),
	}, nil
}

func (observation DocumentObservation) Validate() error {
	if _, err := observation.snapshot.Selection(); err != nil {
		return fmt.Errorf("document observation snapshot: %w", err)
	}
	if !observation.snapshot.DocumentExisted() {
		return fmt.Errorf("document observation requires an existing document")
	}
	return observation.contentHash.Validate()
}

func (observation DocumentObservation) Snapshot() Snapshot {
	return cloneSnapshot(observation.snapshot)
}

func (observation DocumentObservation) ContentHash() artifact.ContentHash {
	return observation.contentHash
}
