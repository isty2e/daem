package controlentry

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/statefileoperation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

// BindStatefileEntry transfers only the acquired entry to Selected, not the supplied root or allocation.
func BindStatefileEntry(root *rootedpath.CapturedRoot, selectedRoot, path string, filesystem mutationfs.Store, phase *physicalwork.TraversalPhase, allocation *physicalwork.Budget) (*statefileoperation.Selected, error) {
	entry, err := bindSelectedEntry(root, selectedRoot, path, phase)
	if err != nil {
		return nil, fmt.Errorf("bind project statefile: %w", err)
	}
	return statefileoperation.NewSelected(filesystem, entry, allocation), nil
}
