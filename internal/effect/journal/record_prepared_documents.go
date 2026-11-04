package journal

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
)

// A prepared result couples complete comparisons with their rewritten image
// references. It is still neither published bytes nor execution authority.
type preparedDocumentRecord struct {
	journal     nativeJournalSnapshots
	documents   nativeDocumentRecords
	initialized bool
}

func (prepared preparedDocumentRecord) encode(ctx context.Context, codec durable.SnapshotCodec) ([]byte, error) {
	if !prepared.initialized {
		return nil, fmt.Errorf("prepared document record is required")
	}
	return marshalNativeJournalRecord(ctx, prepared.journal.record, prepared.journal.before, prepared.journal.after, codec)
}

func (session *documentReconstructionSession) finishLoaded(ctx context.Context) (nativeDocumentRecords, error) {
	if session == nil || session.mode != documentReconstructionLoaded {
		return nativeDocumentRecords{}, session.reject(fmt.Errorf("loaded document completion requires its selected image source"))
	}
	return session.complete(ctx)
}

func (session *documentReconstructionSession) finishPrepared(ctx context.Context) (preparedDocumentRecord, error) {
	if session == nil || session.mode != documentReconstructionPrepared {
		return preparedDocumentRecord{}, session.reject(fmt.Errorf("prepared document completion requires its selected image source"))
	}
	completed, err := session.complete(ctx)
	if err != nil {
		return preparedDocumentRecord{}, err
	}

	journal := session.journal
	journal.record.DocumentGroups = slices.Clone(journal.record.DocumentGroups)
	for index, group := range completed.groups {
		if err := ctx.Err(); err != nil {
			return preparedDocumentRecord{}, session.reject(err)
		}
		_, original, expected, err := documentImageReferences(group.Comparison(), index)
		if err != nil {
			return preparedDocumentRecord{}, session.reject(err)
		}
		journal.record.DocumentGroups[index].Original, journal.record.DocumentGroups[index].Expected = original, expected
	}
	return preparedDocumentRecord{journal: journal, documents: completed, initialized: true}, nil
}
