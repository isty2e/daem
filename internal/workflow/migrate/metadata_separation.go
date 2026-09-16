package migrate

import (
	"fmt"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output/ownership"
)

func (prepared *PreparedState) validateMetadataSeparation() error {
	marker, err := fileset.FileSetAuthorityPath(prepared.paths.StateDir)
	if err != nil {
		return err
	}
	for _, path := range []string{
		prepared.legacy.StatefilePath, prepared.paths.StatefilePath,
		prepared.paths.OwnershipRegistryPath, prepared.paths.CarrierClaimRegistryPath, marker,
	} {
		observed, err := mutation.ObserveDirectoryEntryAuthority(path)
		if err != nil {
			return err
		}
		if provisional, ok := observed.Provisional(); ok {
			if claim, conflict := prepared.outputs.ProvisionalAncestorConflict(provisional); conflict {
				return fmt.Errorf("migration metadata %q overlaps managed output %q", path, claim.Address().Path())
			}
			continue
		}
		exact, ok := observed.Exact()
		if !ok {
			return fmt.Errorf("migration metadata authority is unavailable for %q", path)
		}
		address, err := ownership.NewManagedAddress(exact, "")
		if err != nil {
			return err
		}
		if claim, conflict := prepared.outputs.Conflict(address); conflict {
			return fmt.Errorf("migration metadata %q overlaps managed output %q", path, claim.Address().Path())
		}
	}
	return nil
}
