package execute

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
)

type ownershipMutationPlan struct {
	transitions []ownershipmutation.ClaimTransition
	intents     []ownership.ProvisionalAcquireIntent
}

type ownershipMutationState struct {
	transitions        []ownershipmutation.ClaimTransition
	provisional        []ownership.ProvisionalAcquireIntent
	journalFingerprint string
}

func newOwnershipMutationState(plans ...ownershipMutationPlan) ownershipMutationState {
	state := ownershipMutationState{}
	for _, plan := range plans {
		state.transitions = append(state.transitions, plan.transitions...)
		state.provisional = append(state.provisional, plan.intents...)
	}
	return state
}

func (state ownershipMutationState) hasMutations() bool {
	return len(state.transitions) != 0 || len(state.provisional) != 0
}

func (state *ownershipMutationState) setJournalFingerprint(fingerprint string) error {
	if state == nil {
		return fmt.Errorf("ownership mutation state is required")
	}
	if fingerprint == "" {
		return fmt.Errorf("ownership mutation journal fingerprint is required")
	}
	state.journalFingerprint = fingerprint
	return nil
}

func ownershipPlanForManagedPathEffects(
	effects []ManagedPathEffect,
	owner stateauthority.Authority,
	observations []observe.OwnershipObservation,
	operationID string,
) (ownershipMutationPlan, error) {
	byOutput, err := ownershipObservationsByOutput(observations)
	if err != nil {
		return ownershipMutationPlan{}, err
	}
	plan := ownershipMutationPlan{
		transitions: make([]ownershipmutation.ClaimTransition, 0, len(effects)+1),
		intents:     make([]ownership.ProvisionalAcquireIntent, 0),
	}
	for _, effect := range effects {
		previous, hasPrevious := effect.PreviousState()
		if hasPrevious && (previous.Scope() != effect.Scope() || previous.Destination() != effect.Destination()) {
			if previous.Scope() == target.ScopeGlobal {
				old, present := byOutput[ownershipOutputKey{destination: previous.Destination()}]
				if !present {
					return ownershipMutationPlan{}, fmt.Errorf("previous global managed path %q has no ownership observation", previous.Destination())
				}
				oldClaim, err := exactActiveOwnershipClaim(old, owner)
				if err != nil {
					return ownershipMutationPlan{}, fmt.Errorf("previous global managed path %q: %w", previous.Destination(), err)
				}
				release, err := ownershipmutation.NewReleaseTransition(oldClaim)
				if err != nil {
					return ownershipMutationPlan{}, err
				}
				plan.transitions = append(plan.transitions, release)
			}
			if effect.Scope() == target.ScopeGlobal {
				current, present := byOutput[ownershipOutputKey{destination: effect.Destination()}]
				if !present {
					return ownershipMutationPlan{}, fmt.Errorf("new global managed path %q has no ownership observation", effect.Destination())
				}
				if err := appendOwnershipAcquire(&plan, current, owner, operationID); err != nil {
					return ownershipMutationPlan{}, fmt.Errorf("new global managed path %q: %w", effect.Destination(), err)
				}
			}
			continue
		}
		if effect.Scope() != target.ScopeGlobal {
			continue
		}
		current, present := byOutput[ownershipOutputKey{destination: effect.Destination()}]
		if !present {
			return ownershipMutationPlan{}, fmt.Errorf("global managed path effect for %q has no ownership observation", effect.Destination())
		}

		if !hasPrevious {
			if err := appendOwnershipAcquire(&plan, current, owner, operationID); err != nil {
				return ownershipMutationPlan{}, fmt.Errorf("new global managed path %q: %w", effect.Destination(), err)
			}
			continue
		}
		claim, err := exactActiveOwnershipClaim(current, owner)
		if err != nil {
			return ownershipMutationPlan{}, fmt.Errorf("existing global managed path %q: %w", effect.Destination(), err)
		}
		var transition ownershipmutation.ClaimTransition
		if effect.Kind() == ManagedPathEffectRemove {
			transition, err = ownershipmutation.NewReleaseTransition(claim)
		} else {
			transition, err = ownershipmutation.NewRetainTransition(claim)
		}
		if err != nil {
			return ownershipMutationPlan{}, err
		}
		plan.transitions = append(plan.transitions, transition)
	}
	return plan, nil
}

