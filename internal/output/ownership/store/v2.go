package store

import (
	"fmt"

	footprintcodec "github.com/isty2e/daem/internal/encoding/footprint"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

const previousOwnershipRegistryVersion = 2

type registryV2File struct {
	Version int                     `json:"version"`
	Claims  []registryV2ClaimRecord `json:"claims"`
}

type registryV2ClaimRecord struct {
	PathAuthority      pathAuthorityRecord `json:"path_authority"`
	ContentPath        string              `json:"content_path,omitempty"`
	StatefileAuthority pathAuthorityRecord `json:"statefile_authority"`
	ManifestPath       string              `json:"manifest_path"`
	State              string              `json:"state"`
	OperationID        string              `json:"operation_id,omitempty"`
}

// The v2 input contract is frozen: literal tilde sequences are key text, and
// an absent/empty content_path means whole physical path authority.
func decodeOwnershipRegistryV2(content []byte) (ownership.Registry, error) {
	var persisted registryV2File
	if err := decodeRegistryJSON(content, &persisted); err != nil {
		return ownership.Registry{}, err
	}
	if persisted.Version != previousOwnershipRegistryVersion {
		return ownership.Registry{}, unsupportedOwnershipRegistryVersion(persisted.Version)
	}
	if persisted.Claims == nil {
		return ownership.Registry{}, fmt.Errorf("ownership registry requires claims array")
	}
	records := make([]claimRecord, 0, len(persisted.Claims))
	for index, legacy := range persisted.Claims {
		footprint, err := output.FootprintFromLegacy(output.ContentPath(legacy.ContentPath))
		if err != nil {
			return ownership.Registry{}, fmt.Errorf("ownership registry claims[%d] legacy footprint: %w", index, err)
		}
		record, err := footprintcodec.Encode(footprint)
		if err != nil {
			return ownership.Registry{}, err
		}
		records = append(records, claimRecord{
			PathAuthority: legacy.PathAuthority, Footprint: record,
			StatefileAuthority: legacy.StatefileAuthority, ManifestPath: legacy.ManifestPath,
			State: legacy.State, OperationID: legacy.OperationID,
		})
	}
	return decodeRegistryClaims(records)
}
