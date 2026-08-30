package operationplan

import (
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
)

// ApplyIdentityInput is the exact apply operation projection. Opaque JSON
// members are canonical owner projections assembled without I/O.
type ApplyIdentityInput struct {
	ManifestPath        string
	LockfilePath        string
	LockfileExplicit    bool
	Targets             []string
	ManageUnmanaged     bool
	DelegateMode        string
	ManagedPaths        json.RawMessage
	Aggregates          json.RawMessage
	MCPProviders        json.RawMessage
	RelationActions     json.RawMessage
	RelationOrders      json.RawMessage
	CarrierAdoptions    json.RawMessage
	CarrierAbsences     json.RawMessage
	DelegateActions     json.RawMessage
	Owner               json.RawMessage
	Ownership           json.RawMessage
	GlobalCarrierClaims json.RawMessage
	Diagnostics         json.RawMessage
	ProjectRoot         json.RawMessage
}

// ApplyOperationFingerprint hashes the historical apply operation projection
// without interpreting reconciliation, ownership, or diagnostic semantics.
func ApplyOperationFingerprint(input ApplyIdentityInput) (mutation.OperationFingerprint, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint apply plan: %w", err)
	}
	return mutation.NewOperationFingerprint(payload), nil
}
