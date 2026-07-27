package readiness

import (
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	ownershipobserve "github.com/isty2e/daem/internal/assurance/observe/ownership"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

type managedPathPlanningInputs struct {
	requests  []liveobserve.ManagedPathRequest
	ownership []ownershipobserve.ManagedPathInput
	states    []durable.ManagedPathState
}

func buildManagedPathPlanningInputs(
	locked lock.File,
	currentState durable.Snapshot,
	selection targetselection.Selection,
) (managedPathPlanningInputs, error) {
	states := currentState.ManagedPaths()
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
