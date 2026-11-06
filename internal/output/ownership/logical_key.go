package ownership

import (
	"fmt"

	"github.com/isty2e/daem/internal/output"
)

// LogicalKey selects an output footprint within one owner's operation. It is
// neither a physical ManagedAddress nor an ownership grant.
type LogicalKey struct {
	Destination output.Destination
	Footprint   output.Footprint
}

func (key LogicalKey) ScopeLabel() string {
	if key.Footprint.Kind() == output.FootprintPath {
		return "whole path"
	}
	if path, present := key.Footprint.Key(); present {
		return fmt.Sprintf("key %q", path.String())
	}
	return "invalid footprint"
}
