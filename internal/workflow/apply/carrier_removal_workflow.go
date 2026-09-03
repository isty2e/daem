package apply

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/configrelation"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/subprocess"
)

// CarrierRemovalObserver returns current exact relation and effect facts for
// one durable pending removal. It must not execute the route or treat durable
// state as current host evidence.
type CarrierRemovalObserver func(
	context.Context,
	durablecarrier.PendingCarrierRemoval,
	[]durablecarrier.ManagedCarrierClaim,
) assurancehostroute.ObservationFact

// CarrierRemovalBaselineObserver captures immutable pre-effect facts for one
// fresh invoking action. It must not execute the route or derive facts from a
// previous pending removal.
type CarrierRemovalBaselineObserver func(
	context.Context,
	carrierabsence.Action,
) (durablecarrier.EffectBaselineSet, error)

// GlobalClaimRemover retires one exact global claim only when its dedicated
// compare-and-swap registry still equals the supplied confirmed baseline.
type carrierRemovalGlobalClaimRemover func(
	context.Context,
	durablecarrier.GlobalCarrierClaims,
	durablecarrier.ManagedCarrierClaim,
) (durablecarrier.GlobalCarrierClaims, error)

// Input supplies already-authorized operation facts and effect capabilities.
// It does not perform planning or host-specific route selection.
type carrierRemovalInput struct {
	StatePath                 string
	SelectedRoot              string
	Current                   durable.Snapshot
	GlobalClaims              durablecarrier.GlobalCarrierClaims
	Actions                   []carrierabsence.Action
	RelationAuthorityPaths    []observerelation.AuthorityPath
	ProjectRoot               *rootedpath.CapturedRoot
	Filesystem                mutationfs.RootedStore
	Adapter                   executehostroute.RemovalAdapter
	Executor                  subprocess.CommandExecutor
	Observer                  CarrierRemovalObserver
	BaselineObserver          CarrierRemovalBaselineObserver
	RemoveGlobalClaim         carrierRemovalGlobalClaimRemover
	ValidateBeforeEffects     func(context.Context, mutation.PhysicalAuthoritySet) error
	ReserveStatefileAuthority reserveStatefileEffectAuthority
	StatefileAuthority        *statefileEffectAuthority
	CloseBoundRemoval         func(*configrelation.BoundRemoval) error
	CloseStatefileAuthority   func(*statefileEffectAuthority) error
	MarkExecutionAttempted    func()
	Clock                     func() time.Time
}

func (input carrierRemovalInput) markAttempted() {
	if input.MarkExecutionAttempted != nil {
		input.MarkExecutionAttempted()
	}
}

func (input carrierRemovalInput) closeBoundRemoval(
	removal *configrelation.BoundRemoval,
) error {
	if input.CloseBoundRemoval != nil {
		return input.CloseBoundRemoval(removal)
	}
	return removal.Close()
}

func (input carrierRemovalInput) closeStatefileAuthority(
	authority *statefileEffectAuthority,
) error {
	if input.CloseStatefileAuthority != nil {
		return input.CloseStatefileAuthority(authority)
	}
	return authority.Close()
}

// Result reports the last durably committed state and bounded attempt history.
type carrierRemovalResult struct {
	State        durable.Snapshot
	GlobalClaims durablecarrier.GlobalCarrierClaims
	Attempts     []durableattempt.HostRouteAttempt
	ActionCount  int
}

// Run executes admitted carrier removals in canonical action order and stops
// after the first non-converged or indeterminate action.
func runCarrierRemovals(
	ctx context.Context,
	input carrierRemovalInput,
) (carrierRemovalResult, error) {
	return runCarrierRemovalsWithPlans(
		ctx,
		input,
		applyContinuationPlan{},
		applyContinuationPlan{},
	)
}

func runScheduledCarrierRemovals(
	ctx context.Context,
	input carrierRemovalInput,
	prepared applyContinuationPlan,
	current applyContinuationPlan,
) (carrierRemovalResult, error) {
	if !prepared.valid() || !current.valid() {
		return carrierRemovalResult{
			State:        input.Current,
			GlobalClaims: input.GlobalClaims,
		}, fmt.Errorf("scheduled carrier removal plan is unavailable")
	}
	return runCarrierRemovalsWithPlans(ctx, input, prepared, current)
}

