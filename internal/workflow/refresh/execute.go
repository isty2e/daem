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
	"github.com/isty2e/daem/internal/operationplan"
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

	current, err := planAtPathsWithBarrier(
		ctx,
		execution.input,
		execution.planned.timeout,
		execution.options,
		effectPaths,
		ModeExecute,
		&execution.planned.barrier,
	)
	if err != nil {
		if isPreservedReplanCause(err) {
			return staleBeforeAttempt(result, err)
		}
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
	persistenceRevisions, err := mutation.CaptureRevisionSet(
		ctx,
		refreshAttemptPersistenceRevisionRequests(current)...,
	)
	if err != nil {
		return refusedBeforeAttempt(result, ReasonMutationAuthority, err)
	}

	forwardStructure, err := compileRefreshEffectStructure()
	if err != nil {
		return refusedBeforeAttempt(result, ReasonMutationAuthority, err)
	}
	forwardExecution, err := current.barrier.ReserveForwardEffectExecution(
		forwardStructure,
		current.paths.StatefilePath,
	)
	if err != nil {
		return refusedBeforeAttempt(result, ReasonMutationAuthority, err)
	}
	defer func() {
		if closeErr := forwardExecution.Close(); closeErr != nil {
			result = resultWithCleanupFailure(result, attemptStarted)
			returnErr = errors.Join(
				returnErr,
				fmt.Errorf("close refresh effect execution: %w", closeErr),
			)
		}
	}()
	if err := validateRefreshPeerAuthority(
		ctx,
		execution.root,
		current,
		revisions,
		leases,
	); err != nil {
		return staleBeforeAttempt(result, err)
	}
	if err := forwardExecution.ValidateBarrier(ctx, refreshStepPreAttemptBarrier); err != nil {
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
	if err := forwardExecution.ConsumeLifecycle(
		refreshStepInvokeExternal,
		operationplan.EffectStepExternal,
	); err != nil {
		return refusedBeforeAttempt(result, ReasonMutationAuthority, err)
	}
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
	result.AuthorityOutcome = authorityOutcome(attempt)
	attemptAlternative := 0
	observationStep := refreshStepNotStartedObservation
	if attemptStarted {
		attemptAlternative = 1
		observationStep = refreshStepStartedObservation
	}
	if err := forwardExecution.SelectAlternative(
		refreshAttemptOutcomeChoice,
		attemptAlternative,
	); err != nil {
		return refreshScheduleFailure(result, attemptStarted, err)
	}

	var persistenceErr error
	if attemptStarted {
		postAttemptCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			attemptPersistenceTimeout,
		)
		persistenceErr = forwardExecution.ValidateBarrier(
			postAttemptCtx,
			refreshStepPostAttemptBarrier,
		)
		cancel()
	}
	if err := forwardExecution.ConsumeLifecycle(
		observationStep,
		operationplan.EffectStepObservation,
	); err != nil {
		return refreshScheduleFailure(result, attemptStarted, err)
	}

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
			result = resultAfterClassificationFailure(result, attempt)
			failure := errors.Join(
				postObservationErr,
				fmt.Errorf("classify refresh fallback result: %w", classifyErr),
			)
			classificationChoice := refreshNotStartedClassificationChoice
			classificationTerminal := refreshStepNotStartedClassificationFailed
			if attemptStarted {
				classificationChoice = refreshStartedClassificationChoice
				classificationTerminal = refreshStepStartedClassificationFailed
			}
			if _, scheduleErr := continueRefreshEffect(
				forwardExecution,
				classificationChoice,
				false,
				classificationTerminal,
			); scheduleErr != nil {
				return refreshScheduleFailure(
					result,
					attemptStarted,
					errors.Join(failure, scheduleErr),
				)
			}
			return result, failure
		}
	}
	result = applyClassification(result, classified, attempt)
	if attemptStarted {
		if _, scheduleErr := continueRefreshEffect(
			forwardExecution,
			refreshStartedClassificationChoice,
			true,
			refreshStepStartedClassificationFailed,
		); scheduleErr != nil {
			return refreshScheduleFailure(result, true, scheduleErr)
		}
	} else {
		if err := forwardExecution.SelectAlternative(
			refreshNotStartedClassificationChoice,
			1,
		); err != nil {
			return refreshScheduleFailure(result, false, err)
		}
		if err := finishRefreshEffect(
			forwardExecution,
			refreshStepNotStartedTerminal,
		); err != nil {
			return refreshScheduleFailure(result, false, err)
		}
	}

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
			persistenceErr = errors.Join(persistenceErr, recordErr)
		}
		persistAttempt, scheduleErr := continueRefreshEffect(
			forwardExecution,
			refreshPersistenceChoice,
			persistenceErr == nil,
			refreshStepUnpersistedTerminal,
		)
		if scheduleErr != nil {
			return refreshScheduleFailure(
				result,
				true,
				errors.Join(persistenceErr, scheduleErr),
			)
		}
		if persistAttempt {
			persistCtx, cancel := context.WithTimeout(
				context.WithoutCancel(ctx),
				attemptPersistenceTimeout,
			)
			persistenceErr = func() error {
				_, err := forwardExecution.EstablishStateDir(
					persistCtx,
					refreshStepPersistenceEstablishStateDir,
					func(ctx context.Context) error {
						return validateRefreshPeerAuthority(
							ctx,
							execution.root,
							current,
							persistenceRevisions,
							leases,
						)
					},
				)
				if err != nil {
					_, scheduleErr := continueRefreshEffect(
						forwardExecution,
						refreshPersistenceStateDirChoice,
						false,
						refreshStepPersistenceStateDirFailed,
					)
					return joinRefreshPersistenceFailure(err, scheduleErr)
				}
				if _, scheduleErr := continueRefreshEffect(
					forwardExecution,
					refreshPersistenceStateDirChoice,
					true,
					refreshStepPersistenceStateDirFailed,
				); scheduleErr != nil {
					return scheduleErr
				}
				if err := forwardExecution.BindDescendant(
					persistCtx,
					refreshStepPersistenceBindDescendant,
				); err != nil {
					_, scheduleErr := continueRefreshEffect(
						forwardExecution,
						refreshPersistenceAuthorityChoice,
						false,
						refreshStepPersistenceAuthorityFailed,
					)
					return joinRefreshPersistenceFailure(err, scheduleErr)
				}
				if _, scheduleErr := continueRefreshEffect(
					forwardExecution,
					refreshPersistenceAuthorityChoice,
					true,
					refreshStepPersistenceAuthorityFailed,
				); scheduleErr != nil {
					return errors.Join(scheduleErr, forwardExecution.CloseDescendant())
				}
				finishFailure := func(
					choiceID string,
					terminalID string,
					cause error,
				) error {
					closeErr := forwardExecution.CloseDescendant()
					_, scheduleErr := continueRefreshEffect(
						forwardExecution,
						choiceID,
						false,
						terminalID,
					)
					return errors.Join(cause, closeErr, scheduleErr)
				}
				continueAfter := func(choiceID string, terminalID string) error {
					_, scheduleErr := continueRefreshEffect(
						forwardExecution,
						choiceID,
						true,
						terminalID,
					)
					if scheduleErr != nil {
						return errors.Join(
							scheduleErr,
							forwardExecution.CloseDescendant(),
						)
					}
					return nil
				}

				if err := forwardExecution.ValidateBarrier(
					persistCtx,
					refreshStepPrePersistenceBarrier,
				); err != nil {
					return finishFailure(
						refreshPrePersistenceBarrierChoice,
						refreshStepPrePersistenceBarrierFailed,
						err,
					)
				}
				if err := continueAfter(
					refreshPrePersistenceBarrierChoice,
					refreshStepPrePersistenceBarrierFailed,
				); err != nil {
					return err
				}
				if err := forwardExecution.ValidateDescendant(
					persistCtx,
					refreshStepPrePersistenceDescendant,
				); err != nil {
					return finishFailure(
						refreshPrePersistenceDescendantChoice,
						refreshStepPrePersistenceDescendantFailed,
						err,
					)
				}
				if err := continueAfter(
					refreshPrePersistenceDescendantChoice,
					refreshStepPrePersistenceDescendantFailed,
				); err != nil {
					return err
				}
				err = forwardExecution.PublishDescendant(
					refreshStepPublishAttempt,
					func(entry *rootedpath.EntryAuthority) error {
						_, commitErr := executeeffect.CommitHostRouteAttempts(
							persistCtx,
							storagecommit.Adapter{},
							entry,
							current.currentState,
							[]durableattempt.HostRouteAttempt{record},
							statefile.Codec{},
						)
						return commitErr
					},
				)
				if err != nil {
					return finishFailure(
						refreshPublicationChoice,
						refreshStepPublicationFailed,
						err,
					)
				}
				if err := continueAfter(
					refreshPublicationChoice,
					refreshStepPublicationFailed,
				); err != nil {
					return err
				}
				if err := forwardExecution.ValidateDescendant(
					persistCtx,
					refreshStepPostPersistenceDescendant,
				); err != nil {
					return finishFailure(
						refreshPostPersistenceDescendantChoice,
						refreshStepPostPersistenceDescendantFailed,
						err,
					)
				}
				if err := continueAfter(
					refreshPostPersistenceDescendantChoice,
					refreshStepPostPersistenceDescendantFailed,
				); err != nil {
					return err
				}
				postBarrierErr := forwardExecution.ValidateBarrier(
					persistCtx,
					refreshStepPostPersistenceBarrier,
				)
				postBarrierErr = errors.Join(
					postBarrierErr,
					forwardExecution.CloseDescendant(),
				)
				if postBarrierErr != nil {
					_, scheduleErr := continueRefreshEffect(
						forwardExecution,
						refreshPostPersistenceBarrierChoice,
						false,
						refreshStepPostPersistenceBarrierFailed,
					)
					return joinRefreshPersistenceFailure(postBarrierErr, scheduleErr)
				}
				if _, scheduleErr := continueRefreshEffect(
					forwardExecution,
					refreshPostPersistenceBarrierChoice,
					true,
					refreshStepPostPersistenceBarrierFailed,
				); scheduleErr != nil {
					return scheduleErr
				}
				if err := forwardExecution.ConsumeLifecycle(
					refreshStepPersistenceSettlement,
					operationplan.EffectStepPersistence,
				); err != nil {
					return err
				}
				return finishRefreshEffect(
					forwardExecution,
					refreshStepPersistedTerminal,
				)
			}()
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
		failure := errors.Join(
			fmt.Errorf("persist refresh attempt history: %w", persistenceErr),
			postObservationErr,
		)
		return result, failure
	}
	if postObservationErr != nil && attempt.Succeeded() && !attempt.WorkDirAuthorityFailed() {
		result.ResultClass = ResultPartial
		result.ReasonCode = ReasonPostObservationFailed
		result.Remediation = []string{
			"run daem status and inspect the exact extension relation before retrying",
		}
		failure := fmt.Errorf(
			"refresh post-attempt observation: %w",
			postObservationErr,
		)
		return result, failure
	}
	if result.HasErrors() {
		return result, errors.New(string(result.ReasonCode))
	}
	return result, nil
}
