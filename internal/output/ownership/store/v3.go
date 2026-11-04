package store

import (
	"fmt"

	"github.com/isty2e/daem/internal/output/ownership"
)

const legacyNativeRegistryVersion = 3

type registryV3File struct {
	Version int           `json:"version"`
	Claims  []claimRecord `json:"claims"`
}

func decodeOwnershipRegistryV3(content []byte) (ownership.Registry, error) {
	var persisted registryV3File
	if err := decodeRegistryJSON(content, &persisted); err != nil {
		return ownership.Registry{}, err
	}
	if persisted.Version != legacyNativeRegistryVersion {
		return ownership.Registry{}, unsupportedOwnershipRegistryVersion(persisted.Version)
	}
	if persisted.Claims == nil {
		return ownership.Registry{}, fmt.Errorf("ownership registry requires claims array")
	}
	return decodeRegistryClaims(persisted.Claims)
}
