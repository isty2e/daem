package profile

import (
	"fmt"
	"os"

	desiredhostconfig "github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// ValidateHostConfigDocumentOccupancy checks native baseline identity and metadata,
// not current filesystem authority or admission to an ordinary managed-path route.
func ValidateHostConfigDocumentOccupancy(
	subject topology.SubjectID,
	consumerTargets []target.Target,
	scope target.Scope,
	destination output.Destination,
	contentKind realization.PathProjectionContentKind,
	permissionPolicy realization.PathPermissionPolicy,
	fileMode os.FileMode,
) error {
	if len(consumerTargets) != 1 {
		return fmt.Errorf("native document requires one consumer target")
	}
	id, entityBacked := topologyprojection.EntityID(subject)
	if !entityBacked {
		return fmt.Errorf("native document requires an entity-backed projection subject")
	}
	expected, err := aggregate.HostConfigDocumentSubject(id, consumerTargets[0], scope)
	if err != nil {
		return err
	}
	if subject != expected {
		return fmt.Errorf("native document subject does not match its target and scope")
	}
	if contentKind != realization.PathProjectionFile || permissionPolicy != realization.PathPermissionsExact || fileMode != fileMode.Perm() || fileMode&0o111 != 0 {
		return fmt.Errorf("native document requires a file with exact non-executable permission facts")
	}
	if err := destination.ValidateScope(scope); err != nil {
		return err
	}
	document, err := configuration.DocumentSpecForAlias(configuration.AliasInput{
		Target: consumerTargets[0], Scope: scope, Destination: destination,
	})
	if err != nil {
		return err
	}
	nativePath, native := document.NativePlacementPath()
	if !native {
		return fmt.Errorf("native document is outside its selected native root")
	}
	placement, err := ResolveHostConfigPlacement(configuration.PlacementInput{
		Target: consumerTargets[0], Scope: scope, Path: nativePath,
		Ownership: desiredhostconfig.OwnershipDocument,
	})
	if err != nil {
		return err
	}
	if placement.Destination() != destination {
		return fmt.Errorf("native document destination does not match its selected root")
	}
	return nil
}
