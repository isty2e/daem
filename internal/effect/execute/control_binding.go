package execute

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func (authority *mutationAuthority) bindRecoveryStatefileSemanticEntry(path string) error {
	if authority == nil || authority.physicalWorkBudget == nil {
		return fmt.Errorf("recovery semantic authority is unavailable")
	}
	if authority.control.StatefileBound() {
		return fmt.Errorf("recovery statefile semantic authority is already bound")
	}
	root, destination, err := rootedpath.CaptureDestinationBounded(
		path,
		physicalwork.MaximumPhysicalPathDepth,
		authority.generalTraversalPhase,
	)
	if err != nil {
		return fmt.Errorf("bind recovery statefile semantic authority: %w", err)
	}
	root, err = authority.destinations.RetainRoot(root, destination)
	if err != nil {
		return err
	}
	return authority.control.BindStatefile(root, destination)
}
