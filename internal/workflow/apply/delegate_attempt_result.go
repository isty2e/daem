package apply

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

// DelegateAttemptResult composes one mechanical effect result with bounded
// post-attempt assurance summaries for persistence and presentation.
type DelegateAttemptResult struct {
	attempt       delegate.AttemptRecord
	observation   observerelation.ObservationSummary
	postcondition observerelation.PostconditionSummary
}

func newDelegateAttemptResult(
	attempt delegate.AttemptRecord,
	observation observerelation.ObservationSummary,
	postcondition observerelation.PostconditionSummary,
) (DelegateAttemptResult, error) {
	if attempt.Subject().IsZero() {
		return DelegateAttemptResult{}, fmt.Errorf("delegate attempt result requires subject identity")
	}
	canonicalObservation, err := observerelation.ParseObservationSummary(string(observation))
	if err != nil {
		return DelegateAttemptResult{}, err
	}
	canonicalPostcondition, err := observerelation.ParsePostconditionSummary(string(postcondition))
	if err != nil {
		return DelegateAttemptResult{}, err
	}
	return DelegateAttemptResult{
		attempt:       attempt,
		observation:   canonicalObservation,
		postcondition: canonicalPostcondition,
	}, nil
}

// Attempt returns the sanitized mechanical effect result.
func (result DelegateAttemptResult) Attempt() delegate.AttemptRecord {
	return result.attempt
}

// ObservationSummary returns the bounded post-attempt relation observation.
func (result DelegateAttemptResult) ObservationSummary() observerelation.ObservationSummary {
	return result.observation
}

// PostconditionSummary returns the bounded operation postcondition observation.
func (result DelegateAttemptResult) PostconditionSummary() observerelation.PostconditionSummary {
	return result.postcondition
}

func optionalExitCode(exitCode int, present bool) *int {
	if !present {
		return nil
	}
	return &exitCode
}

func runDelegatesAndPersistAttemptRecords(
	ctx context.Context,
	paths daempaths.Paths,
	locked lock.File,
	selection targetselection.Selection,
	statePath string,
	current durable.Snapshot,
	actionCount int,
	reconciliation reconcile.Result,
	expectedPlanFingerprint mutation.OperationFingerprint,
	options runOptions,
) (runResult, error) {
	delegateActions := reconciliation.Delegates()
	var stateAuthority *rootedpath.EntryAuthority
	if delegateActionsRequireAttemptPersistence(delegateActions) {
		if options.projectRoot == nil {
			return runResult{
					ActionCount: actionCount,
					StatePath:   statePath,
					State:       current,
				}, rootedpath.NewBoundaryFailure(
					rootedpath.FailureRootUnavailable,
					paths.ManifestRoot,
					"delegate attempt persistence requires retained project-root authority",
					nil,
				)
		}
		var bindErr error
		stateAuthority, bindErr = rootedpath.BindSelectedEntryAuthority(
			options.projectRoot,
			paths.ManifestRoot,
			statePath,
		)
		if bindErr != nil {
			return runResult{
				ActionCount: actionCount,
				StatePath:   statePath,
				State:       current,
			}, bindErr
		}
		defer stateAuthority.Close()
	}
	var delegateAttempts []delegate.AttemptRecord
	var declarationErr error
	bindForAction := delegateWorkingDirectoryBinderForAction(
		options,
		paths.ManifestRoot,
	)
	for index, action := range delegateActions {
		actualPlanFingerprint, err := remainingExecutionFingerprint(reconciliation)
		if err != nil {
			declarationErr = err
			break
		}
		phase := fmt.Sprintf("delegate route[%d]", index)
		if err := options.executionGuard.requirePlanCurrent(
			ctx,
			expectedPlanFingerprint,
			actualPlanFingerprint,
			"before "+phase,
		); err != nil {
			declarationErr = err
			break
		}
		var bind subprocess.WorkingDirectoryBinder
		if bindForAction != nil {
			bind = bindForAction(action)
		}
		delegateAttempts = append(
			delegateAttempts,
			options.DelegateExecutor.Execute(ctx, action, bind),
		)
		declarationErr = options.executionGuard.requireDeclarationsCurrent(
			ctx,
			"after "+phase,
		)
		if declarationErr != nil {
			break
		}
	}
	delegateErr := delegate.FailedAttemptError(delegateAttempts)
	rootErr := validateDelegateProjectRoot(options, paths.ManifestRoot, delegateActions)
	summaries := defaultPostAttemptSummaries(locked, delegateAttempts)
	if rootErr == nil {
		summaries = postAttemptSummaries(paths, locked, selection, current, delegateAttempts)
	}
	delegateResults, resultErr := delegateAttemptResults(delegateAttempts, summaries)
	if resultErr != nil {
		return runResult{
				ActionCount: actionCount,
				StatePath:   statePath,
				State:       current,
			}, errors.Join(
				delegateErr,
				declarationErr,
				fmt.Errorf("compose delegate attempt result: %w", resultErr),
			)
	}
	if rootErr != nil {
		return runResult{
			ActionCount:      actionCount,
			StatePath:        statePath,
			State:            current,
			DelegateAttempts: delegateResults,
		}, errors.Join(delegateErr, declarationErr, rootErr)
	}
	persistedAttempts, attemptErr := durableAttemptsFromDelegateResults(delegateResults, time.Now().UTC())
	if attemptErr != nil {
		return runResult{
				ActionCount:      actionCount,
				StatePath:        statePath,
				State:            current,
				DelegateAttempts: delegateResults,
			}, errors.Join(
				delegateErr,
				declarationErr,
				fmt.Errorf("compose durable delegate attempt: %w", attemptErr),
			)
	}
	if len(persistedAttempts) == 0 {
		return runResult{
			ActionCount:      actionCount,
			StatePath:        statePath,
			State:            current,
			DelegateAttempts: delegateResults,
		}, errors.Join(delegateErr, declarationErr)
	}
	nextState, persistErr := execute.CommitDelegateAttempts(
		ctx,
		storagecommit.Adapter{},
		stateAuthority,
		current,
		persistedAttempts,
		statefile.Codec{},
	)
	rootErr = validateDelegateProjectRoot(options, paths.ManifestRoot, delegateActions)
	result := runResult{
		ActionCount:      actionCount,
		StatePath:        statePath,
		State:            nextState,
		DelegateAttempts: delegateResults,
	}
	if persistErr != nil {
		persistErr = fmt.Errorf("persist delegate attempt record: %w", persistErr)
	}
	return result, errors.Join(delegateErr, declarationErr, persistErr, rootErr)
}