func runCarrierRemovalsWithPlans(
	ctx context.Context,
	input carrierRemovalInput,
	prepared applyContinuationPlan,
	current applyContinuationPlan,
) (result carrierRemovalResult, resultErr error) {
	result = carrierRemovalResult{
		State:        input.Current,
		GlobalClaims: input.GlobalClaims,
	}
	if ctx == nil {
		return result, fmt.Errorf("carrier removal context is required")
	}
	for index, action := range input.Actions {
		if err := action.Validate(); err != nil {
			return result, fmt.Errorf("carrier removal action[%d]: %w", index, err)
		}
	}
	preparedRemoval, currentRemoval, removalRefs, err := bindCarrierRemovalPlans(
		input.Actions,
		prepared,
		current,
	)
	if err != nil {
		return result, err
	}
	stateAuthority := input.StatefileAuthority
	ownedStateAuthority := false
	if stateAuthority == nil {
		plan, err := carrierRemovalStatefileEffectPlan(input.Actions)
		if err != nil {
			return result, err
		}
		stateAuthority, err = newStatefileEffectAuthority(
			input.StatePath,
			plan,
			input.ReserveStatefileAuthority,
		)
		if err != nil {
			return result, err
		}
		ownedStateAuthority = stateAuthority != nil
	}
	input.StatefileAuthority = stateAuthority
	var continuation *applyContinuationExecution
	if currentRemoval.valid() {
		continuation, err = newApplyContinuationExecution(preparedRemoval, currentRemoval)
		if err != nil {
			if ownedStateAuthority {
				return result, errors.Join(err, input.closeStatefileAuthority(stateAuthority))
			}
			return result, err
		}
	}
	var authorityCloseErr error
	if continuation != nil {
		defer func() {
			resultErr = errors.Join(continuation.finish(resultErr), authorityCloseErr)
		}()
	}
	if ownedStateAuthority {
		if continuation != nil {
			defer func() {
				authorityCloseErr = input.closeStatefileAuthority(stateAuthority)
			}()
		} else {
			defer func() {
				resultErr = errors.Join(resultErr, input.closeStatefileAuthority(stateAuthority))
			}()
		}
	}
	for index, action := range input.Actions {
		if !action.StateOnly() &&
			!action.VerifiesPendingRemoval() &&
			!action.InvokesHostRoute() &&
			!action.MutatesDirectProjection() {
			continue
		}
		ref := removalRefs[index]
		if action.StateOnly() {
			if continuation != nil {
				if err := continuation.consume(
					ref+"/no-op",
					operationplan.EffectStepNoOp,
				); err != nil {
					return result, fmt.Errorf(
						"carrier removal action[%d] %s: %w",
						index,
						action.Subject().String(),
						err,
					)
				}
			}
			continue
		}
		if action.VerifiesPendingRemoval() {
			if err := settlePendingRemoval(
				ctx,
				input,
				action,
				&result,
				continuation,
				ref,
			); err != nil {
				return result, fmt.Errorf(
					"carrier removal action[%d] %s: %w",
					index,
					action.Subject().String(),
					err,
				)
			}
			result.ActionCount++
			continue
		}
		var actionErr error
		switch {
		case action.InvokesHostRoute():
			actionErr = runOne(ctx, input, action, &result, continuation, ref)
		case action.MutatesDirectProjection():
			actionErr = runDirectProjectionRemoval(
				ctx,
				input,
				action,
				&result,
				continuation,
				ref,
			)
		default:
			continue
		}
		if actionErr != nil {
			return result, fmt.Errorf(
				"carrier removal action[%d] %s: %w",
				index,
				action.Subject().String(),
				actionErr,
			)
		}
		result.ActionCount++
	}
	return result, nil
}

