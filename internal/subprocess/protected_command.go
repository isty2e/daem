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
	attempt    CommandAttemptResult
	completion ProtectedCommandCompletion
}

// ProtectedCommandCompletion carries runner evidence, never captured output or
// a certificate that the caller's protection record has been settled.
type ProtectedCommandCompletion struct {
	binding          executionidentity.Binding
	quiescence       executioncontext.ExecutionQuiescence
	quiescenceErr    error
	releaseAttempted bool
}

func (result ProtectedCommandAttemptResult) Attempt() CommandAttemptResult { return result.attempt }
func (result ProtectedCommandAttemptResult) ReleaseAttempted() bool {
	return result.completion.ReleaseAttempted()
}

func (result ProtectedCommandAttemptResult) Binding() (executionidentity.Binding, bool) {
	return result.completion.Binding()
}

func (result ProtectedCommandAttemptResult) Quiescence() (executioncontext.ExecutionQuiescence, error) {
	return result.completion.Quiescence()
}

func (completion ProtectedCommandCompletion) ReleaseAttempted() bool {
	return completion.releaseAttempted
}

// Binding is the observed helper binding, not proof of its durable publication.
func (completion ProtectedCommandCompletion) Binding() (executionidentity.Binding, bool) {
	return completion.binding, completion.binding.Validate() == nil
}

func (completion ProtectedCommandCompletion) Quiescence() (executioncontext.ExecutionQuiescence, error) {
	if completion.quiescenceErr == nil {
		if err := completion.quiescence.Require(completion.binding); err != nil {
			return executioncontext.ExecutionQuiescence{}, err
		}
	}
	return completion.quiescence, completion.quiescenceErr
}

// ExecuteProtectedInWorkingDirectory requires the caller's already-Armed
// invocation. confirm must return nil only after exact durable binding publication.
// An ordinary injected runner cannot substitute for this native launch protocol.
// finalize runs once before capture processing for every admitted call, including
// pre-run failure. Its context ignores normal cancellation; the caller must supply
// pre-admitted safety work and fresh settlement proofs. The returned error is
// finalization/admission failure, independent of the mechanical attempt outcome.
func (executor CommandExecutor) ExecuteProtectedInWorkingDirectory(
	ctx context.Context,
	request CommandAttemptRequest,
	bind WorkingDirectoryBinder,
	invocation executionidentity.Invocation,
	confirm func(context.Context, executionidentity.Binding) error,
	finalize func(context.Context, ProtectedCommandCompletion) error,
) (ProtectedCommandAttemptResult, error) {
	executor = executor.withDefaults()
	if ctx == nil || !executor.nativeRunner || invocation.Validate() != nil || confirm == nil || finalize == nil {
		cause := errors.New("protected execution requires a context, native runner, invocation, durable binding confirmation and safety finalizer")
		raw := CommandResult{RunnerSetupFailed: true, Err: cause}
		capture := sanitizeCapture(raw, nil, executor.outputLimit)
		return ProtectedCommandAttemptResult{
			attempt:    newCommandAttemptResult(executor.clock(), false, CommandReasonRunnerError, raw, capture),
			completion: ProtectedCommandCompletion{quiescenceErr: errors.New("protected helper was not started")},
		}, cause
	}
	state := &protectedInvocationRun{
		invocation: invocation, confirm: confirm,
		quiescenceErr: errors.New("no qualified execution binding was observed"),
	}
	executor.runner = func(runCtx context.Context, prepared CommandRequest) CommandResult {
		return protectedCommandRunner(runCtx, prepared, state)
	}
	if bind == nil {
		bind = func() (WorkingDirectoryBinding, error) {
			return nil, errors.New("working-directory binder is required")
		}
	}
	var completion ProtectedCommandCompletion
	var finalizationErr error
	attempt := executor.execute(ctx, request, bind, func() {
		completion = ProtectedCommandCompletion{binding: state.binding, quiescence: state.quiescence, quiescenceErr: state.quiescenceErr, releaseAttempted: state.releaseAttempted}
		finalizationErr = finalize(context.WithoutCancel(ctx), completion)
	})
	return ProtectedCommandAttemptResult{
		attempt: attempt, completion: completion,
	}, finalizationErr
}
