package delegate

import (
	"fmt"
	"strings"
)

// Request is the exact route identity carried from a delegated
// realization into planning and execution. It grants no execution authority.
type Request struct {
	routeID              string
	contractVersion      string
	canonicalRequestHash string
}

// NewRequest constructs one validated delegated route identity.
func NewRequest(routeID string, contractVersion string, canonicalRequestHash string) (Request, error) {
	request := Request{
		routeID:              strings.TrimSpace(routeID),
		contractVersion:      strings.TrimSpace(contractVersion),
		canonicalRequestHash: strings.TrimSpace(canonicalRequestHash),
	}
	if err := request.Validate(); err != nil {
		return Request{}, err
	}
	return request, nil
}

// RouteID returns the static route selected by the realization profile.
func (request Request) RouteID() string { return request.routeID }

// ContractVersion returns the exact route adapter contract version.
func (request Request) ContractVersion() string { return request.contractVersion }

// CanonicalRequestHash returns the exact canonical delegated request digest.
func (request Request) CanonicalRequestHash() string {
	return request.canonicalRequestHash
}

// Equal reports whether two route requests carry the same exact identity.
func (request Request) Equal(other Request) bool {
	return request == other
}

// Validate rejects incomplete or non-canonical route identities.
func (request Request) Validate() error {
	if err := validateToken("route id", request.routeID); err != nil {
		return fmt.Errorf("delegated route request: %w", err)
	}
	if err := validateToken("route contract version", request.contractVersion); err != nil {
		return fmt.Errorf("delegated route request: %w", err)
	}
	if err := validateSHA256("canonical request hash", request.canonicalRequestHash); err != nil {
		return fmt.Errorf("delegated route request: %w", err)
	}
	return nil
}

func validateToken(label string, value string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be a stable token", label)
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '-' {
			continue
		}
		return fmt.Errorf("%s must be a stable token", label)
	}
	return nil
}

func validateSHA256(label string, value string) error {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+64 {
		return fmt.Errorf("%s must be a lowercase sha256 digest", label)
	}
	for _, character := range value[len(prefix):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fmt.Errorf("%s must be a lowercase sha256 digest", label)
		}
	}
	return nil
}