func bindCarrierRemovalPlans(
	actions []carrierabsence.Action,
	prepared applyContinuationPlan,
	current applyContinuationPlan,
) (applyContinuationPlan, applyContinuationPlan, []string, error) {
	if !prepared.valid() && !current.valid() {
		return applyContinuationPlan{}, applyContinuationPlan{}, make([]string, len(actions)), nil
	}
	if !prepared.valid() || !current.valid() {
		return applyContinuationPlan{}, applyContinuationPlan{}, nil,
			fmt.Errorf("carrier removal continuation plan pair is incomplete")
	}
	preparedRemoval := prepared.carrierRemovalPlan()
	currentRemoval := current.carrierRemovalPlan()
	if !preparedRemoval.equal(currentRemoval) {
		return applyContinuationPlan{}, applyContinuationPlan{}, nil,
			fmt.Errorf("prepared and current apply continuation plans differ")
	}
	refs := make([]string, len(actions))
	scheduled := 0
	// Reconciliation and compiled carrier facts share canonical action order.
	// Bind them in lockstep so semantic verification remains input-linear.
	for index, action := range actions {
		if !action.StateOnly() &&
			!action.VerifiesPendingRemoval() &&
			!action.InvokesHostRoute() &&
			!action.MutatesDirectProjection() {
			continue
		}
		if scheduled >= len(currentRemoval.carrierRemovals) {
			return applyContinuationPlan{}, applyContinuationPlan{}, nil, fmt.Errorf(
				"carrier removal action[%d] %s: apply continuation carrier removal is not scheduled",
				index,
				action.Subject().String(),
			)
		}
		fact := currentRemoval.carrierRemovals[scheduled]
		fingerprint, err := carrierRemovalScheduleFingerprint(action)
		if err != nil {
			return applyContinuationPlan{}, applyContinuationPlan{}, nil, fmt.Errorf(
				"carrier removal action[%d] %s: %w",
				index,
				action.Subject().String(),
				err,
			)
		}
		if fact.action.Compare(action) != 0 || !fact.fingerprint.Equal(fingerprint) {
			return applyContinuationPlan{}, applyContinuationPlan{}, nil, fmt.Errorf(
				"carrier removal action[%d] %s: apply continuation carrier removal is not scheduled",
				index,
				action.Subject().String(),
			)
		}
		refs[index] = fact.ref
		scheduled++
	}
	if scheduled != len(currentRemoval.carrierRemovals) {
		return applyContinuationPlan{}, applyContinuationPlan{}, nil, fmt.Errorf(
			"carrier removal schedule contains %d actions for %d executable removals",
			len(currentRemoval.carrierRemovals),
			scheduled,
		)
	}
	return preparedRemoval, currentRemoval, refs, nil
}