func delegateActionsRequireAttemptPersistence(actions []reconcile.DelegateAction) bool {
	for _, action := range actions {
		if action.SchedulesAttempt() || action.Disposition() == reconcile.DelegateBlocked {
			return true
		}
	}
	return false
}

func durableAttemptsFromDelegateResults(
	results []DelegateAttemptResult,
	observedAt time.Time,
) ([]durableattempt.DelegateAttempt, error) {
	result := make([]durableattempt.DelegateAttempt, 0, len(results))
	for _, delegateResult := range results {
		item := delegateResult.Attempt()
		status, ok := durableDelegateStatus(item.Status())
		if !ok {
			continue
		}
		exitCode, hasExitCode := item.ExitCode()
		attempt, err := durableattempt.NewDelegateAttempt(durableattempt.DelegateAttemptInput{
			Subject:         item.Subject(),
			Target:          target.Target(item.Target()),
			Scope:           target.Scope(item.Scope()),
			PlanIdentityKey: item.IdentityKey(),
			ObservedAt:      observedAt,
			Status:          status,
			Reason:          durableDelegateReason(item.Reason()),
			Observation:     delegateResult.ObservationSummary(),
			Postcondition:   delegateResult.PostconditionSummary(),
			ExitCode:        optionalExitCode(exitCode, hasExitCode),
			TimedOut:        item.TimedOut(),
			StdoutTruncated: item.StdoutTruncated(),
			StderrTruncated: item.StderrTruncated(),
			Redacted:        item.Redacted(),
		})
		if err != nil {
			return nil, err
		}
		result = append(result, attempt)
	}
	return result, nil
}

func durableDelegateStatus(status delegate.AttemptStatus) (durableattempt.DelegateAttemptStatus, bool) {
	switch status {
	case delegate.AttemptSucceeded:
		return durableattempt.DelegateStatusSucceeded, true
	case delegate.AttemptFailed:
		return durableattempt.DelegateStatusFailed, true
	case delegate.AttemptBlocked:
		return durableattempt.DelegateStatusBlocked, true
	default:
		return "", false
	}
}

func durableDelegateReason(reason delegate.Reason) durableattempt.DelegateAttemptReason {
	switch reason {
	case delegate.ReasonPolicyBlocked:
		return durableattempt.DelegateReasonPolicyBlocked
	case delegate.ReasonMissingEnvRef:
		return durableattempt.DelegateReasonMissingEnvRef
	case delegate.ReasonMissingRunner:
		return durableattempt.DelegateReasonMissingRunner
	case delegate.ReasonNonZeroExit:
		return durableattempt.DelegateReasonNonZeroExit
	case delegate.ReasonTimeout:
		return durableattempt.DelegateReasonTimeout
	case delegate.ReasonRunnerError:
		return durableattempt.DelegateReasonRunnerError
	case delegate.ReasonWorkDirAuthority:
		return durableattempt.DelegateReasonWorkDirAuthority
	default:
		return durableattempt.DelegateReasonNone
	}
}

func delegateWorkingDirectoryBinderForAction(
	options runOptions,
	selectedRoot string,
) delegate.BinderForAction {
	return func(action reconcile.DelegateAction) subprocess.WorkingDirectoryBinder {
		if action.Scope() != target.ScopeProject {
			return func() (subprocess.WorkingDirectoryBinding, error) {
				return nil, rootedpath.NewBoundaryFailure(
					rootedpath.FailureRootUnavailable,
					selectedRoot,
					fmt.Sprintf("delegate attempt scope %q has no working-directory authority", action.Scope()),
					nil,
				)
			}
		}
		return func() (subprocess.WorkingDirectoryBinding, error) {
			if options.projectRoot == nil {
				return nil, rootedpath.NewBoundaryFailure(
					rootedpath.FailureRootUnavailable,
					selectedRoot,
					"delegate attempt requires retained project-root authority",
					nil,
				)
			}
			return options.projectRoot.AcquireSelectedWorkingDirectory(selectedRoot)
		}
	}
}

func validateDelegateProjectRoot(
	options runOptions,
	selectedRoot string,
	actions []reconcile.DelegateAction,
) error {
	for _, action := range actions {
		if action.SchedulesAttempt() && action.Scope() == target.ScopeProject {
			if options.projectRoot == nil {
				return rootedpath.NewBoundaryFailure(
					rootedpath.FailureRootUnavailable,
					selectedRoot,
					"delegate attempt requires retained project-root authority",
					nil,
				)
			}
			return options.projectRoot.ValidateSelection(selectedRoot)
		}
	}
	return nil
}
