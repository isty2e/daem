package carrierclaim

import (
	"bytes"
	"encoding/json"
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
)

const retiredCarrierClaimRegistryVersion = 1

type retiredCarrierClaimRegistryV1 struct {
	Version int               `json:"version"`
	Claims  []json.RawMessage `json:"claims"`
}

func decodeRetiredCarrierClaimRegistry(content []byte) (durablecarrier.GlobalCarrierClaims, error) {
	var persisted retiredCarrierClaimRegistryV1
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&persisted); err != nil {
		return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf(
			"decode retired carrier claim registry version %d: %w",
			retiredCarrierClaimRegistryVersion,
			err,
		)
	}
	if persisted.Version != retiredCarrierClaimRegistryVersion {
		return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf(
			"retired carrier claim registry decoder requires version %d, got %d",
			retiredCarrierClaimRegistryVersion,
			persisted.Version,
		)
	}
	if persisted.Claims == nil || len(persisted.Claims) != 0 {
		return durablecarrier.GlobalCarrierClaims{}, fmt.Errorf(
			"carrier claim registry version %d is not an exact empty retired carrier claim registry: claims must be a present empty array; use the daem version that wrote it to retire managed carriers before upgrading",
			retiredCarrierClaimRegistryVersion,
		)
	}
	return durablecarrier.EmptyGlobalCarrierClaims(), nil
}
