package attempt

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func validateAttemptIdentity(
	subject topology.SubjectID,
	requiredKind topology.SubjectKind,
	selectedTarget target.Target,
	scope target.Scope,
	context string,
) error {
	if err := subject.Validate(); err != nil {
		return fmt.Errorf("%s subject: %w", context, err)
	}
	if subject.Kind() != requiredKind {
		return fmt.Errorf("%s requires %s subject", context, requiredKind)
	}
	if _, err := target.ParseTarget(string(selectedTarget)); err != nil {
		return fmt.Errorf("%s target: %w", context, err)
	}
	if _, err := target.ParseScope(string(scope)); err != nil {
		return fmt.Errorf("%s scope: %w", context, err)
	}
	return nil
}

func validateCanonicalIdentityText(value string, context string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", context)
	}
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be non-empty and trimmed", context)
	}
	if strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character)
	}) >= 0 {
		return fmt.Errorf("%s contains an unsafe control character", context)
	}
	return nil
}

func validateRouteRequestHash(value string, context string) error {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+64 {
		return fmt.Errorf("%s route request hash must be a lowercase SHA-256 digest", context)
	}
	for _, character := range value[len(prefix):] {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return fmt.Errorf("%s route request hash must be a lowercase SHA-256 digest", context)
		}
	}
	return nil
}

func validateHistoricalTime(value time.Time, context string) error {
	encoded := value.Format(time.RFC3339Nano)
	decoded, err := time.Parse(time.RFC3339Nano, encoded)
	if err != nil || !decoded.Equal(value) {
		return fmt.Errorf("%s is outside the durable RFC3339Nano range", context)
	}
	return nil
}
