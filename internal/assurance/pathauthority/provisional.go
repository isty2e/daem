package pathauthority

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Provisional records a normalization-sensitive future path and the exact
// existing namespace that contains it. It is comparison evidence, not exact
// filesystem authority.
type Provisional struct {
	candidateKey     string
	candidateWitness string
	namespace        Exact
}

// NewProvisional reconstructs one future-path observation without granting
// exact authority to its candidate spelling.
func NewProvisional(
	candidateKey string,
	candidateWitness string,
	namespaceKey string,
	namespaceWitness string,
) (Provisional, error) {
	namespace, err := NewExact(namespaceKey, namespaceWitness)
	if err != nil {
		return Provisional{}, fmt.Errorf("provisional namespace: %w", err)
	}
	provisional := Provisional{
		candidateKey:     candidateKey,
		candidateWitness: candidateWitness,
		namespace:        namespace,
	}
	if err := provisional.Validate(); err != nil {
		return Provisional{}, err
	}
	return provisional, nil
}

// Validate rejects incomplete or contradictory provisional path evidence.
func (provisional Provisional) Validate() error {
	if err := validateAbsoluteCleanPath("provisional candidate key", provisional.candidateKey); err != nil {
		return err
	}
	if err := validateWitness(provisional.candidateKey, provisional.candidateWitness); err != nil {
		return fmt.Errorf("provisional candidate: %w", err)
	}
	if !strings.HasPrefix(provisional.candidateWitness, darwinCaseWitnessPrefix) {
		return fmt.Errorf("provisional candidate requires Darwin path semantics")
	}
	if err := provisional.namespace.Validate(); err != nil {
		return fmt.Errorf("provisional namespace: %w", err)
	}
	if !strings.HasPrefix(provisional.namespace.Witness(), darwinCaseWitnessPrefix) {
		return fmt.Errorf("provisional namespace requires Darwin path semantics")
	}
	if !contains(provisional.namespace.Key(), provisional.candidateKey) ||
		provisional.namespace.Key() == provisional.candidateKey {
		return fmt.Errorf("provisional candidate must be below its exact namespace")
	}
	candidateModes := strings.TrimPrefix(provisional.candidateWitness, darwinCaseWitnessPrefix)
	namespaceModes := strings.TrimPrefix(provisional.namespace.Witness(), darwinCaseWitnessPrefix)
	if !strings.HasPrefix(candidateModes, namespaceModes) {
		return fmt.Errorf("provisional candidate and namespace have contradictory ancestor semantics")
	}
	relative, err := filepath.Rel(provisional.namespace.Key(), provisional.candidateKey)
	if err != nil {
		return fmt.Errorf("provisional candidate relative path: %w", err)
	}
	if !containsNonASCIIByte(relative) {
		return fmt.Errorf("provisional candidate suffix must be normalization-sensitive")
	}
	return nil
}

// AdmitsExact reports whether a freshly observed exact path can realize this
// intent. It does not infer Unicode equivalence; the caller must observe the
// originally selected path while retaining namespace exclusion.
func (provisional Provisional) AdmitsExact(exact Exact) error {
	if err := provisional.Validate(); err != nil {
		return err
	}
	if err := exact.Validate(); err != nil {
		return fmt.Errorf("promoted exact authority: %w", err)
	}
	if exact.Witness() != provisional.candidateWitness {
		return fmt.Errorf("promoted exact authority has different filesystem semantics")
	}
	if !contains(provisional.namespace.Key(), exact.Key()) ||
		exact.Key() == provisional.namespace.Key() {
		return fmt.Errorf("promoted exact authority escaped its namespace")
	}
	candidateDepth, err := relativeDepth(provisional.namespace.Key(), provisional.candidateKey)
	if err != nil {
		return fmt.Errorf("provisional candidate depth: %w", err)
	}
	exactDepth, err := relativeDepth(provisional.namespace.Key(), exact.Key())
	if err != nil {
		return fmt.Errorf("promoted exact authority depth: %w", err)
	}
	if candidateDepth != exactDepth {
		return fmt.Errorf("promoted exact authority changed path depth")
	}
	return nil
}

// Equal reports equality of the complete provisional observation.
func (provisional Provisional) Equal(other Provisional) bool {
	return provisional == other && provisional.Validate() == nil
}

// IsZero reports whether no provisional observation was initialized.
func (provisional Provisional) IsZero() bool {
	return provisional == Provisional{}
}

// CandidateKey returns the non-exact candidate comparison key.
func (provisional Provisional) CandidateKey() string {
	return provisional.candidateKey
}

// CandidateWitness returns the candidate filesystem-semantics witness.
func (provisional Provisional) CandidateWitness() string {
	return provisional.candidateWitness
}

// Namespace returns the exact existing namespace used for exclusion.
func (provisional Provisional) Namespace() Exact {
	return provisional.namespace
}

func contains(parent string, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func relativeDepth(parent string, child string) (int, error) {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return 0, err
	}
	if relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return 0, fmt.Errorf("path %q is not below %q", child, parent)
	}
	return len(strings.Split(relative, string(filepath.Separator))), nil
}

func containsNonASCIIByte(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] >= 0x80 {
			return true
		}
	}
	return false
}
