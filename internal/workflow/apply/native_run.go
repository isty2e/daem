package apply

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/effect/execute"
	payloadbuild "github.com/isty2e/daem/internal/effect/payload/build"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lock"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func runNativeWithOptions(ctx context.Context, paths daempaths.Paths, environment desired.Environment, locked lock.File, selection targetselection.Selection, assessment readiness.Assessment, options runOptions) (result runResult, resultErr error) {
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
	input := *options.nativeInput
	input.Paths = executePaths(paths, assessment.StatePath)
	input.ProjectRoot = options.projectRoot
	subjects := managedPathPayloadSubjects(input.Preparation.ManagedPathEffects())
	if len(subjects) != 0 {
		payloads, err := payloadbuild.PayloadSet(ctx, payloadbuild.Input{
			Paths: paths, Environment: environment, Lockfile: locked, Selection: selection, ManagedPathPayloadSubjects: subjects,
		})
		if err != nil {
			return runResult{}, err
		}
		input.Payloads = payloads
		defer func() { resultErr = errors.Join(resultErr, payloads.Cleanup()) }()
	}
	validate := options.validateBeforeEffects
	if options.validatePhysicalAuthorityRequests != nil {
		validate = nil
	}
	applied, err := execute.ApplyNativeWithEffectPlan(ctx, input, *options.applyEffectPlan, execute.ApplyOptions{
		Events: options.ExecuteEvents, ValidateBeforeEffects: validate, ValidatePhysicalAuthorityRequests: options.validatePhysicalAuthorityRequests,
		AcceptVisibilityChanges: options.acceptVisibilityChanges, ValidateCompensationAuthority: options.validateCompensationAuthority,
		AcceptCompensationVisibilityChanges: options.acceptCompensationChanges,
	})
	if applied.ExecutionAttempted {
		options.markAttempted()
	}
	if err != nil {
		return runResult{ActionCount: applied.ActionCount, State: applied.State, StatePath: applied.StatePath, GlobalCarrierClaims: assessment.GlobalCarrierClaims}, err
	}
	return runAfterCarrierClaimRetirements(
		ctx, paths, locked, selection, applied, assessment.Owner, assessment.GlobalCarrierClaims,
		globalRetirements, globalAdoptions, assessment.Reconciliation, assessment.RelationObservations, options,
	)
}
