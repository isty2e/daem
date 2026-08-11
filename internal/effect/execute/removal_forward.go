package execute

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

type forwardRemovalReservation struct {
	state    recovery.RemovalState
	capacity recovery.ForwardRemovalCapacity
	consumed bool
}

type forwardRemovalCertificate struct {
	relation removalRelationKey
	state    recovery.RemovalState
	measure  func(context.Context, recovery.ArtifactWork) (recovery.ArtifactWork, error)
}

func (authority *mutationAuthority) prepareApplyForwardRemovals(
	ctx context.Context,
	managed []ManagedPathEffect,
	aggregates []AggregateEffect,
	payloads payload.PayloadSet,
) error {
	if authority == nil || !authority.removalBindingsPrepared || authority.physicalWorkBudget == nil {
		return fmt.Errorf("forward removal requires prepared physical bindings")
	}
	certificates, err := applyForwardRemovalCertificates(managed, aggregates, payloads, authority.removalDemands)
	if err != nil {
		return err
	}
	return authority.prepareForwardRemovalReservations(ctx, certificates)
}

func applyForwardRemovalCertificates(
	managed []ManagedPathEffect,
	aggregates []AggregateEffect,
	payloads payload.PayloadSet,
	demands recovery.RemovalDemandSet,
) ([]forwardRemovalCertificate, error) {
	certificates := make([]forwardRemovalCertificate, 0, len(managed)+len(aggregates))
	for index, effect := range managed {
		if !effect.RequiresPayload() {
			continue
		}
		state, err := expectedManagedPathRemovalState(effect)
		if err != nil {
			return nil, fmt.Errorf("managed effect[%d] expected removal state: %w", index, err)
		}
		relation := removalRelationKey{scope: effect.Scope(), destination: effect.Destination()}
		if !removalStateIsDemanded(demands, relation, state) {
			continue
		}
		stateKind, _, err := removalStateKind(state)
		if err != nil {
			return nil, fmt.Errorf("managed effect[%d] forward removal state: %w", index, err)
		}
		prepared, present := payloads.LookupSubject(effect.Subject())
		if !present {
			return nil, fmt.Errorf(
				"managed effect[%d] forward removal payload is missing for subject %s/%s %q",
				index,
				effect.Subject().Kind(),
				effect.Subject().Namespace(),
				effect.Subject().Key(),
			)
		}
		if file, isFile := prepared.File(); isFile && stateKind == recovery.PathKindFile {
			contentBytes := int64(len(file.Bytes()))
			certificates = append(certificates, constantForwardRemovalCertificate(
				relation,
				state,
				0,
				contentBytes,
			))
			continue
		}
		verified, err := managedPathPayload(effect, payloads)
		if err != nil {
			return nil, fmt.Errorf("managed effect[%d] forward removal directory payload: %w", index, err)
		}
		directory, isDirectory := verified.Directory()
		if !isDirectory {
			return nil, fmt.Errorf("managed effect[%d] forward removal payload has no content variant", index)
		}
		certificates = append(certificates, forwardRemovalCertificate{
			relation: relation,
			state:    state,
			measure: func(ctx context.Context, maximum recovery.ArtifactWork) (recovery.ArtifactWork, error) {
				return measureDirectoryArtifactRemovalWork(
					ctx,
					directory.View(),
					directory.Identity(),
					maximum,
				)
			},
		})
	}
	for index, effect := range aggregates {
		if effect.Kind() != AggregateEffectCreate {
			continue
		}
		state, err := aggregateDocumentExpectedRemovalState(effect)
		if err != nil {
			return nil, fmt.Errorf("aggregate effect[%d] expected removal state: %w", index, err)
		}
		relation := removalRelationKey{scope: effect.Scope(), destination: effect.Destination()}
		if !removalStateIsDemanded(demands, relation, state) {
			continue
		}
		document := effect.Rendered().Document()
		if !document.Exists() {
			continue
		}
		certificates = append(certificates, constantForwardRemovalCertificate(
			relation,
			state,
			0,
			int64(len(document.Content())),
		))
	}
	return certificates, nil
}

