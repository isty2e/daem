//go:build !darwin && !linux

package access

import (
	"errors"
	"testing"
)

func TestNoFollowTraversalUnavailableIsTyped(t *testing.T) {
	if !errors.Is(unavailableTraversal(), ErrNoFollowTraversalUnavailable) {
		t.Fatalf("adapter error = %v, want typed no-follow capability-unavailable outcome", unavailableTraversal())
	}
}
