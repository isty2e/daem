package clipresent

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/credentialtext"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/subprocess"
)

// Machine identifiers are exact or withheld with the entire result. Errors
// name only the wire field: the rejected spelling may itself be a credential.
func checkAgentConfigIdentifier(field, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s cannot be represented as a UTF-8 JSON identifier", field)
	}
	if agentConfigIdentifierProtected(value) {
		return fmt.Errorf("%s cannot be disclosed as an exact JSON identifier", field)
	}
	return nil
}

func agentConfigIdentifierProtected(value string) bool {
	if _, protected := credentialtext.Redact(value, errorEvidenceRedactionMarker, nil); protected {
		return true
	}
	if escaped := Escape(value); escaped != value {
		_, protected := credentialtext.Redact(escaped, errorEvidenceRedactionMarker, nil)
		return protected
	}
	return false
}

func agentConfigKeySegments(field string, key output.KeyPath) ([]string, error) {
	if err := checkAgentConfigIdentifier(field, key.String()); err != nil {
		return nil, err
	}
	segments := key.Segments()
	for _, segment := range segments {
		if err := checkAgentConfigIdentifier(field, segment); err != nil {
			return nil, err
		}
	}
	return segments, nil
}

func humanAgentConfigValue(key output.KeyPath, value string) string {
	return Escape(agentConfigValueSnippet(key, value))
}

// Keep unmasked snippets in host syntax. Both an enclosing key and the text
// of the value itself can require masking, independent of terminal escaping.
func agentConfigValueSnippet(key output.KeyPath, value string) string {
	const overBudget = "[REDACTED: display limit]"
	if len(value) > maximumHumanErrorDetailRunes {
		return overBudget
	}
	if _, protected := credentialtext.Redact(value, errorEvidenceRedactionMarker, nil); protected {
		return errorEvidenceRedactionMarker
	}
	policy := subprocess.NewCapturePolicy(nil, maximumHumanErrorDetailRunes)
	for _, segment := range key.Segments() {
		encoded, err := json.Marshal(map[string]string{segment: value})
		if err != nil {
			return errorEvidenceRedactionMarker
		}
		result := policy.Sanitize(string(encoded), false)
		if result.Redacted() || result.Truncated() {
			return errorEvidenceRedactionMarker
		}
	}
	result := policy.Sanitize(Escape(value), false)
	if result.Redacted() {
		return errorEvidenceRedactionMarker
	}
	if result.Truncated() {
		return overBudget
	}
	return value
}