func ownershipPlanForAggregateEffects(
	effects []AggregateEffect,
	owner stateauthority.Authority,
	observations []observe.OwnershipObservation,
	operationID string,
) (ownershipMutationPlan, error) {
	byOutput, err := ownershipObservationsByOutput(observations)
	if err != nil {
		return ownershipMutationPlan{}, err
	}
	plan := ownershipMutationPlan{}
	for _, effect := range effects {
		document := effect.DocumentAddress()
		if document.Scope() != target.ScopeGlobal {
			continue
		}
		for _, projection := range effect.projections {
			if len(projection.subjects) == 0 {
				continue
			}
			address := projection.contract.Address()
			key := ownershipOutputKey{
				destination: document.AggregateRoot(),
				contentPath: output.ContentPath(address.ContentPath()),
			}
			observation, present := byOutput[key]
			if !present {
				return ownershipMutationPlan{}, fmt.Errorf(
					"global aggregate projection for %q content path %q has no ownership observation",
					key.destination,
					key.contentPath,
				)
			}

			if len(projection.previous) == 0 {
				if err := appendOwnershipAcquire(&plan, observation, owner, operationID); err != nil {
					return ownershipMutationPlan{}, fmt.Errorf(
						"new global aggregate projection for %q content path %q: %w",
						key.destination,
						key.contentPath,
						err,
					)
				}
				continue
			}
			claim, err := exactActiveOwnershipClaim(observation, owner)
			if err != nil {
				return ownershipMutationPlan{}, fmt.Errorf(
					"existing global aggregate projection for %q content path %q: %w",
					key.destination,
					key.contentPath,
					err,
				)
			}
			if projection.kind == AggregateEffectRecord {
				continue
			}

			var transition ownershipmutation.ClaimTransition
			if projection.kind == AggregateEffectRemove {
				transition, err = ownershipmutation.NewReleaseTransition(claim)
			} else {
				transition, err = ownershipmutation.NewRetainTransition(claim)
			}
			if err != nil {
				return ownershipMutationPlan{}, err
			}
			plan.transitions = append(plan.transitions, transition)
		}
	}
	return plan, nil
}

type ownershipOutputKey struct {
	destination output.Destination
	contentPath output.ContentPath
}

func provisionalAcquireKeysForManagedPath(
	effect ManagedPathEffect,
	phase managedPathPhase,
) []ownershipOutputKey {
	if phase != managedPathPublishPhase || effect.Scope() != target.ScopeGlobal ||
		effect.Kind() == ManagedPathEffectRemove || effect.Kind() == ManagedPathEffectRecord {
		return nil
	}
	previous, present := effect.PreviousState()
	if present && previous.Scope() == effect.Scope() && previous.Destination() == effect.Destination() {
		return nil
	}
	return []ownershipOutputKey{{destination: effect.Destination()}}
}

func provisionalAcquireKeysForAggregate(effect AggregateEffect) []ownershipOutputKey {
	if effect.Scope() != target.ScopeGlobal || effect.Kind() == AggregateEffectRecord {
		return nil
	}
	keys := make([]ownershipOutputKey, 0, len(effect.projections))
	for _, projection := range effect.projections {
		if len(projection.subjects) == 0 || len(projection.previous) != 0 {
			continue
		}
		keys = append(keys, ownershipOutputKey{
			destination: effect.Destination(),
			contentPath: output.ContentPath(projection.contract.Address().ContentPath()),
		})
	}
	return keys
}

