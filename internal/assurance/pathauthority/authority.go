// Package pathauthority models exact filesystem-path identity independently
// from the filesystem observation that established it.
package pathauthority

import (
	"cmp"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	exactWitnessPrefix        = "exact-v1:"
	windowsFoldWitnessPrefix  = "windows-fold-v1:"
	darwinCaseWitnessPrefix   = "darwin-case-v1:"
	darwinNativeWitnessPrefix = "darwin-case-v2:"
)

// Exact is one canonical path key together with the versioned filesystem
// semantics observed while deriving that key.
type Exact struct {
	key     string
	witness string
}

// NewExact validates a canonical path key and its persisted semantics witness.
func NewExact(key string, witness string) (Exact, error) {
	authority := Exact{key: key, witness: witness}
	if err := authority.Validate(); err != nil {
		return Exact{}, err
	}
	return authority, nil
}

// Validate rejects incomplete, malformed, or internally inconsistent exact
// path authority.
func (authority Exact) Validate() error {
	if err := validateAbsoluteCleanPath("exact path authority key", authority.key); err != nil {
		return err
	}
	return validateWitness(authority.key, authority.witness)
}

// Key returns the canonical filesystem equality key.
func (authority Exact) Key() string {
	return authority.key
}

// Witness returns the versioned semantics used to derive Key.
func (authority Exact) Witness() string {
	return authority.witness
}

// Equal reports exact equality of key and observed semantics.
func (authority Exact) Equal(other Exact) bool {
	return authority == other
}

// LegacyComparisonKey returns a Darwin comparison domain and whether it is an
// alias of a native-spelling key. An empty key means no legacy domain exists.
// It is never a filesystem locator or access grant.
func (authority Exact) LegacyComparisonKey() (string, bool) {
	if strings.HasPrefix(authority.witness, darwinCaseWitnessPrefix) {
		return authority.key, false
	}
	if !strings.HasPrefix(authority.witness, darwinNativeWitnessPrefix) {
		return "", false
	}
	return darwinLegacyComparisonKey(authority.key, authority.witness), true
}

// MatchesLegacyComparison compares an observation at an independently qualified
// recorded path. It must not select a locator, rekey an owner, or establish equality.
func (authority Exact) MatchesLegacyComparison(recorded Exact) bool {
	legacy, present := authority.LegacyComparisonKey()
	return present && strings.HasPrefix(recorded.witness, darwinCaseWitnessPrefix) &&
		legacy == recorded.key && darwinFilesystemModes(authority.witness) == darwinFilesystemModes(recorded.witness)
}

// MayOverlap preserves uncertain legacy comparison domains for exclusion only.
// It does not establish equality, containment, or mutation authority.
func (authority Exact) MayOverlap(other Exact) bool {
	return authority.MayContain(other) || other.MayContain(authority)
}

// MayContain is conservative exclusion evidence, never a containment proof.
func (authority Exact) MayContain(other Exact) bool {
	return authority.Contains(other) || legacyContains(authority.key, authority.witness, other.key, other.witness)
}

// Compare returns the deterministic order of exact path authorities.
func (authority Exact) Compare(other Exact) int {
	return cmp.Or(
		cmp.Compare(authority.key, other.key),
		cmp.Compare(authority.witness, other.witness),
	)
}

