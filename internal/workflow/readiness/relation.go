package readiness

import (
	"context"

	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
)

func resolveCarrierObservations(
	ctx context.Context,
	input relationhost.Input,
	explicit *relationobserve.Batch,
) (relationobserve.Batch, error) {
	if explicit != nil {
		return explicit.Clone(), nil
	}
	return relationhost.Observe(ctx, input)
}