func runOne(
	ctx context.Context,
	input carrierRemovalInput,
	action carrierabsence.Action,
	result *carrierRemovalResult,
	execution *applyContinuationExecution,
	ref string,
) (resultErr error) {
	stateAuthority := input.StatefileAuthority
	ensureStateAuthority := func(prefix string, failureCleanup func() error) error {
		if stateAuthority == nil {
			err := fmt.Errorf("carrier removal statefile authority is required")
			if failureCleanup != nil {
				return errors.Join(err, failureCleanup())
			}
			return err
		}
		return scheduledCarrierRemovalEnsure(
			ctx,
			execution,
			prefix,
			stateAuthority,
			failureCleanup,
		)
	}

	preflightRef := ref + "/preflight"
	if execution != nil {
		if err := execution.consume(preflightRef, operationplan.EffectStepObservation); err != nil {
			return err
		}
	}
	command, err := executehostroute.BuildRemovalCommand(executehostroute.RemovalBuildInput{
		Action:  action,
		WorkDir: input.SelectedRoot,
		Adapter: input.Adapter,
	})
	if err != nil {
		if execution != nil {
			if selectErr := execution.selectAlternative(ref+"/preflight-outcome", 0); selectErr != nil {
				return errors.Join(err, selectErr)
			}
		}
		rejectedRef := ref + "/preflight-rejected"
		var record durableattempt.HostRouteAttempt
		recordErr := scheduledContinuationCall(
			execution,
			rejectedRef+"/record",
			operationplan.EffectStepObservation,
			func() error {
				var recordErr error
				record, recordErr = preflightAttempt(action, err, now(input))
				return recordErr
			},
		)
		if recordErr != nil {
			return errors.Join(err, recordErr)
		}
		if validationErr := scheduledCarrierRemovalForward(
			execution,
			rejectedRef+"/forward",
			func() error {
				return validateBeforeRemovalEffects(
					ctx,
					input,
					mutation.PhysicalAuthoritySet{},
				)
			},
			nil,
		); validationErr != nil {
			return errors.Join(err, validationErr)
		}
		if authorityErr := ensureStateAuthority(
			rejectedRef+"/statefile",
			nil,
		); authorityErr != nil {
			return errors.Join(err, authorityErr)
		}
		if persistErr := persistAttempt(
			ctx,
			input,
			stateAuthority,
			result,
			record,
			execution,
			rejectedRef+"/attempt",
			nil,
		); persistErr != nil {
			return errors.Join(err, persistErr)
		}
		terminalErr := error(nil)
		if execution != nil {
			terminalErr = execution.consumeTerminal(rejectedRef + "/failure")
		}
		return errors.Join(
			hostRouteFailuresError([]durableattempt.HostRouteAttempt{record}),
			err,
			terminalErr,
		)
	}
	if execution != nil {
		if err := execution.selectAlternative(ref+"/preflight-outcome", 1); err != nil {
			return err
		}
		if err := execution.consume(
			ref+"/binding",
			operationplan.EffectStepObservation,
		); err != nil {
			return err
		}
	}
	var binding rootedpath.WorkingDirectoryCapability
	err = validateRetainedRemovalBoundary(ctx, input)
	if err == nil {
		binding, err = input.ProjectRoot.AcquireSelectedWorkingDirectory(input.SelectedRoot)
	}
	if err != nil {
		if execution != nil {
			if selectErr := execution.selectAlternative(ref+"/binding-outcome", 0); selectErr != nil {
				return errors.Join(err, selectErr)
			}
		}
		bindingRef := ref + "/binding-failed"
		if validationErr := scheduledCarrierRemovalForward(
			execution,
			bindingRef+"/forward",
			func() error {
				return validateBeforeRemovalEffects(
					ctx,
					input,
					mutation.PhysicalAuthoritySet{},
				)
			},
			nil,
		); validationErr != nil {
			return errors.Join(err, validationErr)
		}
		if authorityErr := ensureStateAuthority(bindingRef+"/statefile", nil); authorityErr != nil {
			return errors.Join(err, authorityErr)
		}
		if execution != nil {
			if consumeErr := execution.consume(
				bindingRef+"/host",
				operationplan.EffectStepExternal,
			); consumeErr != nil {
				return errors.Join(err, consumeErr)
			}
		}
		attempt := input.Executor.ExecuteInWorkingDirectory(
			ctx,
			command.AttemptRequest(),
			func() (subprocess.WorkingDirectoryBinding, error) { return nil, err },
		)
		var record durableattempt.HostRouteAttempt
		classifyErr := scheduledContinuationCall(
			execution,
			bindingRef+"/classify",
			operationplan.EffectStepObservation,
			func() error {
				classified, classifyErr := classify(
					command,
					attempt,
					assurancehostroute.ObservationUnavailable(
						assurancehostroute.ResultReasonObservationUnavailable,
					),
					action.RouteAdmission().Operation().EffectPostconditions(),
				)
				if classifyErr != nil {
					return classifyErr
				}
				var recordErr error
				record, recordErr = durableAttempt(action, classified, true)
				return recordErr
			},
		)
		if classifyErr != nil {
			return errors.Join(err, classifyErr)
		}
		if persistErr := persistAttempt(
			ctx,
			input,
			stateAuthority,
			result,
			record,
			execution,
			bindingRef+"/attempt",
			nil,
		); persistErr != nil {
			return errors.Join(err, persistErr)
		}
		terminalErr := error(nil)
		if execution != nil {
			terminalErr = execution.consumeTerminal(bindingRef + "/failure")
		}
		return errors.Join(
			hostRouteFailuresError([]durableattempt.HostRouteAttempt{record}),
			err,
			terminalErr,
		)
	}
	if execution != nil {
		if err := execution.selectAlternative(ref+"/binding-outcome", 1); err != nil {
			return errors.Join(err, binding.Close())
		}
	}

	bindingOwned := true
	closeBinding := func() error {
		if !bindingOwned {
			return nil
		}
		bindingOwned = false
		return binding.Close()
	}
	defer func() {
		resultErr = errors.Join(resultErr, closeBinding())
	}()
	preparedRef := ref + "/prepared"
	baselines := durablecarrier.EffectBaselineSet{}
	if err := scheduledCarrierRemovalCallWithFailureCleanup(
		execution,
		preparedRef+"/baselines",
		operationplan.EffectStepObservation,
		func() error {
			if input.BaselineObserver == nil {
				return nil
			}
			var err error
			baselines, err = input.BaselineObserver(ctx, action)
			if err != nil {
				return fmt.Errorf("capture carrier removal baselines: %w", err)
			}
			return nil
		},
		closeBinding,
	); err != nil {
		return err
	}
	if err := scheduledCarrierRemovalForward(
		execution,
		preparedRef+"/forward",
		func() error {
			return validateBeforeRemovalEffects(
				ctx,
				input,
				mutation.PhysicalAuthoritySet{},
			)
		},
		closeBinding,
	); err != nil {
		return err
	}
	if err := ensureStateAuthority(preparedRef+"/statefile", closeBinding); err != nil {
		return err
	}
	var pending durablecarrier.PendingCarrierRemoval
	if err := scheduledCarrierRemovalStatefilePublication(
		execution,
		preparedRef+"/statefile/pending",
		func() error {
			entry, err := stateAuthority.EntryForCommit()
			if err != nil {
				return err
			}
			input.markAttempted()
			next, nextPending, err := execute.CommitPendingCarrierRemoval(
				ctx,
				filesystem(input),
				entry,
				result.State,
				result.GlobalClaims,
				action,
				baselines,
				statefile.Codec{},
			)
			if err != nil {
				return err
			}
			result.State = next
			pending = nextPending
			return nil
		},
		closeBinding,
	); err != nil {
		return err
	}
	if err := scheduledCarrierRemovalCallWithFailureCleanup(
		execution,
		preparedRef+"/context-before-host",
		operationplan.EffectStepObservation,
		ctx.Err,
		closeBinding,
	); err != nil {
		return err
	}
	if err := scheduledCarrierRemovalStatefileValidation(
		ctx,
		execution,
		preparedRef+"/statefile/pre-host",
		stateAuthority,
		closeBinding,
	); err != nil {
		return fmt.Errorf("validate StateDir before carrier removal command: %w", err)
	}
	if execution != nil {
		if err := execution.consume(
			preparedRef+"/host",
			operationplan.EffectStepExternal,
		); err != nil {
			return errors.Join(err, closeBinding())
		}
	}
	transferred := false
	attempt := input.Executor.ExecuteInWorkingDirectory(
		ctx,
		command.AttemptRequest(),
		func() (subprocess.WorkingDirectoryBinding, error) {
			transferred = true
			bindingOwned = false
			return binding, nil
		},
	)
	var releaseErr error
	if !transferred {
		releaseErr = closeBinding()
	}
	if execution != nil {
		if err := execution.consume(
			preparedRef+"/statefile/post-host/validate",
			operationplan.EffectStepValidateDescendant,
		); err != nil {
			return errors.Join(err, releaseErr)
		}
	}
	if authorityErr := stateAuthority.Validate(ctx); authorityErr != nil {
		if errors.Is(authorityErr, context.Canceled) || errors.Is(authorityErr, context.DeadlineExceeded) {
			terminalErr := error(nil)
			if execution != nil {
				if selectErr := execution.selectAlternative(
					preparedRef+"/post-host-outcome",
					2,
				); selectErr != nil {
					return errors.Join(authorityErr, releaseErr, selectErr)
				}
				terminalErr = execution.consumeTerminal(preparedRef + "/post-host-canceled")
			}
			return errors.Join(authorityErr, releaseErr, terminalErr)
		}
		if execution != nil {
			if selectErr := execution.selectAlternative(
				preparedRef+"/post-host-outcome",
				1,
			); selectErr != nil {
				return errors.Join(authorityErr, releaseErr, selectErr)
			}
			if consumeErr := execution.consume(
				preparedRef+"/post-host-failure/classify",
				operationplan.EffectStepObservation,
			); consumeErr != nil {
				return errors.Join(authorityErr, releaseErr, consumeErr)
			}
		}
		classified, classifyErr := classify(
			command,
			attempt,
			assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationUnavailable,
			),
			pending.EffectPostconditions(),
		)
		var record durableattempt.HostRouteAttempt
		var recordErr error
		if classifyErr == nil {
			record, recordErr = durableAttempt(action, classified, true)
			if recordErr == nil {
				result.Attempts = append(result.Attempts, record)
			}
		}
		terminalErr := error(nil)
		if execution != nil {
			terminalErr = execution.consumeTerminal(preparedRef + "/post-host-failure")
		}
		failureErr := error(nil)
		if recordErr == nil && classifyErr == nil {
			failureErr = hostRouteFailuresError([]durableattempt.HostRouteAttempt{record})
		}
		return errors.Join(
			failureErr,
			authorityErr,
			classifyErr,
			recordErr,
			releaseErr,
			terminalErr,
		)
	}
	if execution != nil {
		if err := execution.selectAlternative(preparedRef+"/post-host-outcome", 0); err != nil {
			return errors.Join(err, releaseErr)
		}
		if err := execution.consume(
			preparedRef+"/post-host-success",
			operationplan.EffectStepNoOp,
		); err != nil {
			return errors.Join(err, releaseErr)
		}
		if err := execution.consume(
			preparedRef+"/post-host-observation",
			operationplan.EffectStepObservation,
		); err != nil {
			return errors.Join(err, releaseErr)
		}
	}
	observation := assurancehostroute.ObservationUnavailable(
		assurancehostroute.ResultReasonObservationUnavailable,
	)
	if input.Observer != nil {
		observation = input.Observer(
			ctx,
			pending,
			append(
				result.State.ManagedCarrierClaims(),
				result.GlobalClaims.Claims()...,
			),
		)
	}
	if execution != nil {
		if err := execution.consume(
			preparedRef+"/classify",
			operationplan.EffectStepObservation,
		); err != nil {
			return errors.Join(err, releaseErr)
		}
	}
	classified, classifyErr := classify(
		command,
		attempt,
		observation,
		pending.EffectPostconditions(),
	)
	if classifyErr != nil {
		classified, err = classify(
			command,
			attempt,
			assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationParseFailed,
			),
			pending.EffectPostconditions(),
		)
		if err != nil {
			terminalErr := error(nil)
			if execution != nil {
				if selectErr := execution.selectAlternative(
					preparedRef+"/classify-outcome",
					1,
				); selectErr != nil {
					return errors.Join(classifyErr, err, releaseErr, selectErr)
				}
				terminalErr = execution.consumeTerminal(preparedRef + "/classify-failure")
			}
			return errors.Join(classifyErr, err, releaseErr, terminalErr)
		}
	}
	if execution != nil {
		if err := execution.selectAlternative(preparedRef+"/classify-outcome", 0); err != nil {
			return errors.Join(classifyErr, releaseErr, err)
		}
		if err := execution.consume(
			preparedRef+"/classify-usable",
			operationplan.EffectStepNoOp,
		); err != nil {
			return errors.Join(classifyErr, releaseErr, err)
		}
		if err := execution.consume(
			preparedRef+"/retained-boundary",
			operationplan.EffectStepObservation,
		); err != nil {
			return errors.Join(classifyErr, releaseErr, err)
		}
	}
	boundaryErr := validateRetainedRemovalBoundary(ctx, input)
	var record durableattempt.HostRouteAttempt
	recordErr := scheduledContinuationCall(
		execution,
		preparedRef+"/attempt-record",
		operationplan.EffectStepObservation,
		func() error {
			var recordErr error
			record, recordErr = durableAttempt(action, classified, boundaryErr != nil)
			return recordErr
		},
	)
	if recordErr != nil {
		return errors.Join(classifyErr, recordErr, boundaryErr, releaseErr)
	}
	var attemptFailure error
	if removalAttemptFailed(classified, record) {
		attemptFailure = hostRouteFailuresError([]durableattempt.HostRouteAttempt{record})
	}
	if err := persistAttempt(
		ctx,
		input,
		stateAuthority,
		result,
		record,
		execution,
		preparedRef+"/attempt",
		nil,
	); err != nil {
		return errors.Join(attemptFailure, classifyErr, err, boundaryErr, releaseErr)
	}
	finalErr := errors.Join(attemptFailure, classifyErr, boundaryErr, releaseErr)
	if finalErr != nil {
		terminalErr := error(nil)
		if execution != nil {
			if selectErr := execution.selectAlternative(
				preparedRef+"/attempt-outcome",
				1,
			); selectErr != nil {
				return errors.Join(finalErr, selectErr)
			}
			terminalErr = execution.consumeTerminal(preparedRef + "/attempt-failure")
		}
		return errors.Join(finalErr, terminalErr)
	}
	if execution != nil {
		if err := execution.selectAlternative(preparedRef+"/attempt-outcome", 0); err != nil {
			return err
		}
	}
	return retireClaim(
		ctx,
		input,
		action,
		pending,
		stateAuthority,
		result,
		execution,
		ref,
		nil,
	)
}

