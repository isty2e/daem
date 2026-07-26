package delegate

import (
	"context"
	"errors"
	"os"

	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
)

// NewExecutor constructs an executor with safe defaults for omitted dependencies.
func NewExecutor(options Options) Executor {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	outputLimit := options.OutputLimit
	if outputLimit <= 0 {
		outputLimit = DefaultOutputLimit
	}
	lookupEnv := options.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	runner := options.Runner
	return Executor{
		timeout:     timeout,
		outputLimit: outputLimit,
		lookupEnv:   lookupEnv,
		runner:      runner,
	}
}

// Execute executes one delegate action or records why it was not executed.
func (executor Executor) Execute(
	ctx context.Context,
	action reconcile.DelegateAction,
	bind subprocess.WorkingDirectoryBinder,
) AttemptRecord {
	executor = executor.withDefaults()
	if !action.SchedulesAttempt() {
		if action.Disposition() == reconcile.DelegateBlocked {
			return newAttemptRecord(action, AttemptBlocked, ReasonPolicyBlocked, subprocess.CommandAttemptResult{})
		}
		return newAttemptRecord(action, AttemptSkipped, ReasonNotScheduled, subprocess.CommandAttemptResult{})
	}

	disclosure := action.Disclosure()
	if bind == nil {
		bind = func() (subprocess.WorkingDirectoryBinding, error) {
			return nil, errors.New("working-directory binding is required")
		}
	}
	result := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Timeout:     executor.timeout,
		OutputLimit: executor.outputLimit,
		LookupEnv:   executor.lookupEnv,
		Runner:      executor.runner,
	}).ExecuteInWorkingDirectory(ctx, subprocess.CommandAttemptRequest{
		Command:     disclosure.Command,
		Args:        append([]string(nil), disclosure.Args...),
		EnvRefs:     commandEnvRefs(disclosure.Env),
		OutputLimit: executor.outputLimit,
	}, bind)
	status, reason := classifyResult(result)
	return newAttemptRecord(action, status, reason, result)
}

// ExecuteAll executes actions in order and returns sanitized attempt records for every action.
func (executor Executor) ExecuteAll(
	ctx context.Context,
	actions []reconcile.DelegateAction,
	bindForAction BinderForAction,
) ([]AttemptRecord, error) {
	records := make([]AttemptRecord, 0, len(actions))
	failures := make([]AttemptRecord, 0)
	for _, action := range actions {
		var bind subprocess.WorkingDirectoryBinder
		if bindForAction != nil {
			bind = bindForAction(action)
		}
		result := executor.Execute(ctx, action, bind)
		records = append(records, result)
		if result.Failed() {
			failures = append(failures, result)
		}
	}
	if len(failures) != 0 {
		return records, ExecutionError{records: failures}
	}
	return records, nil
}

func (executor Executor) withDefaults() Executor {
	return NewExecutor(Options{
		Timeout:     executor.timeout,
		OutputLimit: executor.outputLimit,
		LookupEnv:   executor.lookupEnv,
		Runner:      executor.runner,
	})
}

func classifyResult(result subprocess.CommandAttemptResult) (AttemptStatus, Reason) {
	switch result.Reason() {
	case subprocess.CommandReasonTimeout:
		return AttemptFailed, ReasonTimeout
	case subprocess.CommandReasonMissingRunner:
		return AttemptFailed, ReasonMissingRunner
	case subprocess.CommandReasonMissingEnvRef:
		return AttemptFailed, ReasonMissingEnvRef
	case subprocess.CommandReasonNonZeroExit:
		return AttemptFailed, ReasonNonZeroExit
	case subprocess.CommandReasonCanceled,
		subprocess.CommandReasonSignaled,
		subprocess.CommandReasonRunnerError:
		return AttemptFailed, ReasonRunnerError
	case subprocess.CommandReasonWorkDirAuthority:
		return AttemptFailed, ReasonWorkDirAuthority
	default:
		return AttemptSucceeded, ReasonNone
	}
}

func commandEnvRefs(bindings []lock.DelegateEnvBinding) []subprocess.CommandEnvRef {
	refs := make([]subprocess.CommandEnvRef, 0, len(bindings))
	for _, binding := range bindings {
		refs = append(refs, subprocess.CommandEnvRef{
			Name:       binding.Name,
			SourceName: binding.SourceName,
		})
	}
	return refs
}
