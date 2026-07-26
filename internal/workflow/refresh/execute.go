package refresh

import (
	"context"
	"errors"
	"fmt"
	"time"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	"github.com/isty2e/daem/internal/assurance/statefile"
	executeeffect "github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
)

const attemptPersistenceTimeout = 5 * time.Second

// ExecuteOptions supplies subprocess boundary dependencies while the workflow
// retains timeout, output-bound, route, and working-directory policy.
type ExecuteOptions struct {
	CommandOptions subprocess.CommandOptions
}

// Execute consumes one disclosed plan exactly once, revalidates its complete
// authority, invokes one exact host request, and records bounded operation
// history only when a process started.
func Execute(
	ctx context.Context,
	prepared *PreparedCommand,
	options ExecuteOptions,
) (result CommandResult, returnErr error) {
	execution, err := prepared.beginExecution()
	if err != nil {
		if prepared == nil {
			return CommandResult{}, err
		}
		return prepared.Disclosure(), err
	}
	result = cloneCommandResult(execution.planned.result)
	result.Mode = ModeExecute
	attemptStarted := false
	defer func() {
		if execution.root == nil {
			return
		}
		if closeErr := execution.root.Close(); closeErr != nil {
			result = resultWithCleanupFailure(result, attemptStarted)
			returnErr = errors.Join(returnErr, fmt.Errorf(
				"close refresh project-root witness: %w",
				closeErr,
			))
		}
	}()

	if ctx == nil {
		return cancelledBeforeAttempt(result, fmt.Errorf("refresh context is required"))
	}
	if err := ctx.Err(); err != nil {
		return cancelledBeforeAttempt(result, err)
	}
	visibleFingerprint, err := refreshFingerprint(execution.planned)
	if err != nil || !execution.planned.fingerprint.Equal(visibleFingerprint) {
		return staleBeforeAttempt(result, errors.Join(mutation.StalePlanError{}, err))
	}
	visibleAuthority, err := buildAuthorityEvidence(execution.planned, execution.root)
	if err != nil ||
		!execution.planned.authority.authorityFingerprint.Equal(
			visibleAuthority.authorityFingerprint,
		) {
		return staleBeforeAttempt(result, errors.Join(mutation.StalePlanError{}, err))
	}
	if matches, revisionErr := execution.planned.revisions.MatchesCurrent(ctx); revisionErr != nil {
		return refusedBeforeAttempt(result, ReasonMutationAuthority, revisionErr)
	} else if !matches {
		return staleBeforeAttempt(result, mutation.StalePlanError{})
	}

	store, err := mutation.NewStore(execution.planned.paths.DataDir)
	if err != nil {
		return refusedBeforeAttempt(result, ReasonMutationAuthority, err)
	}
	effectPaths, err := execution.planned.paths.WithDataDir(store.DataDir())
	if err != nil {
		return refusedBeforeAttempt(result, ReasonMutationAuthority, err)
	}
	leases, err := store.Acquire(ctx, execution.planned.authority.domains...)
	if err != nil {
		return refusedBeforeAttempt(result, ReasonMutationAuthority, err)
	}
	defer func() {
		if releaseErr := leases.Release(); releaseErr != nil {
			result = resultWithCleanupFailure(result, attemptStarted)
			returnErr = errors.Join(returnErr, fmt.Errorf(
				"release refresh mutation authority: %w",
				releaseErr,
			))
		}
	}()
	if matches, matchErr := leases.DomainsMatchCurrent(ctx); matchErr != nil {
		return refusedBeforeAttempt(result, ReasonMutationAuthority, matchErr)
	} else if !matches {
		return staleBeforeAttempt(result, mutation.StalePlanError{})
	}
	if matches, matchErr := execution.planned.revisions.MatchesCurrent(ctx); matchErr != nil {
		return refusedBeforeAttempt(result, ReasonMutationAuthority, matchErr)
	} else if !matches {
		return staleBeforeAttempt(result, mutation.StalePlanError{})
	}
	revisions, err := mutation.CaptureRevisionSet(
		ctx,
		execution.planned.authority.revisions...,
	)
	if err != nil {
		return refusedBeforeAttempt(result, ReasonMutationAuthority, err)
	}

	current, err := planAtPaths(
		ctx,
		execution.input,
		execution.planned.timeout,
		execution.options,
		effectPaths,
		ModeExecute,
	)
	if err != nil {
		return staleBeforeAttempt(result, errors.Join(mutation.StalePlanError{}, err))
	}
	current.authority, err = buildAuthorityEvidence(current, execution.root)
	if err != nil {
		return staleBeforeAttempt(result, errors.Join(mutation.StalePlanError{}, err))
	}
	if !execution.planned.fingerprint.Equal(current.fingerprint) ||
		!execution.planned.authority.authorityFingerprint.Equal(
			current.authority.authorityFingerprint,
		) {
		return staleBeforeAttempt(result, mutation.StalePlanError{})
	}
	if err := execution.root.ValidateSelection(current.paths.ManifestRoot); err != nil {
		return staleBeforeAttempt(result, errors.Join(mutation.StalePlanError{}, err))
	}
	if matches, matchErr := revisions.MatchesCurrent(ctx); matchErr != nil {
		return refusedBeforeAttempt(result, ReasonMutationAuthority, matchErr)
	} else if !matches {
		return staleBeforeAttempt(result, mutation.StalePlanError{})
	}
	if matches, matchErr := leases.DomainsMatchCurrent(ctx); matchErr != nil {
		return refusedBeforeAttempt(result, ReasonMutationAuthority, matchErr)
	} else if !matches {
		return staleBeforeAttempt(result, mutation.StalePlanError{})
	}

	stateAuthority, err := rootedpath.BindSelectedEntryAuthority(
		execution.root,
		current.paths.ManifestRoot,
		current.paths.StatefilePath,
	)
	if err != nil {
		return refusedBeforeAttempt(result, ReasonMutationAuthority, err)
	}
	defer func() {
		if closeErr := stateAuthority.Close(); closeErr != nil {
			result = resultWithCleanupFailure(result, attemptStarted)
			returnErr = errors.Join(returnErr, fmt.Errorf(
				"close refresh statefile authority: %w",
				closeErr,
			))
		}
	}()
	if err := validateBeforeHostAttempt(ctx, execution.root, current, revisions, leases); err != nil {
		return staleBeforeAttempt(result, err)
	}

	commandOptions := options.CommandOptions
	commandOptions.Timeout = time.Duration(
		current.result.Disclosure.Invocation.TimeoutSeconds,
	) * time.Second
	if current.command.attempt.OutputLimit > 0 {
		commandOptions.OutputLimit = current.command.attempt.OutputLimit
	}
	executor := subprocess.NewCommandExecutor(commandOptions)
	attempt := executor.ExecuteInWorkingDirectory(
		ctx,
		current.command.attemptRequest(),
		func() (subprocess.WorkingDirectoryBinding, error) {
			return execution.root.AcquireSelectedWorkingDirectory(
				current.paths.ManifestRoot,
			)
		},
	)
	attemptStarted = attempt.Started()
	result.Attempted = attemptStarted
	result.ProcessOutcome = processOutcome(attempt)

	observationFact := assurancehostroute.ObservationUnavailable(
		assurancehostroute.ResultReasonObservationUnsupported,
	)
	var postObservationErr error
	if current.result.Route.ObservationPosture == PostureRequireCurrent {
		postObservation, observeErr := execution.options.Observer(ctx, ObservationRequest{
			Paths:        current.paths,
			Lockfile:     current.lockfile,
			CurrentState: current.currentState,
			Subject:      current.subject,
			Target:       current.result.Selection.Target,
			Scope:        current.result.Selection.Scope,
		})
		switch {
		case observeErr != nil:
			postObservationErr = observeErr
			observationFact = assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationUnavailable,
			)
			result.Observation = nil
		case !postObservation.Present:
			postObservationErr = fmt.Errorf("required post-attempt relation evidence is unavailable")
			observationFact = assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationUnavailable,
			)
			result.Observation = nil
		default:
			postPaths, authorityErr := canonicalObservationAuthorityPaths(
				postObservation.AuthorityPaths,
			)
			if authorityErr != nil ||
				!sameObservationAuthorityPaths(current.authorityPaths, postPaths) {
				postObservationErr = errors.Join(
					fmt.Errorf("post-attempt observer authority changed"),
					authorityErr,
				)
				observationFact = assurancehostroute.ObservationUnavailable(
					assurancehostroute.ResultReasonObservationUnavailable,
				)
				result.Observation = nil
			} else {
				observationFact = assurancehostroute.CurrentObservation(
					postObservation.Result,
				)
				result.Observation = observationSummary(postObservation.Result)
			}
		}
	}
	classified, classifyErr := assurancehostroute.ClassifyResult(
		assurancehostroute.ResultInput{
			Subject:      current.subject,
			RouteRequest: current.routeRequest,
			Attempt: assurancehostroute.ObservedAttempt(
				attempt,
				assurancehostroute.AttemptReason(attempt.Reason()),
			),
			Observation: observationFact,
			RequiredPostcondition: assurancehostroute.RequireRelationPostcondition(
				assurancehostroute.RelationPostconditionPresent,
			),
		},
	)
	if classifyErr != nil {
		postObservationErr = errors.Join(
			postObservationErr,
			fmt.Errorf("classify refresh post-attempt observation: %w", classifyErr),
		)
		classified, classifyErr = assurancehostroute.ClassifyResult(
			assurancehostroute.ResultInput{
				Subject:      current.subject,
				RouteRequest: current.routeRequest,
				Attempt: assurancehostroute.ObservedAttempt(
					attempt,
					assurancehostroute.AttemptReason(attempt.Reason()),
				),
				Observation: assurancehostroute.ObservationUnavailable(
					assurancehostroute.ResultReasonObservationParseFailed,
				),
				RequiredPostcondition: assurancehostroute.RequireRelationPostcondition(
					assurancehostroute.RelationPostconditionPresent,
				),
			},
		)
		if classifyErr != nil {
			result.ResultClass = resultClassAfterClassificationFailure(attempt)
			result.ReasonCode = ReasonCommandFailed
			return result, errors.Join(
				postObservationErr,
				fmt.Errorf("classify refresh fallback result: %w", classifyErr),
			)
		}
	}
	result = applyClassification(result, classified, attempt)

	var persistenceErr error
	if attemptStarted {
		record, recordErr := assurancehostroute.NewDurableAttempt(
			assurancehostroute.DurableAttemptInput{
				Result:    classified,
				Target:    current.result.Selection.Target,
				Scope:     current.result.Selection.Scope,
				Operation: lock.OperationRefresh,
			},
		)
		if recordErr != nil {
			persistenceErr = recordErr
		} else {
			persistCtx, cancel := context.WithTimeout(
				context.WithoutCancel(ctx),
				attemptPersistenceTimeout,
			)
			_, persistenceErr = executeeffect.CommitHostRouteAttempts(
				persistCtx,
				storagecommit.Adapter{},
				stateAuthority,
				current.currentState,
				[]durableattempt.HostRouteAttempt{record},
				statefile.Codec{},
			)
			cancel()
			if persistenceErr == nil {
				result.AttemptHistory.Persisted = true
			}
		}
	}
	if persistenceErr != nil {
		result.ResultClass = ResultPartial
		result.ReasonCode = ReasonAttemptPersistence
		result.Remediation = []string{
			"inspect daem status and the statefile before retrying refresh",
		}
		return result, errors.Join(
			fmt.Errorf("persist refresh attempt history: %w", persistenceErr),
			postObservationErr,
		)
	}
	if postObservationErr != nil && attempt.Succeeded() {
		result.ResultClass = ResultPartial
		result.ReasonCode = ReasonPostObservationFailed
		result.Remediation = []string{
			"run daem status and inspect the exact extension relation before retrying",
		}
		return result, fmt.Errorf(
			"refresh post-attempt observation: %w",
			postObservationErr,
		)
	}
	if result.HasErrors() {
		return result, errors.New(string(result.ReasonCode))
	}
	return result, nil
}
