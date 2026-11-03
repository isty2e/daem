package delegate

import (
	"context"
	"errors"
	"os"

	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
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
	record, _, _ := executor.ExecuteWithInvocation(ctx, action, bind, nil)
	return record
}

// ScheduledRequest projects the exact request used by Execute; state-only
// actions have no subprocess request.
func (executor Executor) ScheduledRequest(action reconcile.DelegateAction) (subprocess.CommandAttemptRequest, bool) {
	if !action.SchedulesAttempt() {
		return subprocess.CommandAttemptRequest{}, false
	}
	executor = executor.withDefaults()
	return subprocess.CommandAttemptRequest{
		Command: action.Plan().Command().Executable(), Args: action.Plan().Command().Args(),
		EnvRefs: commandEnvRefs(action.Plan().Env().Bindings()), OutputLimit: executor.outputLimit,
	}, true
}

// ExecuteWithInvocation retains delegate result classification while allowing
// the containing workflow to qualify each actual command boundary.
func (executor Executor) ExecuteWithInvocation(ctx context.Context, action reconcile.DelegateAction, bind subprocess.WorkingDirectoryBinder, invoke Invocation) (AttemptRecord, bool, error) {
	executor = executor.withDefaults()
	if !action.SchedulesAttempt() {
		if action.Disposition() == reconcile.DelegateBlocked {
			return newAttemptRecord(action, AttemptBlocked, ReasonPolicyBlocked, subprocess.CommandAttemptResult{}), true, nil
		}
		return newAttemptRecord(action, AttemptSkipped, ReasonNotScheduled, subprocess.CommandAttemptResult{}), true, nil
	}

	if bind == nil {
		bind = func() (subprocess.WorkingDirectoryBinding, error) {
			return nil, errors.New("working-directory binding is required")
		}
	}
	command := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Timeout:     executor.timeout,
		OutputLimit: executor.outputLimit,
		LookupEnv:   executor.lookupEnv,
		Runner:      executor.runner,
	})
	request, _ := executor.ScheduledRequest(action)
	var result subprocess.CommandAttemptResult
	var err error
	if invoke == nil {
		result = command.ExecuteInWorkingDirectory(ctx, request, bind)
	} else {
		var observed bool
		result, observed, err = invoke(ctx, command, request, bind)
		if !observed {
			if err == nil {
				err = errors.New("delegate invocation returned no observation")
			}
			return AttemptRecord{}, false, err
		}
	}
	status, reason := classifyResult(result)
	return newAttemptRecord(action, status, reason, result), true, err
}

// FailedAttemptError returns one combined execution error for failed records,
// or nil when every attempt succeeded or was skipped.
func FailedAttemptError(records []AttemptRecord) error {
	failures := make([]AttemptRecord, 0)
	for _, record := range records {
		if record.Failed() {
			failures = append(failures, record)
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return ExecutionError{records: failures}
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
	if result.WorkDirAuthorityFailed() {
		return AttemptFailed, ReasonWorkDirAuthority
	}
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
	default:
		return AttemptSucceeded, ReasonNone
	}
}

func commandEnvRefs(bindings []realizationdelegate.EnvBinding) []subprocess.CommandEnvRef {
	refs := make([]subprocess.CommandEnvRef, 0, len(bindings))
	for _, binding := range bindings {
		refs = append(refs, subprocess.CommandEnvRef{
			Name:       binding.Name(),
			SourceName: binding.SourceName(),
		})
	}
	return refs
}