// settlePendingRemoval verifies and retires one previously attempted removal
// from fresh current evidence without manufacturing or repeating an attempt.
func settlePendingRemoval(
	ctx context.Context,
	input carrierRemovalInput,
	action carrierabsence.Action,
	result *carrierRemovalResult,
	execution *applyContinuationExecution,
	ref string,
) error {
	pending, present := action.PendingRemoval()
	if !present {
		return fmt.Errorf("pending-removal verification has no exact durable fact")
	}
	if err := scheduledContinuationCall(
		execution,
		ref+"/verify-current",
		operationplan.EffectStepObservation,
		func() error {
			return verifyCurrentRemovalPostconditions(
				ctx,
				input,
				action,
				pending,
				result,
			)
		},
	); err != nil {
		return err
	}
	if err := scheduledCarrierRemovalForward(
		execution,
		ref+"/forward",
		func() error {
			return validateBeforeRemovalEffects(
				ctx,
				input,
				mutation.PhysicalAuthoritySet{},
			)
		},
		nil,
	); err != nil {
		return err
	}
	stateAuthority := input.StatefileAuthority
	if stateAuthority == nil {
		return fmt.Errorf("carrier removal statefile authority is required")
	}
	if err := scheduledCarrierRemovalEnsure(
		ctx,
		execution,
		ref+"/statefile",
		stateAuthority,
		nil,
	); err != nil {
		return err
	}
	return retireClaim(
		ctx,
		input,
		action,
		pending,
		stateAuthority,
		result,
		execution,
		ref,
		nil,
	)
}

