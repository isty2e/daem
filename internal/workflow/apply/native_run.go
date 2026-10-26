package apply

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/effect/execute"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func runNativeWithOptions(ctx context.Context, paths daempaths.Paths, assessment readiness.Assessment, options runOptions) (runResult, error) {
	if options.nativeInput == nil || options.applyEffectPlan == nil || !options.nativeInput.Preparation.BeforeState().Equal(assessment.CurrentState) {
		return runResult{}, fmt.Errorf("native command execution requires its exact reserved plan and live observation")
	}
	input := *options.nativeInput
	input.Paths = executePaths(paths, assessment.StatePath)
	input.ProjectRoot = options.projectRoot
	validate := options.validateBeforeEffects
	if options.validatePhysicalAuthorityRequests != nil {
		validate = nil
	}
	result, err := execute.ApplyNativeWithEffectPlan(ctx, input, *options.applyEffectPlan, execute.ApplyOptions{
		Events: options.ExecuteEvents, ValidateBeforeEffects: validate, ValidatePhysicalAuthorityRequests: options.validatePhysicalAuthorityRequests,
		AcceptVisibilityChanges: options.acceptVisibilityChanges, ValidateCompensationAuthority: options.validateCompensationAuthority,
		AcceptCompensationVisibilityChanges: options.acceptCompensationChanges,
	})
	if result.ExecutionAttempted {
		options.markAttempted()
	}
	return runResult{ActionCount: result.ActionCount, State: result.State, StatePath: result.StatePath, GlobalCarrierClaims: assessment.GlobalCarrierClaims}, err
}
