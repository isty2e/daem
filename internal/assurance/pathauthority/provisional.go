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
	if !isDarwinWitness(provisional.candidateWitness) {
		return fmt.Errorf("provisional candidate requires Darwin path semantics")
	}
	if err := provisional.namespace.Validate(); err != nil {
		return fmt.Errorf("provisional namespace: %w", err)
	}
	if !isDarwinWitness(provisional.namespace.Witness()) {
		return fmt.Errorf("provisional namespace requires Darwin path semantics")
	}
	if !contains(provisional.namespace.Key(), provisional.candidateKey) ||
		provisional.namespace.Key() == provisional.candidateKey {
		return fmt.Errorf("provisional candidate must be below its exact namespace")
	}
	candidateModes := darwinFilesystemModes(provisional.candidateWitness)
	namespaceModes := darwinFilesystemModes(provisional.namespace.Witness())
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
	if !isDarwinWitness(exact.Witness()) || darwinFilesystemModes(exact.Witness()) != darwinFilesystemModes(provisional.candidateWitness) {
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

// CandidateWithin reports whether exact authority contains the provisional
// candidate key. This is conservative conflict evidence only; it does not
// promote the candidate to exact authority.
func (provisional Provisional) CandidateWithin(exact Exact) bool {
	if provisional.Validate() != nil || exact.Validate() != nil {
		return false
	}
	return contains(exact.Key(), provisional.candidateKey) ||
		legacyContains(exact.Key(), exact.Witness(), provisional.candidateKey, provisional.candidateWitness)
}

// CouldEqual excludes incompatible promotion depth or stable ASCII components.
// A true result retains uncertainty; it never certifies Unicode equivalence.
func (provisional Provisional) CouldEqual(exact Exact) bool {
	if provisional.AdmitsExact(exact) != nil {
		return false
	}
	candidate, _ := filepath.Rel(provisional.namespace.Key(), provisional.candidateKey)
	selected, _ := filepath.Rel(provisional.namespace.Key(), exact.Key())
	candidateParts := strings.Split(candidate, string(filepath.Separator))
	selectedParts := strings.Split(selected, string(filepath.Separator))
	for index, component := range candidateParts {
		other := selectedParts[index]
		if !containsNonASCIIByte(component) && !containsNonASCIIByte(other) && component != other {
			return false
		}
	}
	return true
}

// ConfinedWithin proves region membership from an existing namespace or a
// stable ASCII prefix. It does not establish the future entry's exact identity.
func (provisional Provisional) ConfinedWithin(root Exact) bool {
	if provisional.Validate() != nil || root.Validate() != nil {
		return false
	}
	if root.Contains(provisional.namespace) {
		return true
	}
	if !provisional.namespace.Contains(root) {
		return false
	}
	selected, _ := filepath.Rel(provisional.namespace.Key(), root.Key())
	candidate, _ := filepath.Rel(provisional.namespace.Key(), provisional.candidateKey)
	selectedParts := strings.Split(selected, string(filepath.Separator))
	candidateParts := strings.Split(candidate, string(filepath.Separator))
	if len(selectedParts) > len(candidateParts) {
		return false
	}
	for index, component := range selectedParts {
		if containsNonASCIIByte(component) || component != candidateParts[index] {
			return false
		}
	}
	return true
}

// StableRelativePrefix retains observed components and the following ASCII
// components, stopping before the first normalization-sensitive future name.
func (provisional Provisional) StableRelativePrefix(root Exact) (string, bool) {
	if !provisional.ConfinedWithin(root) {
		return "", false
	}
	known := 0
	if root.Contains(provisional.namespace) && root.Key() != provisional.namespace.Key() {
		relative, _ := filepath.Rel(root.Key(), provisional.namespace.Key())
		known = len(strings.Split(relative, string(filepath.Separator)))
	}
	relative, _ := filepath.Rel(root.Key(), provisional.candidateKey)
	parts := strings.Split(relative, string(filepath.Separator))
	end := 0
	for index, component := range parts {
		if index >= known && containsNonASCIIByte(component) {
			break
		}
		end++
	}
	if end == 0 {
		return "", false
	}
	return filepath.Join(parts[:end]...), true
}

// CouldOverlap excludes only regions separated by observed namespaces or stable
// ASCII components. Unknown normalization remains possible overlap.
func (provisional Provisional) CouldOverlap(exact Exact) bool {
	if provisional.Validate() != nil || exact.Validate() != nil {
		return true
	}
	if exact.Contains(provisional.namespace) {
		return true
	}
	if legacyContains(exact.Key(), exact.Witness(), provisional.namespace.Key(), provisional.namespace.Witness()) ||
		legacyContains(provisional.namespace.Key(), provisional.namespace.Witness(), exact.Key(), exact.Witness()) {
		return true
	}
	if !provisional.namespace.Contains(exact) {
		return false
	}
	selected, _ := filepath.Rel(provisional.namespace.Key(), exact.Key())
	candidate, _ := filepath.Rel(provisional.namespace.Key(), provisional.candidateKey)
	selectedParts := strings.Split(selected, string(filepath.Separator))
	candidateParts := strings.Split(candidate, string(filepath.Separator))
	for index := range min(len(selectedParts), len(candidateParts)) {
		left, right := selectedParts[index], candidateParts[index]
		if !containsNonASCIIByte(left) && !containsNonASCIIByte(right) && left != right {
			return false
		}
	}
	return true
}

// MatchesMissingExact reports whether this missing candidate retains the exact
// comparison key and filesystem-semantics witness observed before deletion. It
// does not restore exact authority or prove entry identity.
func (provisional Provisional) MatchesMissingExact(exact Exact) bool {
	if provisional.Validate() != nil || exact.Validate() != nil {
		return false
	}
	if provisional.candidateKey == exact.Key() && provisional.candidateWitness == exact.Witness() {
		return true
	}
	return strings.HasPrefix(exact.Witness(), darwinCaseWitnessPrefix) &&
		strings.HasPrefix(provisional.candidateWitness, darwinNativeWitnessPrefix) &&
		darwinFilesystemModes(provisional.candidateWitness) == darwinFilesystemModes(exact.Witness()) &&
		darwinLegacyComparisonKey(provisional.candidateKey, provisional.candidateWitness) == exact.Key()
}

// LegacyComparisonKey indexes recorded comparison history, not an access path.
func (provisional Provisional) LegacyComparisonKey() (string, bool) {
	if strings.HasPrefix(provisional.candidateWitness, darwinCaseWitnessPrefix) {
		return provisional.candidateKey, false
	}
	if !strings.HasPrefix(provisional.candidateWitness, darwinNativeWitnessPrefix) {
		return "", false
	}
	return darwinLegacyComparisonKey(provisional.candidateKey, provisional.candidateWitness), true
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
