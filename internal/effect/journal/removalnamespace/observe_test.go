package removalnamespace

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestRemovalNamespaceCancellationIsNotAuthorityDrift(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		wrapped := fmt.Errorf("namespace provenance interrupted: %w", cause)
		if got := removalNamespaceCancellation(t.Context(), wrapped); got != wrapped {
			t.Fatalf("namespace cancellation = %v, want original %v", got, wrapped)
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := removalNamespaceCancellation(ctx, nil); !errors.Is(got, context.Canceled) {
		t.Fatalf("post-provenance cancellation = %v, want context.Canceled", got)
	}
}
