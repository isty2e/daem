package compose

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// ReadBaselines reconstructs every participant through its own codec from one
// loaded native document. It neither reads a backup path nor grants authority
// to restore the document. Failure returns no partial baseline group.
func ReadBaselines(ctx context.Context, document aggregate.Document, limits configdocument.Limits, participants []Participant) ([]ProjectionBaseline, error) {
	if _, err := prepare(ctx, document, limits, participants); err != nil {
		return nil, err
	}
	baselines := make([]ProjectionBaseline, 0, len(participants))
	for index, participant := range participants {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		observed, failure := participant.codec.Read(document, participant.selection)
		if failure != nil {
			return nil, failure
		}
		if observed.DocumentExisted() != document.Exists() {
			return nil, fmt.Errorf("recovery participant[%d] observation has different document presence", index)
		}
		snapshot, err := aggregate.NewSnapshot(document.Exists(), participant.selection, observed.States())
		if err != nil {
			return nil, fmt.Errorf("recovery participant[%d] observation: %w", index, err)
		}
		baseline, err := NewProjectionBaseline(participant.codec, snapshot, participant.syntax, participant.areas)
		if err != nil {
			return nil, err
		}
		baselines = append(baselines, baseline)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return baselines, nil
}
