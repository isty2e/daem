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

func (inputs *managedPathPlanningInputs) attachVerifiedTreeEvidence(locked lock.File, observations []observe.ExactSupplyObservation) {
	bySupply := make(map[topology.SubjectID]observe.ExactSupplyObservation)
	for _, observed := range observations {
		bySupply[observed.Subject()] = observed
	}
	states := make(map[topology.SubjectID]durable.ManagedPathState)
	for _, state := range inputs.states {
		states[state.Subject()] = state
	}
	for i := range inputs.requests {
		request := &inputs.requests[i]
		state, ok := states[request.Subject]
		if !ok || request.ContentKind != realization.PathProjectionDirectory || request.Destination != state.Destination() {
			continue
		}
		id, _ := topologyprojection.EntityID(state.Subject())
		contract, found := locked.Locked.ExactSupplySubject(id)
		if !found {
			request.MembershipOnly = false
			continue
		}
		supply := bySupply[contract.SubjectID()]
		if !state.ContentHash().IsInstalled() {
			request.LegacyExecutables = supply.ExecutablePaths()
			if identity, ok := contract.ExactSupply(); ok && identity.ContentHash() == state.ContentHash() && supply.TreeMembers().Known() {
				request.Members = supply.TreeMembers()
				request.VerifiedRelease = artifact.PathHashes{Source: identity.ContentHash(), Installed: supply.InstalledHash()}
			}
			continue
		}
		if !request.Members.Known() {
			request.Members = supply.TreeMembers()
		}
		request.MembershipOnly = request.MembershipOnly && supply.InstalledHash() == state.ContentHash()
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
		request := liveobserve.ManagedPathRequest{Subject: state.Subject(), Destination: state.Destination(), ContentKind: state.ContentKind(), Members: state.TreeMembers()}
		if contract, found := locked.Locked.Subject(state.Subject()); found {
			if desired, ok := contract.Realization(); ok {
				if projection, ok := desired.ManagedPathProjection(); ok {
					request.MembershipOnly = projection.Destination() == state.Destination() && projection.Scope() == state.Scope() && slices.Equal(projection.ConsumerTargets(), state.ConsumerTargets())
				}
			}
		}
		inputs.requests = append(inputs.requests, request)
		inputs.ownership = append(inputs.ownership, ownershipobserve.ManagedPathInput{
			Scope: state.Scope(), Destination: state.Destination(), ConsumerTargets: state.ConsumerTargets(),
		})
	}
	return inputs, nil
}

func selectedManagedPathConsumers(consumers []target.Target, selection targetselection.Selection) bool {
	return slices.ContainsFunc(consumers, selection.Includes)
}