func constantForwardRemovalCertificate(
	relation removalRelationKey,
	state recovery.RemovalState,
	entries int,
	bytes int64,
) forwardRemovalCertificate {
	return forwardRemovalCertificate{
		relation: relation,
		state:    state,
		measure: func(context.Context, recovery.ArtifactWork) (recovery.ArtifactWork, error) {
			return recovery.NewArtifactWork(entries, bytes)
		},
	}
}

func removalStateIsDemanded(
	demands recovery.RemovalDemandSet,
	relation removalRelationKey,
	state recovery.RemovalState,
) bool {
	for _, demand := range demands.Demands() {
		if demand.Scope() != relation.scope || demand.Destination() != relation.destination {
			continue
		}
		for _, admitted := range demand.States() {
			if admitted.Equal(state) {
				return true
			}
		}
	}
	return false
}

func measureDirectoryArtifactRemovalWork(
	ctx context.Context,
	view access.View,
	identity artifact.ExactIdentity,
	maximum recovery.ArtifactWork,
) (recovery.ArtifactWork, error) {
	maximumEntries := min(maximum.Entries(), recovery.MaximumArtifactTreeEntries)
	maximumBytes := min(maximum.Bytes(), recovery.MaximumArtifactTreeBytes)
	traversal, err := access.NewTraversalLimit(
		uint64(maximumEntries+1),
		max(int64(1), maximumBytes),
	)
	if err != nil {
		return recovery.ArtifactWork{}, err
	}
	structure, err := access.NewTreeStructureLimit(
		max(1, maximumEntries),
		recovery.MaximumArtifactTreeDepth,
	)
	if err != nil {
		return recovery.ArtifactWork{}, err
	}
	measurement, err := view.MeasureVerifiedDirectory(
		ctx,
		identity,
		traversal,
		structure,
	)
	if err != nil {
		return recovery.ArtifactWork{}, err
	}
	return recovery.NewArtifactWork(
		measurement.DescendantEntries(),
		measurement.RegularFileBytes(),
	)
}

