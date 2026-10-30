package subprocess

import (
	"context"
	"errors"

	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/subprocess/executioncontext"
)

type protectedInvocationRun struct {
	invocation       executionidentity.Invocation
	confirm          func(context.Context, executionidentity.Binding) error
	binding          executionidentity.Binding
	quiescence       executioncontext.ExecutionQuiescence
	quiescenceErr    error
	releaseAttempted bool
}

// ProtectedCommandAttemptResult keeps mechanical outcome separate from a release
// attempt and fresh writer-group evidence. None of them settles a safety record.
type ProtectedCommandAttemptResult struct {
	attempt          CommandAttemptResult
	binding          executionidentity.Binding
	quiescence       executioncontext.ExecutionQuiescence
	quiescenceErr    error
	releaseAttempted bool
}

func (result ProtectedCommandAttemptResult) Attempt() CommandAttemptResult { return result.attempt }
func (result ProtectedCommandAttemptResult) ReleaseAttempted() bool        { return result.releaseAttempted }

// Binding is the observed helper binding, not proof of its durable publication.
func (result ProtectedCommandAttemptResult) Binding() (executionidentity.Binding, bool) {
	return result.binding, result.binding.Validate() == nil
}

func (result ProtectedCommandAttemptResult) Quiescence() (executioncontext.ExecutionQuiescence, error) {
	if result.quiescenceErr == nil {
		if err := result.quiescence.Require(result.binding); err != nil {
			return executioncontext.ExecutionQuiescence{}, err
		}
	}
	return result.quiescence, result.quiescenceErr
}

// ExecuteProtectedInWorkingDirectory requires the caller's already-Armed
// invocation. confirm must return nil only after exact durable binding publication.
// An ordinary injected runner cannot substitute for this native launch protocol.
func (executor CommandExecutor) ExecuteProtectedInWorkingDirectory(
	ctx context.Context,
	request CommandAttemptRequest,
	bind WorkingDirectoryBinder,
	invocation executionidentity.Invocation,
	confirm func(context.Context, executionidentity.Binding) error,
) ProtectedCommandAttemptResult {
	executor = executor.withDefaults()
	if ctx == nil || !executor.nativeRunner || invocation.Validate() != nil || confirm == nil {
		raw := CommandResult{RunnerSetupFailed: true, Err: errors.New("protected execution requires a native runner, invocation and durable binding confirmation")}
		capture := sanitizeCapture(raw, nil, executor.outputLimit)
		return ProtectedCommandAttemptResult{
			attempt:       newCommandAttemptResult(executor.clock(), false, CommandReasonRunnerError, raw, capture),
			quiescenceErr: errors.New("protected helper was not started"),
		}
	}
	state := &protectedInvocationRun{
		invocation: invocation, confirm: confirm,
		quiescenceErr: errors.New("no qualified execution binding was observed"),
	}
	executor.runner = func(runCtx context.Context, prepared CommandRequest) CommandResult {
		return protectedCommandRunner(runCtx, prepared, state)
	}
	attempt := executor.ExecuteInWorkingDirectory(ctx, request, bind)
	return ProtectedCommandAttemptResult{
		attempt: attempt, binding: state.binding, quiescence: state.quiescence,
		quiescenceErr: state.quiescenceErr, releaseAttempted: state.releaseAttempted,
	}
}