func validateBeforeRemovalEffects(
	ctx context.Context,
	input carrierRemovalInput,
	authority mutation.PhysicalAuthoritySet,
) error {
	if err := validateRetainedRemovalBoundary(ctx, input); err != nil {
		return err
	}
	if input.ValidateBeforeEffects != nil {
		return input.ValidateBeforeEffects(ctx, authority)
	}
	return nil
}

func validateRetainedRemovalBoundary(ctx context.Context, input carrierRemovalInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if input.ProjectRoot == nil {
		return rootedpath.NewBoundaryFailure(
			rootedpath.FailureRootUnavailable,
			input.SelectedRoot,
			"carrier removal requires retained project-root authority",
			nil,
		)
	}
	if err := input.ProjectRoot.ValidateSelection(input.SelectedRoot); err != nil {
		return err
	}
	return nil
}

func classify(
	command executehostroute.Command,
	attempt subprocess.CommandAttemptResult,
	observation assurancehostroute.ObservationFact,
	effectPostconditions effectpostcondition.Set,
) (assurancehostroute.Result, error) {
	return assurancehostroute.ClassifyResult(assurancehostroute.ResultInput{
		Subject:      command.Subject(),
		RouteRequest: command.RouteRequest(),
		Attempt:      assurancehostroute.ObservedAttempt(attempt, assurancehostroute.AttemptReason(attempt.Reason())),
		Observation:  observation,
		RequiredPostcondition: assurancehostroute.RequirePostconditions(
			assurancehostroute.RelationPostconditionAbsent,
			effectPostconditions,
		),
	})
}