// Contains reports whether authority's canonical key contains other's key.
// Differing witnesses do not make lexically overlapping authority safe: callers
// must conservatively retain the overlap and reject semantic drift elsewhere.
func (authority Exact) Contains(other Exact) bool {
	relative, err := filepath.Rel(authority.key, other.key)
	if err != nil {
		return false
	}
	return relative == "." ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

// ImmediateChildName projects a candidate spelling from canonical path keys.
// It does not prove matching filesystem semantics or grant entry authority.
func (authority Exact) ImmediateChildName(other Exact) (string, bool) {
	if authority.Validate() != nil || other.Validate() != nil {
		return "", false
	}
	relative, err := filepath.Rel(authority.key, other.key)
	if err != nil || relative == "." || relative == ".." || filepath.Base(relative) != relative {
		return "", false
	}
	return relative, true
}

// IsZero reports whether no exact authority was initialized.
func (authority Exact) IsZero() bool {
	return authority == (Exact{})
}

func validateWitness(key string, witness string) error {
	switch {
	case witness == exactWitnessPrefix:
		return nil
	case witness == windowsFoldWitnessPrefix:
		if key != strings.ToLower(key) {
			return fmt.Errorf("Windows-fold path authority key %q must be lowercase", key)
		}
		return nil
	case isDarwinWitness(witness):
		modes := darwinWitnessModes(witness)
		native := strings.HasPrefix(witness, darwinNativeWitnessPrefix)
		if native && !strings.ContainsRune(modes, 'u') {
			return fmt.Errorf("Darwin native-spelling witness requires a retained component")
		}
		components := rootedComponents(key)
		if len(modes) != len(components) {
			return fmt.Errorf(
				"Darwin path authority witness records %d components, want %d for %q",
				len(modes),
				len(components),
				key,
			)
		}
		for index := range len(modes) {
			if modes[index] == 'u' && native {
				if !containsNonASCIIByte(components[index]) || components[index] == strings.ToLower(components[index]) {
					return fmt.Errorf("Darwin retained component %q must preserve its non-ASCII spelling", components[index])
				}
				continue
			}
			if modes[index] != 's' && modes[index] != 'i' {
				return fmt.Errorf(
					"Darwin path authority witness has unsupported mode %q at component %d",
					modes[index],
					index,
				)
			}
			if modes[index] == 'i' && components[index] != strings.ToLower(components[index]) {
				return fmt.Errorf(
					"Darwin case-insensitive path authority component %q must be lowercase",
					components[index],
				)
			}
		}
		return nil
	case witness == "":
		return fmt.Errorf("path authority semantics witness is required")
	default:
		return fmt.Errorf("unsupported path authority semantics witness %q", witness)
	}
}

func isDarwinWitness(witness string) bool {
	return strings.HasPrefix(witness, darwinCaseWitnessPrefix) || strings.HasPrefix(witness, darwinNativeWitnessPrefix)
}

func darwinWitnessModes(witness string) string {
	return strings.TrimPrefix(strings.TrimPrefix(witness, darwinCaseWitnessPrefix), darwinNativeWitnessPrefix)
}

func darwinFilesystemModes(witness string) string {
	return strings.ReplaceAll(darwinWitnessModes(witness), "u", "i")
}

func darwinLegacyComparisonKey(key, witness string) string {
	modes := darwinWitnessModes(witness)
	result := filepath.VolumeName(key) + string(filepath.Separator)
	for index, component := range rootedComponents(key) {
		if modes[index] == 'u' {
			component = strings.ToLower(component)
		}
		result = filepath.Join(result, component)
	}
	return result
}

func legacyContains(parentKey, parentWitness, childKey, childWitness string) bool {
	if strings.HasPrefix(parentWitness, darwinNativeWitnessPrefix) && strings.HasPrefix(childWitness, darwinCaseWitnessPrefix) {
		return contains(darwinLegacyComparisonKey(parentKey, parentWitness), childKey)
	}
	if strings.HasPrefix(parentWitness, darwinCaseWitnessPrefix) && strings.HasPrefix(childWitness, darwinNativeWitnessPrefix) {
		return contains(parentKey, darwinLegacyComparisonKey(childKey, childWitness))
	}
	return false
}

func rootedComponents(path string) []string {
	volume := filepath.VolumeName(path)
	relative := strings.TrimPrefix(path, volume+string(filepath.Separator))
	if relative == "" {
		return nil
	}
	return strings.Split(relative, string(filepath.Separator))
}

func validateAbsoluteCleanPath(label string, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s is not valid UTF-8", label)
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("%s contains a NUL byte", label)
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("%s %q must be absolute", label, value)
	}
	if filepath.Clean(value) != value {
		return fmt.Errorf("%s %q must be clean", label, value)
	}
	return nil
}
