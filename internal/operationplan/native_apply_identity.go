package operationplan

import (
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
)

func NativeApplyOperationFingerprint(base ApplyIdentityInput, native json.RawMessage) (mutation.OperationFingerprint, error) {
	content, err := json.Marshal(struct {
		Profile string
		Base    ApplyIdentityInput
		Native  json.RawMessage
	}{"native-key-apply", base, native})
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint native apply: %w", err)
	}
	return mutation.NewOperationFingerprint(content), nil
}