func durableAttempt(
	action carrierabsence.Action,
	result assurancehostroute.Result,
	workDirAuthorityLost bool,
) (durableattempt.HostRouteAttempt, error) {
	return assurancehostroute.NewDurableAttempt(assurancehostroute.DurableAttemptInput{
		Result:               result,
		Target:               action.Target(),
		Scope:                action.Scope(),
		Operation:            lock.OperationRemove,
		WorkDirAuthorityLost: workDirAuthorityLost,
	})
}

func preflightAttempt(
	action carrierabsence.Action,
	err error,
	observedAt time.Time,
) (durableattempt.HostRouteAttempt, error) {
	class := durableattempt.HostRouteResultBlockedPreflight
	reason := durableattempt.HostRouteReasonPreflightFailed
	var validation *executehostroute.ValidationError
	if errors.As(err, &validation) {
		reason = durableattempt.HostRouteResultReason(validation.Code())
		switch validation.Code() {
		case executehostroute.ReasonUnsupportedSource:
			class = durableattempt.HostRouteResultUnsupportedSource
		case executehostroute.ReasonUnsupportedScope:
			class = durableattempt.HostRouteResultUnsupportedScope
		}
	}
	request := action.RouteAdmission().Request()
	return durableattempt.NewHostRouteAttempt(durableattempt.HostRouteAttemptInput{
		Subject:          action.Subject(),
		Target:           action.Target(),
		Scope:            action.Scope(),
		Operation:        lock.OperationRemove,
		RouteID:          request.RouteID(),
		RouteRequestHash: request.CanonicalRequestHash(),
		ObservedAt:       observedAt,
		ResultClass:      class,
		Reason:           reason,
		Observation:      observerelation.ObservationNotObserved,
		Postcondition:    observerelation.PostconditionNotObserved,
	})
}

