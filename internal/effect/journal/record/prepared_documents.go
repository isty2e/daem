package record

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
)

// A prepared result couples complete comparisons with their rewritten image
// references. It is still neither published bytes nor execution authority.
type PreparedDocuments struct {
	journal     Native
	documents   LoadedDocuments
	initialized bool
}

func (prepared PreparedDocuments) Encode(ctx context.Context, codec durable.SnapshotCodec) ([]byte, error) {
	if !prepared.initialized {
		return nil, fmt.Errorf("prepared document record is required")
	}
	return prepared.journal.encode(ctx, codec)
}

func (prepared PreparedDocuments) Comparisons() ([]journaldocument.Comparison, error) {
	if !prepared.initialized {
		return nil, fmt.Errorf("prepared document record is required")
	}
	comparisons := make([]journaldocument.Comparison, len(prepared.documents.groups))
	for index, group := range prepared.documents.groups {
		comparisons[index] = group.Comparison()
	}
	return comparisons, nil
}

func (session *LoadedReconstruction) FinishLoaded(ctx context.Context) (LoadedDocuments, error) {
	if session == nil || session.mode != documentReconstructionLoaded {
		return LoadedDocuments{}, session.reject(fmt.Errorf("loaded document completion requires its selected image source"))
	}
	return session.complete(ctx)
}

func (session *LoadedReconstruction) finishPrepared(ctx context.Context) (PreparedDocuments, error) {
	if session == nil || session.mode != documentReconstructionPrepared {
		return PreparedDocuments{}, session.reject(fmt.Errorf("prepared document completion requires its selected image source"))
	}
	completed, err := session.complete(ctx)
	if err != nil {
		return PreparedDocuments{}, err
	}

	journal := session.journal
	journal.record.DocumentGroups = slices.Clone(journal.record.DocumentGroups)
	for index, group := range completed.groups {
		if err := ctx.Err(); err != nil {
			return PreparedDocuments{}, session.reject(err)
		}
		_, original, expected, err := documentImageReferences(group.Comparison(), index)
		if err != nil {
			return PreparedDocuments{}, session.reject(err)
		}
		journal.record.DocumentGroups[index].Original, journal.record.DocumentGroups[index].Expected = original, expected
	}
	return PreparedDocuments{journal: journal, documents: completed, initialized: true}, nil
}
