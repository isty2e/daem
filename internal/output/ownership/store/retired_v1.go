package store

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/output/ownership"
)

const retiredOwnershipRegistryVersion = 1

type retiredOwnershipRegistryV1 struct {
	Version int               `json:"version"`
	Claims  []json.RawMessage `json:"claims"`
}

func decodeRetiredOwnershipRegistry(content []byte) (ownership.Registry, error) {
	var persisted retiredOwnershipRegistryV1
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&persisted); err != nil {
		return ownership.Registry{}, fmt.Errorf(
			"decode retired ownership registry version %d: %w",
			retiredOwnershipRegistryVersion,
			err,
		)
	}
	if persisted.Version != retiredOwnershipRegistryVersion {
		return ownership.Registry{}, fmt.Errorf(
			"retired ownership registry decoder requires version %d, got %d",
			retiredOwnershipRegistryVersion,
			persisted.Version,
		)
	}
	if persisted.Claims == nil || len(persisted.Claims) != 0 {
		return ownership.Registry{}, fmt.Errorf(
			"ownership registry version %d is not an exact empty retired ownership registry: claims must be a present empty array; use the daem version that wrote it to retire managed ownership before upgrading",
			retiredOwnershipRegistryVersion,
		)
	}
	return ownership.EmptyRegistry(), nil
}
