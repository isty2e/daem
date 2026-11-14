// Package controlentry binds selected control-file authority and owns journal entry pairs.
// Containing operations retain their root, allocation, and recovery policy.
package controlentry

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func bindSelectedEntry(root *rootedpath.CapturedRoot, selectedRoot, path string, phase *physicalwork.TraversalPhase) (*rootedpath.EntryAuthority, error) {
	if phase == nil {
		return nil, fmt.Errorf("project control traversal authority is unavailable")
	}
	return rootedpath.BindSelectedControlEntryAuthorityBounded(root, selectedRoot, path, physicalwork.MaximumPhysicalPathDepth, phase)
}
