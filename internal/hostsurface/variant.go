package hostsurface

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// VariantID distinguishes independently selectable host contracts that share
// a target, scope, and entity kind. It is not an observation, effect,
// capability, or persisted occurrence identity.
type VariantID string

// VariantDefault is the sole MCP variant and the default when a family admits
// exactly one host contract at a target and scope.
const VariantDefault VariantID = "default"

// ParseVariantID validates a host-surface variant token.
func ParseVariantID(value string) (VariantID, error) {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return "", fmt.Errorf("host-surface variant must be a stable token")
	}
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("host-surface variant must be valid UTF-8")
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '.' ||
			character == '_' ||
			character == '-' {
			continue
		}
		return "", fmt.Errorf("host-surface variant must be a stable token")
	}
	return VariantID(value), nil
}