func ownershipObservationsByOutput(
	observations []observe.OwnershipObservation,
) (map[ownershipOutputKey]observe.OwnershipObservation, error) {
	byOutput := make(map[ownershipOutputKey]observe.OwnershipObservation, len(observations))
	for index, observation := range observations {
		if err := observation.Validate(); err != nil {
			return nil, fmt.Errorf("execution ownership observation[%d]: %w", index, err)
		}
		key := ownershipOutputKey{
			destination: observation.Destination(),
			contentPath: observation.ContentPath(),
		}
		if _, exists := byOutput[key]; exists {
			return nil, fmt.Errorf(
				"duplicate execution ownership observation for %q content path %q",
				key.destination,
				key.contentPath,
			)
		}
		byOutput[key] = observation
	}
	return byOutput, nil
}

func appendOwnershipAcquire(
	plan *ownershipMutationPlan,
	observation observe.OwnershipObservation,
	owner stateauthority.Authority,
	operationID string,
) error {
	if plan == nil {
		return fmt.Errorf("ownership mutation plan is required")
	}
	if _, claimed := observation.Claim().Get(); claimed {
		return fmt.Errorf("new global output unexpectedly has an ownership claim")
	}
	if address, exact := observation.ExactAddress(); exact {
		transition, err := ownershipmutation.NewAcquireTransition(address, owner, operationID)
		if err != nil {
			return err
		}
		plan.transitions = append(plan.transitions, transition)
		return nil
	}
	provisional, present := observation.ProvisionalPath()
	if !present {
		return fmt.Errorf("new global output has neither exact nor provisional authority")
	}
	intent, err := ownership.NewProvisionalAcquireIntent(
		observation.Destination(),
		observation.ContentPath(),
		provisional,
		owner,
		operationID,
	)
	if err != nil {
		return err
	}
	plan.intents = append(plan.intents, intent)
	return nil
}

func (state *ownershipMutationState) promoteVisibleAcquires(
	ctx context.Context,
	keys []ownershipOutputKey,
	authority *mutationAuthority,
	registryStore ownershipmutation.RegistryStore,
	stateCodec durable.SnapshotCodec,
	gate visibilityEffectGate,
) error {
	if state == nil || len(state.provisional) == 0 || len(keys) == 0 {
		return nil
	}
	requested := make(map[ownershipOutputKey]struct{}, len(keys))
	for _, key := range keys {
		requested[key] = struct{}{}
	}
	for index := 0; index < len(state.provisional); {
		intent := state.provisional[index]
		key := ownershipOutputKey{
			destination: intent.Destination(),
			contentPath: intent.ContentPath(),
		}
		if _, selected := requested[key]; !selected {
			index++
			continue
		}
		transition, fingerprint, promoted, err := promoteVisibleAcquire(
			ctx,
			intent,
			state.journalFingerprint,
			authority,
			registryStore,
			stateCodec,
			gate,
		)
		if err != nil {
			return fmt.Errorf(
				"promote ownership for %q content path %q: %w",
				intent.Destination(),
				intent.ContentPath(),
				err,
			)
		}
		if !promoted {
			index++
			continue
		}
		state.journalFingerprint = fingerprint
		state.transitions = append(state.transitions, transition)
		state.provisional = append(state.provisional[:index], state.provisional[index+1:]...)
	}
	return nil
}

