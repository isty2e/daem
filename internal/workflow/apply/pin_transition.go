package apply

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
)

func runPinTransition(
	ctx context.Context,
	paths daempaths.Paths,
	authority *statefileEffectAuthority,
	current durable.Snapshot,
	registry durablecarrier.GlobalCarrierClaims,
	item preparedHostRoute,
	options runOptions,
) (state durable.Snapshot, claims durablecarrier.GlobalCarrierClaims, records []durableattempt.HostRouteAttempt, returnErr error) {
	state, claims = current, registry
	transition, present := item.action.PinTransition()
	if !present || !item.action.InvokesHostRoute() {
		return state, claims, nil, fmt.Errorf("Pi pin transition requires an admitted action")
	}
	if err := options.validateBeforeEffects(ctx, mutation.PhysicalAuthoritySet{}); err != nil {
		return state, claims, nil, err
	}
	if err := authority.Ensure(ctx); err != nil {
		return state, claims, nil, err
	}
	binding, err := acquireHostRouteWorkingDirectory(options, paths.ManifestRoot)
	if err != nil {
		return state, claims, nil, err
	}
	defer func() {
		if binding != nil {
			returnErr = errors.Join(returnErr, binding.Close())
		}
	}()

	prepareCorrelation := func(ctx context.Context, budget *physicalwork.Budget) error {
		options.markAttempted()
		if item.action.CarrierIdentity().ClaimStore() == durablecarrier.ClaimStoreGlobalRegistry {
			claims, err = publishGlobalPinTransition(ctx, paths, authority, claims, transition, observerelation.CorrelationResult{}, false, options)
		} else {
			entry, entryErr := authority.EntryForCommit()
			if entryErr != nil {
				return entryErr
			}
			commit := func(entry *rootedpath.EntryAuthority) (durable.Snapshot, error) {
				return execute.CommitReservedProjectPinTransition(ctx, storagecommit.Adapter{}, entry, state, transition, options.physicalStateCodec)
			}
			if budget == nil {
				state, err = commit(entry)
			} else {
				state, err = options.nativeInvocations.commitCorrelation(ctx, budget, commit)
			}
		}
		if err != nil {
			return fmt.Errorf("reserve Pi pin transition: %w", err)
		}
		return authority.Validate(ctx)
	}

	var attempt subprocess.CommandAttemptResult
	var releaseErr error
	if options.nativeInvocations != nil {
		var observed bool
		attempt, observed, err = options.nativeInvocations.execute(ctx, options.HostRouteExecutor, item.command.AttemptRequest(), binding, prepareCorrelation)
		binding = nil
		if err != nil {
			if observed {
				result, classifyErr := assurancehostroute.ClassifyResult(assurancehostroute.ResultInput{
					Subject: item.command.Subject(), RouteRequest: item.command.RouteRequest(), Attempt: observedHostRouteAttempt(attempt),
					Observation: assurancehostroute.ObservationUnavailable(assurancehostroute.ResultReasonObservationUnavailable), RequiredPostcondition: installRelationPostcondition(item.action),
				})
				if classifyErr != nil {
					return state, claims, nil, errors.Join(err, classifyErr)
				}
				record, recordErr := durableAttemptFromHostRouteResult(item.action, result, false)
				if recordErr != nil {
					return state, claims, nil, errors.Join(err, recordErr)
				}
				records = []durableattempt.HostRouteAttempt{record}
			}
			return state, claims, records, errors.Join(err, options.executionGuard.requireDeclarationsCurrent(ctx, "after Pi pin transition"))
		}
	} else {
		if err := prepareCorrelation(ctx, nil); err != nil {
			return state, claims, nil, err
		}
		attempt, releaseErr = executeHostRouteAttempt(ctx, options.HostRouteExecutor, item.command.AttemptRequest(), binding)
		binding = nil
	}
	defer func() {
		returnErr = errors.Join(returnErr, releaseErr, options.executionGuard.requireDeclarationsCurrent(ctx, "after Pi pin transition"))
	}()
	if err := authority.Validate(ctx); err != nil {
		return state, claims, nil, err
	}
	observation := assurancehostroute.ObservationUnavailable(assurancehostroute.ResultReasonObservationUnavailable)
	if options.HostRouteObserver != nil {
		observation = options.HostRouteObserver(ctx, item.command, state.PendingCarrierInstalls(), append(state.ManagedCarrierClaims(), claims.Claims()...))
	}
	result, err := assurancehostroute.ClassifyResult(assurancehostroute.ResultInput{
		Subject: item.command.Subject(), RouteRequest: item.command.RouteRequest(),
		Attempt: observedHostRouteAttempt(attempt), Observation: observation,
		RequiredPostcondition: installRelationPostcondition(item.action),
	})
	if err != nil {
		return state, claims, nil, err
	}
	record, err := durableAttemptFromHostRouteResult(item.action, result, false)
	if err != nil {
		return state, claims, nil, err
	}
	records = []durableattempt.HostRouteAttempt{record}
	if err := validateHostRouteProjectRoot(options, paths.ManifestRoot); err != nil {
		return state, claims, records, err
	}

	correlation, observed := observation.Correlation()
	completed := observed && result.Class() == assurancehostroute.ResultAttemptedObservedPresent && result.PostconditionsSatisfied()
	if completed {
		if item.action.CarrierIdentity().ClaimStore() == durablecarrier.ClaimStoreGlobalRegistry {
			claims, err = publishGlobalPinTransition(ctx, paths, authority, claims, transition, correlation, true, options)
		} else {
			if err := authority.Validate(ctx); err != nil {
				return state, claims, records, err
			}
			entry, entryErr := authority.EntryForCommit()
			if entryErr != nil {
				return state, claims, records, entryErr
			}
			state, err = execute.CommitCompletedProjectPinTransition(ctx, storagecommit.Adapter{}, entry, state, transition, correlation, options.physicalStateCodec)
		}
		if err != nil {
			return state, claims, records, fmt.Errorf("complete Pi pin transition management: %w", err)
		}
	}

	if err := authority.Validate(ctx); err != nil {
		return state, claims, records, err
	}
	entry, err := authority.EntryForCommit()
	if err != nil {
		return state, claims, records, err
	}
	state, err = execute.CommitHostRouteAttempts(ctx, storagecommit.Adapter{}, entry, state, records, options.physicalStateCodec)
	if err != nil {
		return state, claims, records, err
	}
	if err := authority.Validate(ctx); err != nil {
		return state, claims, records, err
	}
	if err := validateHostRouteProjectRoot(options, paths.ManifestRoot); err != nil {
		return state, claims, records, err
	}
	if !completed {
		return state, claims, records, hostRouteFailuresError(records)
	}
	return state, claims, records, nil
}

