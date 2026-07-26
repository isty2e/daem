package targetavailability

import (
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
)

// UnsupportedProjection identifies one selected desired entity whose target
// projection is not implemented.
type UnsupportedProjection struct {
	entityID entity.ID
	targets  []target.Target
}

// EntityID returns the desired entity whose projection is unavailable.
func (projection UnsupportedProjection) EntityID() entity.ID {
	return projection.entityID
}

// Targets returns the selected targets without an implemented projection.
func (projection UnsupportedProjection) Targets() []target.Target {
	return append([]target.Target(nil), projection.targets...)
}

// SelectedUnsupportedProjections derives selected target projection gaps from
// canonical desired resources and projection catalogs.
func SelectedUnsupportedProjections(
	environment desired.Environment,
	selection targetselection.Selection,
) []UnsupportedProjection {
	projections := make([]UnsupportedProjection, 0)
	for _, skill := range environment.Skills() {
		targets := selectedUnsupportedSkillTargets(skill.Targets(), selection)
		if len(targets) != 0 {
			projections = append(projections, UnsupportedProjection{
				entityID: skill.ID(),
				targets:  targets,
			})
		}
	}

	for _, hook := range environment.Hooks() {
		targets := selectedUnsupportedHookTargets(hook.Targets(), hook.Scope(), selection)
		if len(targets) != 0 {
			projections = append(projections, UnsupportedProjection{
				entityID: hook.ID(),
				targets:  targets,
			})
		}
	}

	return projections
}

func selectedUnsupportedSkillTargets(targets []target.Target, selection targetselection.Selection) []target.Target {
	selected := make([]target.Target, 0, len(targets))
	for _, selectedTarget := range targets {
		if selection.Includes(selectedTarget) && !profile.Profile(selectedTarget).Supports(entity.KindSkill) {
			selected = append(selected, selectedTarget)
		}
	}

	return selected
}

func selectedUnsupportedHookTargets(
	targets []target.Target,
	scope target.Scope,
	selection targetselection.Selection,
) []target.Target {
	selected := make([]target.Target, 0, len(targets))
	for _, selectedTarget := range targets {
		_, admitted := topologyhook.ProjectionNamespace(selectedTarget, scope)
		if selection.Includes(selectedTarget) && !admitted {
			selected = append(selected, selectedTarget)
		}
	}

	return selected
}