func promoteVisibleAcquire(
	ctx context.Context,
	intent ownership.ProvisionalAcquireIntent,
	journalFingerprint string,
	authority *mutationAuthority,
	registryStore ownershipmutation.RegistryStore,
	stateCodec durable.SnapshotCodec,
	gate visibilityEffectGate,
) (ownershipmutation.ClaimTransition, string, bool, error) {
	if authority == nil || authority.recoveryJournalRecord == nil {
		return ownershipmutation.ClaimTransition{}, "", false, fmt.Errorf("recovery journal record authority is unavailable")
	}
	if registryStore == nil {
		return ownershipmutation.ClaimTransition{}, "", false, fmt.Errorf("ownership registry is unavailable")
	}
	if stateCodec == nil {
		return ownershipmutation.ClaimTransition{}, "", false, fmt.Errorf("state codec is unavailable")
	}
	destination, err := authority.resolveBoundDestination(target.ScopeGlobal, intent.Destination())
	if err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	observed, err := mutation.ObserveDirectoryEntryAuthority(destination.hostPath)
	if err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	exact, visible := observed.Exact()
	if !visible {
		if _, provisional := observed.Provisional(); provisional {
			return ownershipmutation.ClaimTransition{}, journalFingerprint, false, nil
		}
		return ownershipmutation.ClaimTransition{}, "", false, fmt.Errorf("visible ownership authority observation is empty")
	}
	address, err := ownership.NewManagedAddress(exact, string(intent.ContentPath()))
	if err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	transition, err := ownershipmutation.NewAcquireTransitionFromIntent(intent, address)
	if err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	registry, err := registryStore.Load(ctx)
	if err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	if claim, conflict := registry.Conflict(address); conflict {
		actual, valueErr := ownership.PresentClaim(claim)
		if valueErr != nil {
			return ownershipmutation.ClaimTransition{}, "", false, valueErr
		}
		return ownershipmutation.ClaimTransition{}, "", false, &ownership.StaleClaimError{
			Address:  address,
			Expected: ownership.NoClaim(),
			Actual:   actual,
		}
	}
	if err := gate.validateBefore(ctx); err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	promotion, err := journal.PromoteProvisionalAcquire(
		ctx,
		authority.filesystem,
		authority.recoveryJournalRecord,
		authority.recoveryJournal,
		authority.activeJournalAuthority,
		journalFingerprint,
		intent,
		transition,
		stateCodec,
	)
	if refreshed, available := promotion.ActiveJournalAuthority(); available {
		if refreshErr := authority.setActiveJournalAuthority(refreshed); refreshErr != nil {
			err = errors.Join(err, fmt.Errorf("refresh active recovery journal authority: %w", refreshErr))
		}
	}
	if err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	if err := gate.acceptAfter(ctx); err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	if err := gate.validateBefore(ctx); err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	if err := convergeClaim(
		ctx,
		registryStore,
		transition.Address(),
		transition.Before(),
		transition.Prepared(),
	); err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	if err := gate.acceptAfter(ctx); err != nil {
		return ownershipmutation.ClaimTransition{}, "", false, err
	}
	return transition, promotion.RecordFingerprint(), true, nil
}

func exactActiveOwnershipClaim(
	observation observe.OwnershipObservation,
	owner stateauthority.Authority,
) (ownership.Claim, error) {
	address, exact := observation.ExactAddress()
	if !exact {
		return ownership.Claim{}, fmt.Errorf("lacks exact path authority")
	}
	claim, claimed := observation.Claim().Get()
	if !claimed || !claim.Address().Equal(address) ||
		!claim.OwnedBy(owner) || claim.State() != ownership.ClaimActive {
		return ownership.Claim{}, fmt.Errorf("lacks its exact active ownership claim")
	}
	return claim, nil
}

func prepareClaimTransitions(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	transitions []ownershipmutation.ClaimTransition,
	forwardGate visibilityEffectGate,
) error {
	if len(transitions) == 0 {
		return nil
	}
	set, err := ownershipmutation.NewClaimTransitionSet(transitions)
	if err != nil {
		return err
	}
	convergence, err := set.Preparation()
	if err != nil {
		return err
	}
	return executeClaimConvergence(
		ctx,
		registryStore,
		convergence,
		forwardGate,
		"validate ownership claim preparation",
		"prepare ownership claim",
		"accept prepared ownership claim",
	)
}

func finalizeClaimTransitions(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	transitions []ownershipmutation.ClaimTransition,
	gate visibilityEffectGate,
) error {
	if len(transitions) == 0 {
		return nil
	}
	set, err := ownershipmutation.NewClaimTransitionSet(transitions)
	if err != nil {
		return err
	}
	convergence, err := set.Finalization()
	if err != nil {
		return err
	}
	return executeClaimConvergence(
		ctx,
		registryStore,
		convergence,
		gate,
		"validate ownership claim finalization",
		"finalize ownership claim",
		"accept finalized ownership claim",
	)
}

