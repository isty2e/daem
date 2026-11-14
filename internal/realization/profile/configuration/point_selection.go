package configuration

import "fmt"

// PointAlternative names a role within a versioned preferred/fallback policy,
// not a path expression or permission to select a file.
type PointAlternative string

const (
	PointPreferred PointAlternative = "preferred"
	PointFallback  PointAlternative = "fallback"
)

func (alternative PointAlternative) Validate() error {
	switch alternative {
	case PointPreferred, PointFallback:
		return nil
	default:
		return fmt.Errorf("unknown file-point alternative %q", alternative)
	}
}
