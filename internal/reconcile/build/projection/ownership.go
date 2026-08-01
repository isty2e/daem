package projection

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/output"
)

type ownershipObservationKey struct {
	destination output.Destination
	contentPath output.ContentPath
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
			destination: observation.Destination(), contentPath: observation.ContentPath(),
		}
		if _, exists := indexed[key]; exists {
			return nil, nil, fmt.Errorf(
				"duplicate ownership observation for %q content path %q",
				observation.Destination(),
				observation.ContentPath(),
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