func rollbackClaimsToBefore(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	transitions []ownershipmutation.ClaimTransition,
	gate visibilityEffectGate,
) error {
	if len(transitions) == 0 {
		return nil
	}
	return rollbackClaimTransitions(ctx, registryStore, transitions, gate)
}

func rollbackClaimTransitions(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	transitions []ownershipmutation.ClaimTransition,
	gate visibilityEffectGate,
) error {
	if len(transitions) == 0 {
		return nil
	}
	set, err := ownershipmutation.NewClaimTransitionSet(transitions)
	if err != nil {
		return err
	}
	convergence, err := set.Rollback()
	if err != nil {
		return err
	}
	return executeClaimConvergence(
		ctx,
		registryStore,
		convergence,
		gate,
		"validate ownership claim rollback",
		"rollback ownership claim",
		"accept rolled back ownership claim",
	)
}

func executeClaimConvergence(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	convergence ownership.ClaimConvergence,
	gate visibilityEffectGate,
	validateDetail string,
	commitDetail string,
	acceptDetail string,
) error {
	if registryStore == nil {
		return fmt.Errorf("ownership registry is unavailable")
	}
	if err := gate.validateBefore(ctx); err != nil {
		return fmt.Errorf("%s: %w", validateDetail, err)
	}
	if _, err := registryStore.Converge(ctx, convergence); err != nil {
		return fmt.Errorf("%s: %w", commitDetail, err)
	}
	if err := gate.acceptAfter(ctx); err != nil {
		return fmt.Errorf("%s: %w", acceptDetail, err)
	}
	return nil
}

func convergeClaim(
	ctx context.Context,
	registryStore ownershipmutation.RegistryStore,
	address ownership.ManagedAddress,
	from ownership.ClaimValue,
	to ownership.ClaimValue,
) error {
	change, err := ownership.NewClaimChange(address, from, to)
	if err != nil {
		return err
	}
	convergence, err := ownership.NewClaimConvergence([]ownership.ClaimChange{change})
	if err != nil {
		return err
	}
	_, err = registryStore.Converge(ctx, convergence)
	return err
}

func (authority *mutationAuthority) bindOwnershipRegistry(path string) error {
	if authority == nil {
		return fmt.Errorf("mutation authority is required")
	}
	if authority.hasOwnershipRegistry {
		existingKey, existingErr := mutation.CanonicalDirectoryEntryKey(authority.ownershipRegistry.Path())
		requestedKey, requestedErr := mutation.CanonicalDirectoryEntryKey(path)
		if existingErr != nil || requestedErr != nil {
			return errors.Join(existingErr, requestedErr)
		}
		if existingKey != requestedKey {
			return fmt.Errorf("ownership registry has conflicting physical bindings")
		}
		return nil
	}
	if authority.ownershipRegistryBinder == nil {
		return fmt.Errorf("rooted ownership registry binder is required")
	}
	root, destination, err := rootedpath.CaptureDestination(path)
	if err != nil {
		return fmt.Errorf("capture ownership registry authority: %w", err)
	}
	root, err = authority.retainGlobalRoot(root, destination)
	if err != nil {
		return err
	}
	registry, err := authority.ownershipRegistryBinder(root, destination)
	if err != nil {
		return err
	}
	if registry == nil {
		return fmt.Errorf("rooted ownership registry binder returned a nil store")
	}
	authority.ownershipRegistry = registry
	authority.hasOwnershipRegistry = true
	return nil
}

func (authority *mutationAuthority) rootedOwnershipRegistry() (ownershipmutation.RegistryStore, error) {
	if authority == nil || !authority.hasOwnershipRegistry {
		return nil, fmt.Errorf("rooted ownership registry authority is unavailable")
	}
	return authority.ownershipRegistry, nil
}

func (authority *mutationAuthority) rootedOwnershipRegistryOption() ownershipmutation.RegistryReader {
	if authority == nil || !authority.hasOwnershipRegistry {
		return nil
	}
	return authority.ownershipRegistry
}
