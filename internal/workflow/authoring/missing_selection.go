package authoring

import (
	"fmt"
	"slices"
	"strings"
)

// ResourceSelection identifies a removable manifest declaration. Alias is an
// optional installed name; Name remains the unambiguous declaration key.
type ResourceSelection struct {
	Name    string
	Alias   string
	Scope   string
	Targets []string
}

// ResourceSelectionNotFoundError retains manifest-backed alternatives to a missing selection.
type ResourceSelectionNotFoundError struct {
	Kind      string
	Name      string
	Available []ResourceSelection
}

func (err *ResourceSelectionNotFoundError) Error() string {
	return fmt.Sprintf("%s resource %q not found", err.Kind, err.Name)
}

// AvailableSelections returns a copy of the admitted alternatives.
func (err *ResourceSelectionNotFoundError) AvailableSelections() []ResourceSelection {
	selections := slices.Clone(err.Available)
	for index := range selections {
		selections[index].Targets = slices.Clone(selections[index].Targets)
	}
	return selections
}

func missingResourceSelection(kind string, name string, available []ResourceSelection) error {
	candidates := make([]ResourceSelection, 0)
	bestRank := 3
	for _, selection := range available {
		rank := selection.matchRank(name)
		if rank > bestRank || rank == 3 {
			continue
		}
		if rank < bestRank {
			candidates = candidates[:0]
			bestRank = rank
		}
		selection.Targets = slices.Clone(selection.Targets)
		slices.Sort(selection.Targets)
		candidates = append(candidates, selection)
	}
	slices.SortFunc(candidates, func(left, right ResourceSelection) int {
		if order := strings.Compare(left.Name, right.Name); order != 0 {
			return order
		}
		if order := strings.Compare(left.Scope, right.Scope); order != 0 {
			return order
		}
		return strings.Compare(strings.Join(left.Targets, "\x00"), strings.Join(right.Targets, "\x00"))
	})
	if len(candidates) > 3 {
		candidates = slices.Clone(candidates[:3])
	}
	return &ResourceSelectionNotFoundError{Kind: kind, Name: name, Available: candidates}
}

func (selection ResourceSelection) matchRank(name string) int {
	if selection.Name == name || selection.Alias != "" && selection.Alias == name {
		return 0
	}
	if strings.EqualFold(selection.Name, name) || selection.Alias != "" && strings.EqualFold(selection.Alias, name) {
		return 1
	}
	if oneEditApart(strings.ToLower(selection.Name), strings.ToLower(name)) ||
		selection.Alias != "" && oneEditApart(strings.ToLower(selection.Alias), strings.ToLower(name)) {
		return 2
	}
	return 3
}

func oneEditApart(left, right string) bool {
	a, b := []rune(left), []rune(right)
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(b)-len(a) > 1 {
		return false
	}
	for i := range a {
		if a[i] == b[i] {
			continue
		}
		if len(a) == len(b) {
			return slices.Equal(a[i+1:], b[i+1:])
		}
		return slices.Equal(a[i:], b[i+1:])
	}
	return true
}
