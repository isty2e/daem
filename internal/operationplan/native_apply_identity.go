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

type ReplacementSource struct{ Subject, SourceID, ResolvedRef, ContentHash, Profile, Grammar string }

type ReplacementRoot struct{ Identity, Policy string }

type ReplacementRole struct {
	Root                               ReplacementRoot
	Target, Scope, Path, Child, Policy string
}

type ReplacementMember struct{ Subject, Target, Scope, Path, Footprint, Key, Codec string }

type ReplacementProtection struct {
	Namespace  json.RawMessage
	Protection string
	Roles      []ReplacementRole
	Global     []ReplacementRoot
	Members    []ReplacementMember
}

// NativeReplacementPayload is shared by the disclosed v1 token and the under-lease identity.
func NativeReplacementPayload(identity ApplyIdentityInput, source ReplacementSource, protections []ReplacementProtection, limits json.RawMessage) ([]byte, error) {
	return json.Marshal(struct {
		Operation  string
		Identity   ApplyIdentityInput
		Source     ReplacementSource
		Protection []ReplacementProtection
		Limits     json.RawMessage
	}{"replace-agent-config/v1", identity, source, protections, limits})
}
