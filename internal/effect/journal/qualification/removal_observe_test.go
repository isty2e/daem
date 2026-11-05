//go:build darwin || linux

package qualification

import (
	"context"
	"fmt"
	"testing"
)

func TestRemovalObservationCancellationPreservesBoundaryCause(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		wrapped := fmt.Errorf("storage observation interrupted: %w", cause)
		if got := removalObservationCancellation(wrapped); got != wrapped {
			t.Fatalf("cancellation translation = %v, want original %v", got, wrapped)
		}
	}
}
