// Package effectpostcondition owns the closed route-coupled effect facts that
// a locked operation requires Assurance to verify. Relation postconditions
// remain a separate host-relation concern.
package effectpostcondition

import (
	"fmt"
	"slices"
)

// Requirement identifies one bounded route-coupled effect postcondition.
type Requirement string

const (
	// CarrierArtifactsAbsent requires fresh proof that every carrier artifact
	// selected by the exact route dossier is absent.
	CarrierArtifactsAbsent Requirement = "carrier_artifacts_absent"
	// LocalSourceUnchanged requires fresh proof that the selected local source
	// directory has not changed across the delegated removal attempt.
	LocalSourceUnchanged Requirement = "local_source_unchanged"
)

func parseRequirement(value string) (Requirement, error) {
	requirement := Requirement(value)
	switch requirement {
	case CarrierArtifactsAbsent, LocalSourceUnchanged:
		return requirement, nil
	default:
		return "", fmt.Errorf("effect postcondition requirement %q is unsupported", value)
	}
}

// Set is one canonical sorted unique collection of effect postconditions.
// The zero value explicitly means that a route has no coupled effect
// postconditions; it does not weaken a non-empty locked set.
type Set struct {
	requirements []Requirement
}

// NewSet validates, sorts, and defensively copies effect postconditions.
func NewSet(values []Requirement) (Set, error) {
	requirements := append([]Requirement(nil), values...)
	for index, value := range requirements {
		parsed, err := parseRequirement(string(value))
		if err != nil {
			return Set{}, fmt.Errorf("effect postcondition[%d]: %w", index, err)
		}
		requirements[index] = parsed
	}
	slices.Sort(requirements)
	for index := 1; index < len(requirements); index++ {
		if requirements[index-1] == requirements[index] {
			return Set{}, fmt.Errorf(
				"effect postcondition requirement %q is duplicated",
				requirements[index],
			)
		}
	}
	return Set{requirements: requirements}, nil
}

// Validate rejects forged or non-canonical sets.
func (set Set) Validate() error {
	expected, err := NewSet(set.requirements)
	if err != nil {
		return err
	}
	if !set.Equal(expected) {
		return fmt.Errorf("effect postcondition set is not canonical")
	}
	return nil
}

// Requirements returns a defensive copy in canonical order.
func (set Set) Requirements() []Requirement {
	return append([]Requirement(nil), set.requirements...)
}

// Empty reports whether no coupled effect postcondition is required.
func (set Set) Empty() bool {
	return len(set.requirements) == 0
}

// Equal reports exact canonical set equality.
func (set Set) Equal(other Set) bool {
	if len(set.requirements) != len(other.requirements) {
		return false
	}
	for index := range set.requirements {
		if set.requirements[index] != other.requirements[index] {
			return false
		}
	}
	return true
}
