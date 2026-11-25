package statefile

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

// DecodePersistedFacts admits the same snapshot documents as Load, including
// exact empty retirement records, without observing a statefile namespace.
// Entry admission and fresh owner qualification remain the caller's work.
func DecodePersistedFacts(ctx context.Context, content []byte) (durable.Snapshot, error) {
	snapshot, _, err := DecodePersisted(ctx, content)
	return snapshot, err
}

// DecodePersisted returns admitted facts and the self-reference, if the physical
// format contains one. It never derives a locator from a comparison identity.
func DecodePersisted(ctx context.Context, content []byte) (durable.Snapshot, pathauthority.Reference, error) {
	if ctx == nil {
		return durable.Snapshot{}, pathauthority.Reference{}, fmt.Errorf("statefile facts context is required")
	}
	if err := ctx.Err(); err != nil {
		return durable.Snapshot{}, pathauthority.Reference{}, err
	}
	snapshot, reference, err := decodePersisted(ctx, content)
	if cancellation := ctx.Err(); cancellation != nil {
		return durable.Snapshot{}, pathauthority.Reference{}, cancellation
	}
	return snapshot, reference, err
}
