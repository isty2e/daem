package journal

import (
	"context"
	"errors"
	"fmt"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func captureDocumentRemoval(ctx context.Context, namespace journalnamespace.DocumentNamespace, comparison journaldocument.Comparison, relations []aggregate.DocumentAddress, budget *physicalwork.Budget) (*recovery.DocumentRemoval, error) {
	demands, err := comparison.RemovalDemands(ctx, relations)
	if err != nil || demands.Len() == 0 {
		return nil, err
	}
	aliases, err := journaldocument.AliasSet(ctx, namespace.Addresses())
	if err != nil {
		return nil, err
	}
	for _, relation := range relations {
		if _, present := aliases[relation]; !present {
			return nil, fmt.Errorf("actual removal writer has no qualified document alias")
		}
	}
	if err := budget.AdmitObservation(); err != nil {
		return nil, err
	}
	writer, err := namespace.WriterPath()
	if err != nil {
		return nil, err
	}
	cleanup, physical, err := captureRemovalNamespace(ctx, writer, make(map[string]struct{}), budget)
	if err != nil {
		return nil, err
	}
	if physical != writer {
		return nil, fmt.Errorf("document removal namespace changed its physical writer")
	}
	capability, err := namespace.AcquireReferent(ctx, budget)
	if err != nil {
		return nil, err
	}
	if err := capability.Close(); err != nil {
		return nil, err
	}
	observed, err := namespace.ObserveRemovalNamespace(ctx, cleanup, budget)
	if err != nil || observed.Status() != recovery.RemovalNamespaceMatched {
		return nil, errors.Join(err, fmt.Errorf("document removal namespace no longer matches the writer"))
	}
	if err := reserveRemovalIntentNamespacePathCapacity(budget, writer, cleanup); err != nil {
		return nil, err
	}
	removal, err := recovery.NewDocumentRemoval(demands, cleanup)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &removal, nil
}
