package operationplan

import (
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
)

// RecoveryPathAuthorityFingerprint is one owner-supplied exact path-authority
// identity projected into an active recovery fingerprint.
type RecoveryPathAuthorityFingerprint struct {
	Key              string
	SemanticsWitness string
}

// ActiveRecoveryCleanupObligation is one owner-classified removal continuation.
type ActiveRecoveryCleanupObligation struct {
	Scope       string
	Destination json.RawMessage
	Action      string
	Readiness   string
	Reason      string
	Detail      string
}

// ActiveRecoveryClaimTransition is one owner-supplied durable claim transition.
type ActiveRecoveryClaimTransition struct {
	Kind                    string
	PathAuthority           RecoveryPathAuthorityFingerprint
	ContentPath             string
	OwnerStatefileAuthority RecoveryPathAuthorityFingerprint
	OwnerManifestPath       string
	OperationID             string
}

// ActiveRecoveryIdentityInput is the exact active-journal operation projection.
// Opaque JSON members are canonical owner projections assembled without I/O.
type ActiveRecoveryIdentityInput struct {
	ManifestRoot                 string
	StatefilePath                string
	RecoveryDir                  string
	OperationID                  string
	OperationDir                 string
	Classification               string
	JournalAuthorityFingerprint  string
	StateDirAuthorityFingerprint string
	Actions                      json.RawMessage
	GuardedActions               json.RawMessage
	RemovalCleanupObligations    []ActiveRecoveryCleanupObligation
	StatefileBefore              json.RawMessage
	ClaimTransitions             []ActiveRecoveryClaimTransition
}

// ActiveRecoveryOperationFingerprint hashes the historical active-journal
// recovery projection without interpreting journal or StateDir semantics.
func ActiveRecoveryOperationFingerprint(
	input ActiveRecoveryIdentityInput,
) (mutation.OperationFingerprint, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint recovery plan: %w", err)
	}
	return mutation.NewOperationFingerprint(payload), nil
}

// CleanupRecoveryIdentityInput is the exact cleanup-only journal operation
// projection. Journal authority is supplied as an opaque owner fingerprint.
type CleanupRecoveryIdentityInput struct {
	RecoveryDir                 string
	OperationID                 string
	Classification              string
	Action                      string
	JournalAuthorityFingerprint string
	Phase                       string
	ResiduePresent              bool
}

// CleanupRecoveryOperationFingerprint hashes the historical cleanup-only
// recovery projection without interpreting journal or retirement semantics.
func CleanupRecoveryOperationFingerprint(
	input CleanupRecoveryIdentityInput,
) (mutation.OperationFingerprint, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf(
			"fingerprint journal cleanup plan: %w",
			err,
		)
	}
	return mutation.NewOperationFingerprint(payload), nil
}
