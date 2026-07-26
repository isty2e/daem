package refresh

import (
	"fmt"
	"time"
)

const (
	DefaultHostCommandTimeout = 10 * time.Minute
	MinimumHostCommandTimeout = time.Second
	MaximumHostCommandTimeout = time.Hour
)

// HostCommandTimeout is the canonical bounded duration of one delegated
// refresh host-command attempt.
type HostCommandTimeout struct {
	seconds int
}

// NewHostCommandTimeout validates an explicit whole-second execution budget.
func NewHostCommandTimeout(requested time.Duration) (HostCommandTimeout, error) {
	if requested < MinimumHostCommandTimeout || requested > MaximumHostCommandTimeout {
		return HostCommandTimeout{}, fmt.Errorf(
			"refresh host-command timeout must be between %s and %s",
			MinimumHostCommandTimeout,
			MaximumHostCommandTimeout,
		)
	}
	if requested%time.Second != 0 {
		return HostCommandTimeout{}, fmt.Errorf(
			"refresh host-command timeout must be exactly representable as whole seconds",
		)
	}
	return HostCommandTimeout{seconds: int(requested / time.Second)}, nil
}

func normalizeHostCommandTimeout(requested time.Duration) (HostCommandTimeout, error) {
	if requested == 0 {
		requested = DefaultHostCommandTimeout
	}
	return NewHostCommandTimeout(requested)
}

// Duration returns the exact host-command attempt duration.
func (timeout HostCommandTimeout) Duration() time.Duration {
	return time.Duration(timeout.seconds) * time.Second
}

// Seconds returns the exact disclosure value.
func (timeout HostCommandTimeout) Seconds() int {
	return timeout.seconds
}
