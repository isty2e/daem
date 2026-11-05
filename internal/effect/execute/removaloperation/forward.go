package removaloperation

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"

	"github.com/isty2e/daem/internal/effect/journal/removalnamespace"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	"github.com/isty2e/daem/internal/effect/journal/recovery"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

type forwardRemovalReservation struct {
	state    recovery.RemovalState
	capacity physicalwork.ForwardRemovalCapacity
	consumed bool
}

type ForwardCandidate struct {
	Relation Relation
	State    recovery.RemovalState
	Measure  func(context.Context, workmeasure.ArtifactWork) (workmeasure.ArtifactWork, error)
}

func (authority *Operation) PrepareApplyForward(
	ctx context.Context,
	produce func(recovery.RemovalDemandSet) ([]ForwardCandidate, error),
) error {
	if authority == nil || !authority.removalBindingsPrepared || authority.physicalWorkBudget == nil {
		return fmt.Errorf("forward removal requires prepared physical bindings")
	}
	certificates, err := produce(authority.removalDemands)
	if err != nil {
		return err
	}
	return authority.PrepareForward(ctx, certificates)
}

func (authority *Operation) PrepareForwardFor(
	ctx context.Context,
	demands recovery.RemovalDemandSet,
	certificates []ForwardCandidate,
) error {
	if authority == nil {
		return fmt.Errorf("forward removal authority is unavailable")
	}
	if authority.forwardRemovalPrepared {
		if authority.forwardRemovalExecution == nil {
			return fmt.Errorf("forward removal preparation is incomplete")
		}
		return nil
	}
	if ctx == nil {
		return fmt.Errorf("forward removal preparation context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	certificatesByRelation := make(map[Relation][]ForwardCandidate, len(certificates))
	for index, certificate := range certificates {
		if certificate.Measure == nil {
			return fmt.Errorf("forward removal candidate[%d] has no work measurement", index)
		}
		if err := certificate.Relation.Destination.ValidateScope(certificate.Relation.Scope); err != nil {
			return fmt.Errorf("forward removal candidate[%d] relation: %w", index, err)
		}
		if _, _, err := certificate.State.Kind(); err != nil {
			return fmt.Errorf("forward removal candidate[%d] state: %w", index, err)
		}
		values := certificatesByRelation[certificate.Relation]
		duplicate := false
		for _, existing := range values {
			if existing.State.Equal(certificate.State) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			certificatesByRelation[certificate.Relation] = append(values, certificate)
		}
	}

	reservations := make(map[Relation][]forwardRemovalReservation, authority.removalDemands.Len())
	for _, demand := range demands.Demands() {
		relation := Relation{Scope: demand.Scope(), Destination: demand.Destination()}
		destination, present := authority.removalDestinations[relation]
		if !present || !destination.IsRooted() {
			return fmt.Errorf("forward removal relation %q has no physical destination", relation.Destination)
		}
		states := demand.States()
		currentState, currentWork, currentPresent, err := authority.observeForwardRemovalCurrent(
			ctx,
			relation,
			states,
			certificatesByRelation[relation],
		)
		if err != nil {
			return fmt.Errorf("observe forward removal state for %q: %w", demand.Destination(), err)
		}
		for _, state := range states {
			kind, exists, err := state.Kind()
			if err != nil {
				return fmt.Errorf("forward removal state for %q: %w", demand.Destination(), err)
			}
			if !exists {
				continue
			}
			work := currentWork
			if !currentPresent || !state.Equal(currentState) {
				certificate, found, findErr := findForwardRemovalCertificate(
					certificatesByRelation[relation],
					state,
				)
				if findErr != nil {
					return findErr
				}
				if !found {
					// A durable demand may contain states that are neither current nor
					// producible by this execution direction. They need no capacity;
					// beginForwardRemoval still rejects any unreserved candidate.
					continue
				}
				work, err = certificate.Measure(ctx, authority.physicalWorkBudget.RemainingTreeWork())
				if err == nil {
					err = authority.physicalWorkBudget.AdmitTree(work)
				}
			}
			if err != nil {
				return fmt.Errorf("establish forward removal capacity for %q: %w", demand.Destination(), err)
			}
			capacity, err := removalnamespace.ReserveForwardRootedCleanupWork(
				authority.physicalWorkBudget,
				destination.RootedPath(),
				work,
				kind == recovery.PathKindDirectory,
			)
			if err != nil {
				return fmt.Errorf("reserve forward removal capacity for %q: %w", demand.Destination(), err)
			}
			reservations[relation] = append(reservations[relation], forwardRemovalReservation{
				state: state, capacity: capacity,
			})
		}
	}
	execution, err := authority.reserveForwardRemovalExecutionBudget(reservations)
	if err != nil {
		return err
	}
	authority.forwardRemovalReservations = reservations
	authority.forwardRemovalExecution = execution
	authority.forwardRemovalPrepared = true
	return nil
}

func (authority *Operation) reserveForwardRemovalExecutionBudget(
	reservations map[Relation][]forwardRemovalReservation,
) (*physicalwork.Budget, error) {
	if authority == nil {
		return nil, fmt.Errorf("forward removal authority is unavailable")
	}
	if authority.physicalWorkBudget == nil {
		return nil, fmt.Errorf("forward removal operation budget is unavailable")
	}
	destinationPaths := make([]string, 0)
	for _, demand := range authority.removalDemands.Demands() {
		relation := Relation{Scope: demand.Scope(), Destination: demand.Destination()}
		relationReservations := reservations[relation]
		destination, present := authority.removalDestinations[relation]
		if !present || !destination.IsRooted() {
			return nil, fmt.Errorf("forward removal relation %q has no physical destination", relation.Destination)
		}
		for range relationReservations {
			destinationPaths = append(destinationPaths, destination.HostPath())
		}
	}
	if err := removalnamespace.ReserveForwardRemovalExecutionWork(
		authority.physicalWorkBudget,
		destinationPaths,
	); err != nil {
		return nil, fmt.Errorf("reserve forward removal path work: %w", err)
	}
	execution, err := authority.physicalWorkBudget.BeginReservedForwardExecution()
	if err != nil {
		return nil, err
	}
	return execution, nil
}

func findForwardRemovalCertificate(
	certificates []ForwardCandidate,
	state recovery.RemovalState,
) (ForwardCandidate, bool, error) {
	var result ForwardCandidate
	found := false
	for _, certificate := range certificates {
		if !certificate.State.Equal(state) {
			continue
		}
		if found {
			return ForwardCandidate{}, false, fmt.Errorf(
				"forward removal state has duplicate writer certificates",
			)
		}
		result = certificate
		found = true
	}
	return result, found, nil
}

func (authority *Operation) observeForwardRemovalCurrent(
	ctx context.Context,
	relation Relation,
	states []recovery.RemovalState,
	certificates []ForwardCandidate,
) (recovery.RemovalState, workmeasure.ArtifactWork, bool, error) {
	destination, present := authority.removalDestinations[relation]
	if !present || !destination.IsRooted() {
		return recovery.RemovalState{}, workmeasure.ArtifactWork{}, false, fmt.Errorf(
			"forward removal destination is not physically bound",
		)
	}
	capability, err := authority.destinations.AcquireBounded(destination, authority.physicalWorkBudget)
	if err != nil {
		return recovery.RemovalState{}, workmeasure.ArtifactWork{}, false, err
	}
	observation, _, work, observeErr := journalqualification.ObserveRootedRemovalEntry(
		ctx,
		authority.filesystem,
		capability,
		authority.physicalWorkBudget,
		authority.physicalWorkBudget.RemainingTreeWork(),
	)
	closeErr := capability.Close()
	if observeErr != nil || closeErr != nil {
		return recovery.RemovalState{}, workmeasure.ArtifactWork{}, false, errors.Join(observeErr, closeErr)
	}
	if observation.Status() == recovery.RemovalResidueEntryAbsent {
		return recovery.RemovalState{}, workmeasure.ArtifactWork{}, false, nil
	}
	if observation.Status() != recovery.RemovalResidueEntryPresent {
		return recovery.RemovalState{}, workmeasure.ArtifactWork{}, false, fmt.Errorf(
			"current entry is not a removable regular file or directory",
		)
	}

	matching := make([]recovery.RemovalState, 0, len(states))
	for _, state := range states {
		if state.AdmitsEntry(observation) {
			matching = append(matching, state)
		}
	}
	if len(matching) == 0 {
		return recovery.RemovalState{}, workmeasure.ArtifactWork{}, false, fmt.Errorf(
			"current entry does not match an admitted removable state",
		)
	}
	for _, state := range matching {
		if _, hasCertificate, _ := findForwardRemovalCertificate(certificates, state); !hasCertificate {
			return state, work, true, nil
		}
	}
	return matching[0], work, true, nil
}

func (authority *Operation) beginForwardRemoval(
	ctx context.Context,
	destination mutationnamespace.Destination,
	capability rootedpath.CommitCapability,
	expected mutationfs.EntryIdentity,
	intent recovery.RemovalIntent,
) (mutationfs.TreeTraversalLimits, error) {
	if !authority.forwardRemovalPrepared {
		return mutationfs.TreeTraversalLimits{}, fmt.Errorf("forward removal capacity was not prepared before effects")
	}
	relation := Relation{Scope: destination.Scope(), Destination: destination.Logical()}
	reservations := authority.forwardRemovalReservations[relation]
	var envelope physicalwork.ForwardRemovalCapacity
	haveEnvelope := false
	for _, reservation := range reservations {
		if reservation.consumed {
			continue
		}
		if !haveEnvelope {
			envelope = reservation.capacity
			haveEnvelope = true
			continue
		}
		var err error
		envelope, err = envelope.Envelope(reservation.capacity)
		if err != nil {
			return mutationfs.TreeTraversalLimits{}, err
		}
	}
	if !haveEnvelope {
		return mutationfs.TreeTraversalLimits{}, fmt.Errorf("forward removal has no unconsumed state reservation")
	}
	observationBudget, err := envelope.BeginObservation()
	if err != nil {
		return mutationfs.TreeTraversalLimits{}, err
	}
	observation, observedIdentity, work, err := journalqualification.ObserveRootedRemovalEntry(
		ctx,
		authority.filesystem,
		capability,
		observationBudget,
		envelope.MaximumWork(),
	)
	if err != nil {
		return mutationfs.TreeTraversalLimits{}, err
	}
	if observation.Status() != recovery.RemovalResidueEntryPresent ||
		observedIdentity == nil || !expected.Equal(observedIdentity) ||
		!intent.AdmitsEntry(observation) {
		return mutationfs.TreeTraversalLimits{}, fmt.Errorf("forward removal candidate changed after capacity reservation")
	}
	matched := -1
	for index := range reservations {
		reservation := reservations[index]
		if reservation.consumed || !reservation.state.AdmitsEntry(observation) {
			continue
		}
		if !reservation.capacity.Admits(work) {
			return mutationfs.TreeTraversalLimits{}, fmt.Errorf("forward removal candidate exceeds its reserved work")
		}
		matched = index
		break
	}
	if matched < 0 {
		return mutationfs.TreeTraversalLimits{}, fmt.Errorf("forward removal candidate has no matching state reservation")
	}
	reservations[matched].consumed = true
	authority.forwardRemovalReservations[relation] = reservations
	return mutationfs.NewTreeTraversalLimits(
		work.Entries(),
		recovery.MaximumArtifactTreeDepth,
		work.Bytes(),
	)
}

func (authority *Operation) PrepareForward(ctx context.Context, certificates []ForwardCandidate) error {
	return authority.PrepareForwardFor(ctx, authority.removalDemands, certificates)
}

func (authority *Operation) PrepareRecoveryForward(
	ctx context.Context,
	produce func(recovery.RemovalDemandSet) ([]ForwardCandidate, error),
) error {
	if authority == nil || !authority.removalBindingsPrepared || authority.physicalWorkBudget == nil {
		return fmt.Errorf("recovery forward removal requires prepared physical bindings")
	}
	if authority.forwardRemovalPrepared {
		if authority.forwardRemovalExecution == nil {
			return fmt.Errorf("recovery forward removal preparation is incomplete")
		}
		return nil
	}
	certificates, err := produce(authority.removalDemands)
	if err != nil {
		return err
	}
	return authority.PrepareForward(ctx, certificates)
}
