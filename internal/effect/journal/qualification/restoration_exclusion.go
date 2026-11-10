package qualification

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output/ownership"
)

// RequireRestorationWriteExclusion binds semantic effects to independently
// observed path authority. It neither admits a recovery phase nor grants writes.
func RequireRestorationWriteExclusion(owner stateauthority.Authority, registry ownership.Registry, observed mutation.DirectoryEntryAuthorityObservation, restoration journaldocument.Restoration) error {
	if err := restoration.Validate(); err != nil {
		return err
	}
	if !restoration.Changed() {
		return nil
	}
	if err := observed.Validate(); err != nil {
		return err
	}
	if physical, exact := observed.Exact(); exact {
		var values []ownership.ManagedAddress
		for _, footprint := range restoration.ValueFootprints() {
			address, err := ownership.NewManagedAddress(physical, footprint)
			if err != nil {
				return err
			}
			values = append(values, address)
		}
		var existence, modes []pathauthority.Exact
		if restoration.ChangesExistence() {
			existence = []pathauthority.Exact{physical}
		}
		if restoration.ChangesMode() {
			modes = []pathauthority.Exact{physical}
		}
		return registry.RequireWriteExclusion(owner, values, existence, modes, nil)
	}
	if missing, provisional := observed.Provisional(); provisional {
		return registry.RequireWriteExclusion(owner, nil, nil, nil, []pathauthority.Provisional{missing})
	}
	return fmt.Errorf("restoration has no independently qualified exact or provisional path")
}
