package operationplan

import (
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
)

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
