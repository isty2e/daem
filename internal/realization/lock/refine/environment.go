package refine

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/realization/lock"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
)

// ExpectedManagedPaths refines manifest-derived path intent for lock-readiness comparison.
func ExpectedManagedPaths(
	environment desired.Environment,
	locked lock.LockedSection,
) ([]lock.LockedSubjectContract, error) {
	if err := environment.Validate(); err != nil {
		return nil, fmt.Errorf("desired environment: %w", err)
	}
	contracts := make([]lock.LockedSubjectContract, 0)
	for _, value := range environment.Skills() {
		projections, err := SkillPathProjections(value)
		if err != nil {
			return nil, err
		}
		contracts = append(contracts, projections...)
	}
	for _, value := range environment.Instructions() {
		projections, err := InstructionsPathProjections(value)
		if err != nil {
			return nil, err
		}
		contracts = append(contracts, projections...)
	}
	hookTopology, err := topologyhook.Lower(environment.HookAssets(), environment.Hooks())
	if err != nil {
		return nil, err
	}
	hookAssetProjections, err := HookAssetPathProjections(
		environment.HookAssets(),
		hookTopology,
		locked.Subjects(),
	)
	if err != nil {
		return nil, err
	}
	contracts = append(contracts, hookAssetProjections...)
	return contracts, nil
}

// ValidateManagedPathIntent checks path placements that need no resolved Supply fact.
func ValidateManagedPathIntent(environment desired.Environment) error {
	if err := environment.Validate(); err != nil {
		return fmt.Errorf("desired environment: %w", err)
	}
	for _, value := range environment.Skills() {
		if _, err := SkillPathProjections(value); err != nil {
			return err
		}
	}
	for _, value := range environment.Instructions() {
		if _, err := InstructionsPathProjections(value); err != nil {
			return err
		}
	}
	_, err := topologyhook.Lower(environment.HookAssets(), environment.Hooks())
	return err
}