func (authority *mutationAuthority) prepareForwardRemovalReservations(
	ctx context.Context,
	certificates []forwardRemovalCertificate,
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
	certificatesByRelation := make(map[removalRelationKey][]forwardRemovalCertificate, len(certificates))
	for _, certificate := range certificates {
		values := certificatesByRelation[certificate.relation]
		duplicate := false
		for _, existing := range values {
			if existing.state.Equal(certificate.state) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			certificatesByRelation[certificate.relation] = append(values, certificate)
		}
	}

	reservations := make(map[removalRelationKey][]forwardRemovalReservation, authority.removalDemands.Len())
	for _, demand := range authority.removalDemands.Demands() {
		relation := removalRelationKey{scope: demand.Scope(), destination: demand.Destination()}
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
			kind, exists, err := removalStateKind(state)
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
				work, err = certificate.measure(ctx, authority.physicalWorkBudget.RemainingTreeWork())
				if err == nil {
					err = authority.physicalWorkBudget.AdmitTree(work)
				}
			}
			if err != nil {
				return fmt.Errorf("establish forward removal capacity for %q: %w", demand.Destination(), err)
			}
			capacity, err := authority.physicalWorkBudget.ReserveForwardRemoval(
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

func (authority *mutationAuthority) reserveForwardRemovalExecutionBudget(
	reservations map[removalRelationKey][]forwardRemovalReservation,
) (*recovery.PhysicalWorkBudget, error) {
	if authority == nil {
		return nil, fmt.Errorf("forward removal authority is unavailable")
	}
	if authority.physicalWorkBudget == nil {
		return nil, fmt.Errorf("forward removal operation budget is unavailable")
	}
	destinationPaths := make([]string, 0)
	for _, demand := range authority.removalDemands.Demands() {
		relation := removalRelationKey{scope: demand.Scope(), destination: demand.Destination()}
		relationReservations := reservations[relation]
		destination, present := authority.removalDestinations[relation]
		if !present || !destination.isRooted() {
			return nil, fmt.Errorf("forward removal relation %q has no physical destination", relation.destination)
		}
		for range relationReservations {
			destinationPaths = append(destinationPaths, destination.hostPath)
		}
	}
	if err := journal.ReserveForwardRemovalExecutionWork(
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

func removalStateKind(state recovery.RemovalState) (string, bool, error) {
	if before, present := state.Before(); present {
		return before.Kind, before.Existed, nil
	}
	if expected, present := state.Expected(); present {
		return expected.Kind, expected.Existed, nil
	}
	return "", false, fmt.Errorf("removal state is empty")
}

func findForwardRemovalCertificate(
	certificates []forwardRemovalCertificate,
	state recovery.RemovalState,
) (forwardRemovalCertificate, bool, error) {
	var result forwardRemovalCertificate
	found := false
	for _, certificate := range certificates {
		if !certificate.state.Equal(state) {
			continue
		}
		if found {
			return forwardRemovalCertificate{}, false, fmt.Errorf(
				"forward removal state has duplicate writer certificates",
			)
		}
		result = certificate
		found = true
	}
	return result, found, nil
}

func (authority *mutationAuthority) observeForwardRemovalCurrent(
	ctx context.Context,
	relation removalRelationKey,
	states []recovery.RemovalState,
	certificates []forwardRemovalCertificate,
) (recovery.RemovalState, recovery.ArtifactWork, bool, error) {
	destination, present := authority.removalDestinations[relation]
	if !present || !destination.isRooted() {
		return recovery.RemovalState{}, recovery.ArtifactWork{}, false, fmt.Errorf(
			"forward removal destination is not physically bound",
		)
	}
	capability, err := destination.root.AcquireBounded(
		destination.destination,
		recovery.MaximumPhysicalPathDepth,
		authority.physicalWorkBudget,
	)
	if err != nil {
		return recovery.RemovalState{}, recovery.ArtifactWork{}, false, err
	}
	observation, _, work, observeErr := journal.ObserveRootedRemovalEntry(
		ctx,
		authority.filesystem,
		capability,
		authority.physicalWorkBudget,
		authority.physicalWorkBudget.RemainingTreeWork(),
	)
	closeErr := capability.Close()
	if observeErr != nil || closeErr != nil {
		return recovery.RemovalState{}, recovery.ArtifactWork{}, false, errors.Join(observeErr, closeErr)
	}
	if observation.Status() == recovery.RemovalResidueEntryAbsent {
		return recovery.RemovalState{}, recovery.ArtifactWork{}, false, nil
	}
	if observation.Status() != recovery.RemovalResidueEntryPresent {
		return recovery.RemovalState{}, recovery.ArtifactWork{}, false, fmt.Errorf(
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
		return recovery.RemovalState{}, recovery.ArtifactWork{}, false, fmt.Errorf(
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

func (authority *mutationAuthority) beginForwardRemoval(
	ctx context.Context,
	destination mutationDestination,
	capability rootedpath.CommitCapability,
	expected mutationfs.EntryIdentity,
	intent recovery.RemovalIntent,
) (mutationfs.TreeTraversalLimits, error) {
	if !authority.forwardRemovalPrepared {
		return mutationfs.TreeTraversalLimits{}, fmt.Errorf("forward removal capacity was not prepared before effects")
	}
	relation := removalRelationKey{scope: destination.scope, destination: destination.logical}
	reservations := authority.forwardRemovalReservations[relation]
	var envelope recovery.ForwardRemovalCapacity
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
	observation, observedIdentity, work, err := journal.ObserveRootedRemovalEntry(
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
