package projection

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/output"
)

type ownershipObservationKey struct {
	destination output.Destination
	footprint   output.Footprint
}

func ownershipObservations(
	observations []observe.OwnershipObservation,
) (map[ownershipObservationKey]observe.OwnershipObservation, map[ownershipObservationKey]struct{}, error) {
	indexed := make(map[ownershipObservationKey]observe.OwnershipObservation, len(observations))
	keys := make([]ownershipObservationKey, 0, len(observations))
	conflicts := make(map[ownershipObservationKey]struct{})
	for index, observation := range observations {
		if err := observation.Validate(); err != nil {
			return nil, nil, fmt.Errorf("ownership observation[%d]: %w", index, err)
		}
		key := ownershipObservationKey{
			destination: observation.Destination(), footprint: observation.Footprint(),
		}
		if _, exists := indexed[key]; exists {
			path, _ := observation.Footprint().Key()
			return nil, nil, fmt.Errorf(
				"duplicate ownership observation for %q %s footprint %q",
				observation.Destination(),
				observation.Footprint().Kind(),
				path.String(),
			)
		}
		for _, previousKey := range keys {
			previous := indexed[previousKey]
			if previous.Overlaps(observation) {
				conflicts[previousKey] = struct{}{}
				conflicts[key] = struct{}{}
			}
		}
		indexed[key] = observation
		keys = append(keys, key)
	}
	return indexed, conflicts, nil
}
