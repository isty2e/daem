package qualification

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
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
	return requireRestorationEffectsWriteExclusion(owner.Key(), registry, observed, restoration.ValueFootprints(), restoration.ChangesExistence(), restoration.ChangesMode())
}

func RequireRestorationEffectsWriteExclusion(owner stateauthority.Key, registry ownership.Registry, observed mutation.DirectoryEntryAuthorityObservation, effects recovery.RestorationEffects) error {
	if err := effects.Validate(); err != nil {
		return err
	}
	if !effects.Changed() {
		return nil
	}
	return requireRestorationEffectsWriteExclusion(owner, registry, observed, effects.ValueFootprints(), effects.ChangesExistence(), effects.ChangesMode())
}

func requireRestorationEffectsWriteExclusion(owner stateauthority.Key, registry ownership.Registry, observed mutation.DirectoryEntryAuthorityObservation, footprints []output.Footprint, changesExistence, changesMode bool) error {
	if err := observed.Validate(); err != nil {
		return err
	}
	if physical, exact := observed.Exact(); exact {
		var values []ownership.ManagedAddress
		for _, footprint := range footprints {
			address, err := ownership.NewManagedAddress(physical, footprint)
			if err != nil {
				return err
			}
			values = append(values, address)
		}
		var existence, modes []pathauthority.Exact
		if changesExistence {
			existence = []pathauthority.Exact{physical}
		}
		if changesMode {
			modes = []pathauthority.Exact{physical}
		}
		return registry.RequireWriteExclusion(owner, values, existence, modes, nil)
	}
	if missing, provisional := observed.Provisional(); provisional {
		return registry.RequireWriteExclusion(owner, nil, nil, nil, []pathauthority.Provisional{missing})
	}
	return fmt.Errorf("restoration has no independently qualified exact or provisional path")
}
