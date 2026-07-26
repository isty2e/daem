package access

import "fmt"

// LimitError reports that one artifact access operation exceeded the
// caller-selected byte budget. The access package observes the exhaustion but
// does not own the policy that selected the limit.
type LimitError struct {
	operation string
	path      string
	limit     int64
	observed  int64
}

func (err *LimitError) Error() string {
	if err == nil {
		return "artifact access byte limit exceeded"
	}
	return fmt.Sprintf(
		"artifact access %s exceeds byte limit %d at %q (observed %d)",
		err.operation,
		err.limit,
		err.path,
		err.observed,
	)
}

// Limit returns the caller-selected maximum byte count.
func (err *LimitError) Limit() int64 {
	if err == nil {
		return 0
	}
	return err.limit
}

// Observed returns the first byte count known to exceed the limit.
func (err *LimitError) Observed() int64 {
	if err == nil {
		return 0
	}
	return err.observed
}

func newLimitError(operation string, path string, limit int64, observed int64) *LimitError {
	return &LimitError{
		operation: operation,
		path:      path,
		limit:     limit,
		observed:  observed,
	}
}