func persistAttempt(
	ctx context.Context,
	input carrierRemovalInput,
	authority *statefileEffectAuthority,
	result *carrierRemovalResult,
	record durableattempt.HostRouteAttempt,
	execution *applyContinuationExecution,
	prefix string,
	failureCleanup func() error,
) error {
	if err := scheduledCarrierRemovalStatefileValidation(
		ctx,
		execution,
		prefix+"/pre-persistence",
		authority,
		failureCleanup,
	); err != nil {
		return err
	}
	if err := scheduledCarrierRemovalStatefilePublication(
		execution,
		prefix+"/persistence",
		func() error {
			entry, err := authority.EntryForCommit()
			if err != nil {
				return err
			}
			input.markAttempted()
			next, err := execute.CommitHostRouteAttempts(
				ctx,
				filesystem(input),
				entry,
				result.State,
				[]durableattempt.HostRouteAttempt{record},
				statefile.Codec{},
			)
			if err != nil {
				return fmt.Errorf("persist carrier removal attempt: %w", err)
			}
			result.State = next
			result.Attempts = append(result.Attempts, record)
			return nil
		},
		failureCleanup,
	); err != nil {
		return err
	}
	return scheduledCarrierRemovalStatefileValidation(
		ctx,
		execution,
		prefix+"/post-persistence",
		authority,
		failureCleanup,
	)
}

func removalVerified(result assurancehostroute.Result) bool {
	return result.PostconditionsSatisfied()
}

func removalAttemptFailed(
	result assurancehostroute.Result,
	record durableattempt.HostRouteAttempt,
) bool {
	return !removalVerified(result) ||
		record.Reason() == durableattempt.HostRouteReasonWorkDirAuthority
}

func now(input carrierRemovalInput) time.Time {
	if input.Clock != nil {
		return input.Clock().UTC()
	}
	return time.Now().UTC()
}

func filesystem(input carrierRemovalInput) mutationfs.RootedStore {
	if input.Filesystem != nil {
		return input.Filesystem
	}
	return storagecommit.Adapter{}
}
