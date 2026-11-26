package readiness

import (
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	ownershipobserve "github.com/isty2e/daem/internal/assurance/observe/ownership"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type managedPathPlanningInputs struct {
	requests  []liveobserve.ManagedPathRequest
	ownership []ownershipobserve.ManagedPathInput
	states    []durable.ManagedPathState
}

func (inputs managedPathPlanningInputs) withLegacyTreeEvidence(locked lock.File, observations []observe.ExactSupplyObservation) {
	bySupply := make(map[topology.SubjectID]artifact.ExecutablePaths)
	for _, observed := range observations {
		if classes := observed.ExecutablePaths(); classes != nil {
			bySupply[observed.Subject()] = classes
		}
	}
	legacy := make(map[topology.SubjectID]artifact.ExecutablePaths)
	for _, state := range inputs.states {
		if state.ContentKind() != realization.PathProjectionDirectory || state.ContentHash().IsInstalled() {
			continue
		}
		if entityID, ok := topologyprojection.EntityID(state.Subject()); ok {
			if supply, ok := locked.Locked.ExactSupplySubject(entityID); ok {
				legacy[state.Subject()] = bySupply[supply.SubjectID()]
			}
		}
	}
	for index, request := range inputs.requests {
		inputs.requests[index].LegacyExecutables = legacy[request.Subject]
	}
}

func buildManagedPathPlanningInputs(
	locked lock.File,
	states []durable.ManagedPathState,
	selection targetselection.Selection,
) (managedPathPlanningInputs, error) {
	inputs := managedPathPlanningInputs{states: states}
	for _, contract := range locked.Locked.Subjects() {
		realization, realized := contract.Realization()
		if !realized {
			continue
		}
		projection, managedPath := realization.ManagedPathProjection()
		if !managedPath || !selectedManagedPathConsumers(projection.ConsumerTargets(), selection) {
			continue
		}
		inputs.requests = append(inputs.requests, liveobserve.ManagedPathRequest{
			Subject: contract.SubjectID(), Destination: projection.Destination(),
			ContentKind: projection.ContentKind(),
		})
		inputs.ownership = append(inputs.ownership, ownershipobserve.ManagedPathInput{
			Scope: projection.Scope(), Destination: projection.Destination(),
			ConsumerTargets: projection.ConsumerTargets(),
		})
	}
	for _, state := range states {
		if !selectedManagedPathConsumers(state.ConsumerTargets(), selection) {
			continue
		}
		inputs.requests = append(inputs.requests, liveobserve.ManagedPathRequest{
			Subject: state.Subject(), Destination: state.Destination(), ContentKind: state.ContentKind(),
		})
		inputs.ownership = append(inputs.ownership, ownershipobserve.ManagedPathInput{
			Scope: state.Scope(), Destination: state.Destination(), ConsumerTargets: state.ConsumerTargets(),
		})
	}
	return inputs, nil
}

func selectedManagedPathConsumers(consumers []target.Target, selection targetselection.Selection) bool {
	return slices.ContainsFunc(consumers, selection.Includes)
}
