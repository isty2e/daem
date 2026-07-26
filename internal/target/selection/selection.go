package targetselection

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/target"
)

// ErrInvalid classifies a command target selection that cannot be formed from
// the available target set.
var ErrInvalid = errors.New("target selection")

// Selection is a normalized command target selector.
type Selection struct {
	targets []target.Target
}

// ForAvailableTargets selects requested targets from caller-supplied manifest resource availability.
func ForAvailableTargets(available []target.Target, requested []string) (Selection, error) {
	availableTargets, err := availableTargetSet(available)
	if err != nil {
		return Selection{}, err
	}

	if len(requested) == 0 {
		return Selection{targets: orderedTargets(availableTargets)}, nil
	}

	selected, err := parseRequestedTargets(requested)
	if err != nil {
		return Selection{}, err
	}

	for _, target := range selected {
		if _, ok := availableTargets[target]; !ok {
			return Selection{}, fmt.Errorf("target %q does not match any manifest resource", target)
		}
	}

	return Selection{targets: selected}, nil
}

// ForDiagnostics selects any supported target for environment diagnostics.
func ForDiagnostics(requested []string) (Selection, error) {
	if len(requested) == 0 {
		return Selection{targets: target.SupportedTargets()}, nil
	}

	selected, err := parseRequestedTargets(requested)
	if err != nil {
		return Selection{}, err
	}

	return Selection{targets: selected}, nil
}

// Targets returns the selected targets in stable order.
func (selection Selection) Targets() []target.Target {
	return append([]target.Target(nil), selection.targets...)
}

// Includes reports whether a target is selected.
func (selection Selection) Includes(selected target.Target) bool {
	return slices.Contains(selection.targets, selected)
}

// IncludesAny reports whether at least one candidate target is selected.
func (selection Selection) IncludesAny(candidates []target.Target) bool {
	return slices.ContainsFunc(candidates, selection.Includes)
}

func parseRequestedTargets(values []string) ([]target.Target, error) {
	seen := make(map[target.Target]struct{}, len(values))

	for _, value := range values {
		selected, err := target.ParseTarget(strings.TrimSpace(value))
		if err != nil {
			return nil, err
		}

		if _, exists := seen[selected]; exists {
			continue
		}
		seen[selected] = struct{}{}
	}

	return orderedTargets(seen), nil
}

func availableTargetSet(values []target.Target) (map[target.Target]struct{}, error) {
	targets := make(map[target.Target]struct{}, len(values))
	for _, selected := range values {
		if _, err := target.ParseTarget(string(selected)); err != nil {
			return nil, err
		}
		targets[selected] = struct{}{}
	}

	return targets, nil
}

func orderedTargets(targets map[target.Target]struct{}) []target.Target {
	ordered := make([]target.Target, 0, len(targets))
	for _, selected := range target.SupportedTargets() {
		if _, ok := targets[selected]; ok {
			ordered = append(ordered, selected)
		}
	}

	return ordered
}