func publishGlobalPinTransition(
	ctx context.Context,
	paths daempaths.Paths,
	authority *statefileEffectAuthority,
	current durablecarrier.GlobalCarrierClaims,
	transition durablecarrier.PinTransition,
	observation observerelation.CorrelationResult,
	complete bool,
	options runOptions,
) (durablecarrier.GlobalCarrierClaims, error) {
	if err := options.executionGuard.requireDeclarationsCurrent(ctx, "before Pi pin claim publication"); err != nil {
		return current, err
	}
	if err := validateHostRouteProjectRoot(options, paths.ManifestRoot); err != nil {
		return current, err
	}
	if err := authority.Validate(ctx); err != nil {
		return current, err
	}
	store, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		return current, err
	}
	var next, observed durablecarrier.GlobalCarrierClaims
	if complete {
		next, err = current.WithCompletedPinTransition(transition, observation)
		if err == nil {
			observed, err = store.CompletePinTransitionIfCurrent(ctx, current, transition, observation)
		}
	} else {
		next, err = current.WithReservedPinTransition(transition)
		if err == nil {
			observed, err = store.ReservePinTransitionIfCurrent(ctx, current, transition)
		}
	}
	result, err := globalCarrierClaimsAfterPersistence(current, next, observed, err)
	if err != nil {
		return result, err
	}
	if err := authority.Validate(ctx); err != nil {
		return result, err
	}
	if options.acceptVisibilityChanges == nil {
		return result, fmt.Errorf("Pi pin transition registry visibility acceptance is required")
	}
	if err := options.acceptVisibilityChanges(ctx); err != nil {
		return result, err
	}
	if err := validateHostRouteProjectRoot(options, paths.ManifestRoot); err != nil {
		return result, err
	}
	return result, options.executionGuard.requireDeclarationsCurrent(ctx, "after Pi pin claim publication")
}
