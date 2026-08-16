package listworkflow

import (
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/target"
)

// availableTargets is the list resources/paths selection set: header defaults
// unioned with every target that can appear in those commands.
func availableTargets(environment desired.Environment) []target.Target {
	seen := make(map[target.Target]struct{})
	add := func(values []target.Target) {
		for _, selected := range values {
			seen[selected] = struct{}{}
		}
	}
	add(environment.Targets())
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
	for _, extension := range environment.Extensions() {
		add([]target.Target{extension.Target()})
	}

	ordered := make([]target.Target, 0, len(seen))
	for _, selected := range target.SupportedTargets() {
		if _, ok := seen[selected]; ok {
			ordered = append(ordered, selected)
		}
	}
	return ordered
}
