package apply

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/configrelation"
	payloadbuild "github.com/isty2e/daem/internal/effect/payload/build"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lock"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func runNativeWithOptions(ctx context.Context, paths daempaths.Paths, environment desired.Environment, locked lock.File, selection targetselection.Selection, assessment readiness.Assessment, options runOptions) (runResult, error) {
	if options.nativeInput == nil || options.applyEffectPlan == nil || !options.nativeInput.Preparation.BeforeState().Equal(assessment.CurrentState) {
		return runResult{}, fmt.Errorf("native command execution requires its exact reserved plan and live observation")
	}
	if options.requireContinuation {
		if err := bindApplyContinuationPlans(assessment.Reconciliation, options.preparedContinuation, options.currentContinuation); err != nil {
			return runResult{}, err
		}
	}
	_, globalRetirements, err := stateOnlyCarrierClaimRetirements(assessment.Reconciliation.CarrierAbsences())
	if err != nil {
		return runResult{}, err
	}
	_, globalAdoptions, err := stateOnlyCarrierClaimAdoptions(assessment.CurrentState, assessment.Reconciliation.CarrierAdoptions())
	if err != nil {
		return runResult{}, err
	}
	orders := options.nativeInput.Preparation.PiOrders()
	var orderResult relationOrderRunResult
	if len(orders) != 0 {
		orderResult, err = prepareNativeOrderConvergence(ctx, paths, locked, assessment.Reconciliation, orders, options)
		if err != nil {
			return runResult{RelationOrderResults: orderResult.results}, err
		}
		recordNativeOrderEvents(&orderResult, configrelation.OrderSequenceStarted, false, nil, options)
	}
	applied, err := executeNativeSegment(ctx, paths, environment, locked, selection, *options.nativeInput, *options.applyEffectPlan, options)
	if err != nil {
		if len(orders) != 0 {
			changed := applied.State.Equal(options.nativeInput.Preparation.AfterState())
			recordNativeOrderEvents(&orderResult, configrelation.OrderSequenceFailed, changed, err, options)
		}
		return runResult{ActionCount: applied.ActionCount, State: applied.State, StatePath: applied.StatePath, GlobalCarrierClaims: assessment.GlobalCarrierClaims, RelationOrderResults: orderResult.results}, err
	}
	if len(orders) != 0 {
		recordNativeOrderEvents(&orderResult, configrelation.OrderSequenceDone, true, nil, options)
		options.nativeOrderResult = &orderResult
	}
	if options.afterNativeCore != nil {
		refreshed, continuation, err := options.afterNativeCore(ctx, applied)
		if err != nil {
			return runResult{ActionCount: applied.ActionCount, State: applied.State, StatePath: applied.StatePath, GlobalCarrierClaims: assessment.GlobalCarrierClaims}, err
		}
		assessment = refreshed
		applied.State = refreshed.CurrentState
		options.currentContinuation = continuation
		if err := bindApplyContinuationPlans(assessment.Reconciliation, options.preparedContinuation, continuation); err != nil {
			return runResult{ActionCount: applied.ActionCount, State: applied.State, StatePath: applied.StatePath, GlobalCarrierClaims: assessment.GlobalCarrierClaims}, err
		}
	}
	return runAfterCarrierClaimRetirements(
		ctx, paths, locked, selection, applied, assessment.Owner, assessment.GlobalCarrierClaims,
		globalRetirements, globalAdoptions, assessment.Reconciliation, assessment.RelationObservations, options,
	)
}

func executeNativeSegment(ctx context.Context, paths daempaths.Paths, environment desired.Environment, locked lock.File, selection targetselection.Selection, input execute.NativeApplyInput, plan execute.ApplyEffectPlan, options runOptions) (result execute.ApplyResult, resultErr error) {
	input.Paths = executePaths(paths, input.Paths.StatefilePath)
	input.ProjectRoot = options.projectRoot
	subjects := managedPathPayloadSubjects(input.Preparation.ManagedPathEffects())
	if len(subjects) != 0 {
		payloads, err := payloadbuild.PayloadSet(ctx, payloadbuild.Input{
			Paths: paths, Environment: environment, Lockfile: locked, Selection: selection, ManagedPathPayloadSubjects: subjects,
		})
		if err != nil {
			return execute.ApplyResult{StatePath: input.Paths.StatefilePath, State: input.CurrentState}, err
		}
		input.Payloads = payloads
		defer func() { resultErr = errors.Join(resultErr, payloads.Cleanup()) }()
	}
	validate := options.validateBeforeEffects
	if options.validatePhysicalAuthorityRequests != nil {
		validate = nil
	}
	result, resultErr = execute.ApplyNativeWithEffectPlan(ctx, input, plan, execute.ApplyOptions{
		Events: options.ExecuteEvents, ValidateBeforeEffects: validate, ValidatePhysicalAuthorityRequests: options.validatePhysicalAuthorityRequests,
		AcceptVisibilityChanges: options.acceptVisibilityChanges, ValidateCompensationAuthority: options.validateCompensationAuthority,
		AcceptCompensationVisibilityChanges: options.acceptCompensationChanges,
	})
	if result.ExecutionAttempted {
		options.markAttempted()
	}
	if result.StatePath == "" {
		result.StatePath, result.State = input.Paths.StatefilePath, input.CurrentState
	}
	return result, resultErr
}
