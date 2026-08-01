package statefile

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
)

const retiredStatefileVersion = 7

// retiredStatefileV7 contains only the old fact-family boundaries needed to
// prove that no legacy path authority remains. Legacy row shapes stay opaque.
type retiredStatefileV7 struct {
	Version                int               `json:"version"`
	ManagedPaths           []json.RawMessage `json:"managed_paths"`
	ManagedAggregates      []json.RawMessage `json:"managed_aggregate_contributions"`
	PendingCarrierInstalls []json.RawMessage `json:"pending_carrier_installs"`
	PendingCarrierRemovals []json.RawMessage `json:"pending_carrier_removals"`
	ManagedCarrierClaims   []json.RawMessage `json:"managed_carrier_claims"`
	DelegateAttempts       []json.RawMessage `json:"delegate_attempts"`
	HostRouteAttempts      []json.RawMessage `json:"host_route_attempts"`
}

func decodeRetiredStatefile(content []byte) (durable.Snapshot, error) {
	var persisted retiredStatefileV7
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&persisted); err != nil {
		return durable.Snapshot{}, fmt.Errorf("decode retired statefile version %d: %w", retiredStatefileVersion, err)
	}
	if persisted.Version != retiredStatefileVersion {
		return durable.Snapshot{}, fmt.Errorf(
			"retired statefile decoder requires version %d, got %d",
			retiredStatefileVersion,
			persisted.Version,
		)
	}
	families := []struct {
		name string
		rows []json.RawMessage
	}{
		{name: "managed_paths", rows: persisted.ManagedPaths},
		{name: "managed_aggregate_contributions", rows: persisted.ManagedAggregates},
		{name: "pending_carrier_installs", rows: persisted.PendingCarrierInstalls},
		{name: "pending_carrier_removals", rows: persisted.PendingCarrierRemovals},
		{name: "managed_carrier_claims", rows: persisted.ManagedCarrierClaims},
		{name: "delegate_attempts", rows: persisted.DelegateAttempts},
		{name: "host_route_attempts", rows: persisted.HostRouteAttempts},
	}
	for _, family := range families {
		if family.rows == nil || len(family.rows) != 0 {
			return durable.Snapshot{}, fmt.Errorf(
				"statefile version %d is not an exact empty retired statefile: field %q must be a present empty array; use the daem version that wrote it to recover or retire managed state before upgrading",
				retiredStatefileVersion,
				family.name,
			)
		}
	}
	return durable.EmptySnapshot(), nil
}
