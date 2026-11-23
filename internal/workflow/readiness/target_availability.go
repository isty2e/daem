package readiness

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
	targets := projectionTargetSet(environment, locked, state)
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

func projectionTargetSet(environment desired.Environment, locked lock.File, state durable.Snapshot) map[target.Target]struct{} {
	targets := declaredTargetSet(environment)
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
	return targets
}

// Top-level targets and extension relations do not contribute declared projection targets.
func declaredTargetSet(environment desired.Environment) map[target.Target]struct{} {
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
	for _, config := range environment.AgentConfigs() {
		add([]target.Target{config.Target()})
	}
	for _, server := range environment.MCPServers() {
		for _, binding := range server.Bindings() {
			add([]target.Target{binding.Target()})
		}
	}
	return targets
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
