package targetavailability

import (
	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/desired"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
)

// FromManifestLockAndState includes managed state-only projection targets for cleanup planning.
func FromManifestLockAndState(
	environment desired.Environment,
	locked lock.File,
	state durable.Snapshot,
	globalClaims durablecarrier.GlobalCarrierClaims,
) ([]target.Target, error) {
	available := FromEnvironment(environment)

	targets := make(map[target.Target]struct{}, len(available)+locked.Locked.Len())
	for _, selected := range available {
		targets[selected] = struct{}{}
	}
	for _, subject := range locked.Locked.Subjects() {
		if realization, ok := subject.Realization(); ok {
			for _, consumer := range realization.ConsumerTargets() {
				targets[consumer] = struct{}{}
			}
		}
	}
	for _, managedPath := range state.ManagedPaths() {
		for _, consumer := range managedPath.ConsumerTargets() {
			targets[consumer] = struct{}{}
		}
	}
	for _, managedAggregate := range state.ManagedAggregates() {
		targets[managedAggregate.Contribution().Target()] = struct{}{}
	}
	for _, pending := range state.PendingCarrierInstalls() {
		targets[pending.Identity().Target()] = struct{}{}
	}
	for _, claim := range state.ManagedCarrierClaims() {
		targets[claim.Identity().Target()] = struct{}{}
	}
	for _, claim := range globalClaims.Claims() {
		targets[claim.Identity().Target()] = struct{}{}
	}

	return orderedTargets(targets), nil
}

// FromEnvironment returns the exact target availability contributed by
// authored desired families. Top-level targets and extension relations are
// intentionally not part of this command-selection algebra.
func FromEnvironment(environment desired.Environment) []target.Target {
	targets := make(map[target.Target]struct{})
	add := func(values []target.Target) {
		for _, selected := range values {
			targets[selected] = struct{}{}
		}
	}
	for _, skill := range environment.Skills() {
		add(skill.Targets())
	}
	for _, set := range environment.SkillSets() {
		add(set.Targets())
	}
	for _, hook := range environment.Hooks() {
		add(hook.Targets())
	}
	for _, instructions := range environment.Instructions() {
		add(instructions.Targets())
	}
	for _, server := range environment.MCPServers() {
		for _, binding := range server.Bindings() {
			add([]target.Target{binding.Target()})
		}
	}
	return orderedTargets(targets)
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
