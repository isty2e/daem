package pipackage

import (
	"fmt"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

// SettingsDocumentAddress identifies the independently selected Pi settings layer.
// It grants no filesystem observation or publication authority.
func SettingsDocumentAddress(scope target.Scope) (aggregate.DocumentAddress, error) {
	if scope != target.ScopeProject && scope != target.ScopeGlobal {
		return aggregate.DocumentAddress{}, fmt.Errorf("Pi settings scope is invalid")
	}
	capability, admitted := hostsurfacecatalog.Product().ExtensionOrderCapability(target.TargetPi, scope, desiredextension.CarrierPiPackage)
	projections := capability.NativeSequences()
	if !admitted || len(projections) != 1 {
		return aggregate.DocumentAddress{}, fmt.Errorf("Pi settings requires one admitted native sequence projection")
	}
	return aggregate.NewDocumentAddress(target.TargetPi, scope, projections[0].Destination())
}
