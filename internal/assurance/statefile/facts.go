package statefile

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
)

// DecodePersistedFacts admits the same snapshot documents as Load, including
// exact empty retirement records, without observing a statefile namespace.
// Entry admission and fresh owner qualification remain the caller's work.
func DecodePersistedFacts(ctx context.Context, content []byte) (durable.Snapshot, error) {
	if ctx == nil {
		return durable.Snapshot{}, fmt.Errorf("statefile facts context is required")
	}
	if err := ctx.Err(); err != nil {
		return durable.Snapshot{}, err
	}
	snapshot, err := decodePersisted(ctx, content)
	if cancellation := ctx.Err(); cancellation != nil {
		return durable.Snapshot{}, cancellation
	}
	return snapshot, err
}
