package apply

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/execute"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

func runHostRoutesAndPersistAttemptRecords(
	ctx context.Context,
	paths daempaths.Paths,
	locked lock.File,
	statePath string,
	current durable.Snapshot,
	owner stateauthority.Authority,
	globalCarrierClaims durablecarrier.GlobalCarrierClaims,
	relationActions []reconciliation.RelationAction,
	options runOptions,
) (
	resultState durable.Snapshot,
	resultClaims durablecarrier.GlobalCarrierClaims,
	resultRecords []durableattempt.HostRouteAttempt,
	returnErr error,
) {
	attemptedPhase := ""
	defer func() {
		if attemptedPhase == "" {
			return
		}
		returnErr = errors.Join(
			returnErr,
			options.executionGuard.requireDeclarationsCurrent(
				ctx,
				"after "+attemptedPhase,
			),
		)
	}()

	records := make([]durableattempt.HostRouteAttempt, 0)
	failures := make([]durableattempt.HostRouteAttempt, 0)
	prepared := make([]preparedHostRoute, 0)
	globalPromotions := make([]reconciliation.RelationAction, 0)
	for _, action := range relationActions {
		if isGlobalCarrierPromotionCandidate(current, action) {
			globalPromotions = append(globalPromotions, action)
		}
		if !action.InvokesHostRoute() {
			continue
		}
		command, err := executehostroute.BuildCommand(executehostroute.BuildInput{
			Action:   action,
			Lockfile: locked,
			WorkDir:  paths.ManifestRoot,
		})
		if err != nil {
			record, recordErr := durableAttemptFromHostRoutePreflight(action, err, time.Now().UTC())
			if recordErr != nil {
				return current, globalCarrierClaims, records, fmt.Errorf("compose host route preflight attempt: %w", recordErr)
			}
			records = append(records, record)
			failures = append(failures, record)
			continue
		}
		prepared = append(prepared, preparedHostRoute{action: action, command: command})
	}

	nextState := current
	emptyAuthority, err := mutation.NewPhysicalAuthoritySet()
	if err != nil {
		return current, globalCarrierClaims, records, err
	}
	var stateAuthority *rootedpath.EntryAuthority
	if len(records) != 0 || len(prepared) != 0 || len(globalPromotions) != 0 {
		if options.validateBeforeEffects == nil {
			return current, globalCarrierClaims, records, fmt.Errorf(
				"host route effect validation is required",
			)
		}
		if len(records) != 0 || len(globalPromotions) != 0 {
			if err := options.validateBeforeEffects(ctx, emptyAuthority); err != nil {
				return current, globalCarrierClaims, records, err
			}
		}
		if err := validateHostRouteProjectRoot(options, paths.ManifestRoot); err != nil {
			return nextState, globalCarrierClaims, records, errors.Join(hostRouteFailuresError(failures), err)
		}
		stateAuthority, err = rootedpath.BindSelectedEntryAuthority(
			options.projectRoot,
			paths.ManifestRoot,
			statePath,
		)
		if err != nil {
			return nextState, globalCarrierClaims, records, errors.Join(hostRouteFailuresError(failures), err)
		}
		defer stateAuthority.Close()
	}
	if len(records) != 0 {
		var err error
		options.markAttempted()
		nextState, err = execute.CommitHostRouteAttempts(
			ctx,
			storagecommit.Adapter{},
			stateAuthority,
			nextState,
			records,
			statefile.Codec{},
		)
		if err != nil {
			return nextState, globalCarrierClaims, records, fmt.Errorf("persist host route preflight record: %w", err)
		}
		if err := validateHostRouteProjectRoot(options, paths.ManifestRoot); err != nil {
			return nextState, globalCarrierClaims, records, errors.Join(hostRouteFailuresError(failures), err)
		}
	}
	if len(globalPromotions) != 0 {
		options.markAttempted()
	}
	nextState, globalCarrierClaims, err = commitInterruptedGlobalCarrierClaims(
		ctx,
		paths,
		stateAuthority,
		nextState,
		globalCarrierClaims,
		globalPromotions,
		options,
	)
	if err != nil {
		return nextState, globalCarrierClaims, records, fmt.Errorf(
			"promote interrupted global carrier install: %w",
			err,
		)
	}

	for index, item := range prepared {
		phase := fmt.Sprintf("host route[%d]", index)
		if err := options.validateBeforeEffects(ctx, emptyAuthority); err != nil {
			return nextState, globalCarrierClaims, records, errors.Join(
				hostRouteFailuresError(failures),
				err,
			)
		}
		binding, bindErr := acquireHostRouteWorkingDirectory(options, paths.ManifestRoot)
		if bindErr != nil {
			attemptedPhase = phase
			attempt := options.HostRouteExecutor.ExecuteInWorkingDirectory(
				ctx,
				item.command.AttemptRequest(),
				func() (subprocess.WorkingDirectoryBinding, error) { return nil, bindErr },
			)
			result, classifyErr := assurancehostroute.ClassifyResult(assurancehostroute.ResultInput{
				Subject:      item.command.Subject(),
				RouteRequest: item.command.RouteRequest(),
				Attempt:      observedHostRouteAttempt(attempt),
				Observation: assurancehostroute.ObservationUnavailable(
					assurancehostroute.ResultReasonObservationUnavailable,
				),
				RequiredPostcondition: installRelationPostcondition(item.action),
			})
			if classifyErr != nil {
				return nextState, globalCarrierClaims, records, errors.Join(bindErr, classifyErr)
			}
			record, recordErr := durableAttemptFromHostRouteResult(item.action, result, false)
			if recordErr != nil {
				return nextState, globalCarrierClaims, records, errors.Join(bindErr, recordErr)
			}
			records = append(records, record)
			failures = append(failures, record)
			return nextState, globalCarrierClaims, records, errors.Join(
				hostRouteFailuresError(failures),
				bindErr,
			)
		}
		var err error
		options.markAttempted()
		nextState, err = execute.CommitPendingCarrierInstalls(
			ctx,
			storagecommit.Adapter{},
			stateAuthority,
			nextState,
			owner,
			[]reconciliation.RelationAction{item.action},
			statefile.Codec{},
		)
		if err != nil {
			_ = binding.Close()
			return nextState, globalCarrierClaims, records, errors.Join(hostRouteFailuresError(failures), fmt.Errorf("persist pending carrier install: %w", err))
		}
		attemptedPhase = phase
		attempt, bindingReleaseErr := executeHostRouteAttempt(
			ctx,
			options.HostRouteExecutor,
			item.command.AttemptRequest(),
			binding,
		)
		observation := assurancehostroute.ObservationUnavailable(assurancehostroute.ResultReasonObservationUnavailable)
		if options.HostRouteObserver != nil {
			observation = options.HostRouteObserver(
				ctx,
				item.command,
				nextState.PendingCarrierInstalls(),
				append(
					nextState.ManagedCarrierClaims(),
					globalCarrierClaims.Claims()...,
				),
			)
		}
		result, classifyErr := assurancehostroute.ClassifyResult(assurancehostroute.ResultInput{
			Subject:               item.command.Subject(),
			RouteRequest:          item.command.RouteRequest(),
			Attempt:               observedHostRouteAttempt(attempt),
			Observation:           observation,
			RequiredPostcondition: installRelationPostcondition(item.action),
		})
		if classifyErr != nil {
			classificationFailure := fmt.Errorf(
				"classify host route post-attempt observation: %w",
				classifyErr,
			)
			fallbackResult, fallbackErr := assurancehostroute.ClassifyResult(assurancehostroute.ResultInput{
				Subject:      item.command.Subject(),
				RouteRequest: item.command.RouteRequest(),
				Attempt:      observedHostRouteAttempt(attempt),
				Observation: assurancehostroute.ObservationUnavailable(
					assurancehostroute.ResultReasonObservationParseFailed,
				),
				RequiredPostcondition: installRelationPostcondition(item.action),
			})
			if fallbackErr != nil {
				return nextState, globalCarrierClaims, records, errors.Join(classificationFailure, fallbackErr, bindingReleaseErr)
			}
			rootErr := validateHostRouteProjectRoot(options, paths.ManifestRoot)
			if rootErr != nil {
				record, recordErr := durableAttemptFromHostRouteResult(
					item.action,
					fallbackResult,
					true,
				)
				if recordErr != nil {
					return nextState, globalCarrierClaims, records, errors.Join(
						classificationFailure,
						recordErr,
						rootErr,
						bindingReleaseErr,
					)
				}
				records = append(records, record)
				failures = append(failures, record)
				return nextState, globalCarrierClaims, records, errors.Join(
					hostRouteFailuresError(failures),
					classificationFailure,
					rootErr,
					bindingReleaseErr,
				)
			}
			nextState, err = execute.CommitRetiredPendingCarrierInstall(
				ctx,
				storagecommit.Adapter{},
				stateAuthority,
				nextState,
				owner,
				item.action,
				statefile.Codec{},
			)
			if err != nil {
				return nextState, globalCarrierClaims, records, errors.Join(
					classificationFailure,
					fmt.Errorf("retire completed carrier install: %w", err),
					bindingReleaseErr,
				)
			}
			record, recordErr := durableAttemptFromHostRouteResult(
				item.action,
				fallbackResult,
				false,
			)
			if recordErr != nil {
				return nextState, globalCarrierClaims, records, errors.Join(classificationFailure, recordErr, bindingReleaseErr)
			}
			records = append(records, record)
			failures = append(failures, record)
			nextState, err = execute.CommitHostRouteAttempts(
				ctx,
				storagecommit.Adapter{},
				stateAuthority,
				nextState,
				[]durableattempt.HostRouteAttempt{record},
				statefile.Codec{},
			)
			if err != nil {
				return nextState, globalCarrierClaims, records, errors.Join(
					hostRouteFailuresError(failures),
					classificationFailure,
					fmt.Errorf("persist host route attempt record: %w", err),
					bindingReleaseErr,
				)
			}
			if err := validateHostRouteProjectRoot(options, paths.ManifestRoot); err != nil {
				return nextState, globalCarrierClaims, records, errors.Join(
					hostRouteFailuresError(failures),
					classificationFailure,
					err,
					bindingReleaseErr,
				)
			}
			if bindingReleaseErr != nil {
				return nextState, globalCarrierClaims, records, errors.Join(
					hostRouteFailuresError(failures),
					classificationFailure,
					fmt.Errorf(
						"release unused host route working-directory authority: %w",
						bindingReleaseErr,
					),
				)
			}
			return nextState, globalCarrierClaims, records, errors.Join(
				hostRouteFailuresError(failures),
				classificationFailure,
			)
		}
		rootErr := validateHostRouteProjectRoot(options, paths.ManifestRoot)
		if rootErr != nil {
			record, recordErr := durableAttemptFromHostRouteResult(item.action, result, true)
			if recordErr != nil {
				return nextState, globalCarrierClaims, records, errors.Join(recordErr, rootErr, bindingReleaseErr)
			}
			records = append(records, record)
			failures = append(failures, record)
			return nextState, globalCarrierClaims, records, errors.Join(hostRouteFailuresError(failures), rootErr)
		}
		if correlation, observed := observation.Correlation(); observed && result.Class() == assurancehostroute.ResultAttemptedObservedPresent {
			if item.action.Scope() == target.ScopeGlobal {
				nextState, globalCarrierClaims, err = commitObservedGlobalCarrierClaim(
					ctx,
					paths,
					stateAuthority,
					nextState,
					globalCarrierClaims,
					item.action,
					correlation,
				)
			} else {
				nextState, err = execute.CommitObservedProjectCarrierClaim(
					ctx,
					storagecommit.Adapter{},
					stateAuthority,
					nextState,
					item.action,
					correlation,
					statefile.Codec{},
				)
			}
			if err != nil {
				return nextState, globalCarrierClaims, records, errors.Join(
					hostRouteFailuresError(failures),
					fmt.Errorf("persist observed carrier claim: %w", err),
					bindingReleaseErr,
				)
			}
		}
		nextState, err = execute.CommitRetiredPendingCarrierInstall(
			ctx,
			storagecommit.Adapter{},
			stateAuthority,
			nextState,
			owner,
			item.action,
			statefile.Codec{},
		)
		if err != nil {
			return nextState, globalCarrierClaims, records, errors.Join(
				hostRouteFailuresError(failures),
				fmt.Errorf("retire completed carrier install: %w", err),
				bindingReleaseErr,
			)
		}
		record, recordErr := durableAttemptFromHostRouteResult(item.action, result, false)
		if recordErr != nil {
			return nextState, globalCarrierClaims, records, errors.Join(recordErr, bindingReleaseErr)
		}
		records = append(records, record)
		if hostRouteResultFailed(result) {
			failures = append(failures, record)
		}
		nextState, err = execute.CommitHostRouteAttempts(
			ctx,
			storagecommit.Adapter{},
			stateAuthority,
			nextState,
			[]durableattempt.HostRouteAttempt{record},
			statefile.Codec{},
		)
		if err != nil {
			return nextState, globalCarrierClaims, records, errors.Join(hostRouteFailuresError(failures), fmt.Errorf("persist host route attempt record: %w", err), bindingReleaseErr)
		}
		if err := validateHostRouteProjectRoot(options, paths.ManifestRoot); err != nil {
			return nextState, globalCarrierClaims, records, errors.Join(
				hostRouteFailuresError(failures),
				err,
				bindingReleaseErr,
			)
		}
		if bindingReleaseErr != nil {
			return nextState, globalCarrierClaims, records, errors.Join(hostRouteFailuresError(failures), fmt.Errorf("release unused host route working-directory authority: %w", bindingReleaseErr))
		}
		declarationErr := options.executionGuard.requireDeclarationsCurrent(
			ctx,
			"after "+phase,
		)
		attemptedPhase = ""
		if declarationErr != nil {
			return nextState, globalCarrierClaims, records, errors.Join(
				hostRouteFailuresError(failures),
				declarationErr,
			)
		}
	}
	return nextState, globalCarrierClaims, records, hostRouteFailuresError(failures)
}

func executeHostRouteAttempt(
	ctx context.Context,
	executor subprocess.CommandExecutor, request subprocess.CommandAttemptRequest, binding subprocess.WorkingDirectoryBinding,
) (subprocess.CommandAttemptResult, error) {
	transferred := false
	attempt := executor.ExecuteInWorkingDirectory(
		ctx,
		request,
		func() (subprocess.WorkingDirectoryBinding, error) {
			transferred = true
			return binding, nil
		},
	)
	if transferred {
		return attempt, nil
	}
	return attempt, binding.Close()
}

func hostRouteFailuresError(records []durableattempt.HostRouteAttempt) error {
	if len(records) == 0 {
		return nil
	}
	return hostRouteExecutionError{records: records}
}

type preparedHostRoute struct {
	action  reconciliation.RelationAction
	command executehostroute.Command
}

type hostRouteExecutionError struct {
	records []durableattempt.HostRouteAttempt
}

func (err hostRouteExecutionError) Error() string {
	if len(err.records) == 0 {
		return "host route attempt failed"
	}
	parts := make([]string, 0, len(err.records))
	for _, record := range err.records {
		parts = append(parts, fmt.Sprintf(
			"%s/%s %q: %s/%s",
			record.Subject().Kind(),
			record.Subject().Namespace(),
			record.Subject().Key(),
			record.ResultClass(),
			record.Reason(),
		))
	}
	return "host route attempt failed: " + strings.Join(parts, "; ")
}

func durableAttemptFromHostRouteResult(
	action reconciliation.RelationAction,
	result assurancehostroute.Result,
	workDirAuthorityLost bool,
) (durableattempt.HostRouteAttempt, error) {
	return assurancehostroute.NewDurableAttempt(assurancehostroute.DurableAttemptInput{
		Result:               result,
		Target:               action.Target(),
		Scope:                action.Scope(),
		Operation:            lock.OperationInstall,
		WorkDirAuthorityLost: workDirAuthorityLost,
	})
}

func durableAttemptFromHostRoutePreflight(
	action reconciliation.RelationAction,
	err error,
	observedAt time.Time,
) (durableattempt.HostRouteAttempt, error) {
	subject := action.Subject()
	route := action.RouteRequest()
	class := durableattempt.HostRouteResultBlockedPreflight
	reason := durableattempt.HostRouteReasonPreflightFailed
	var validationErr *executehostroute.ValidationError
	if errors.As(err, &validationErr) {
		reason = durableattempt.HostRouteResultReason(validationErr.Code())
		switch validationErr.Code() {
		case executehostroute.ReasonUnsupportedSource:
			class = durableattempt.HostRouteResultUnsupportedSource
		case executehostroute.ReasonUnsupportedScope:
			class = durableattempt.HostRouteResultUnsupportedScope
		default:
			class = durableattempt.HostRouteResultBlockedPreflight
		}
	}
	return durableattempt.NewHostRouteAttempt(durableattempt.HostRouteAttemptInput{
		Subject:          subject,
		Target:           action.Target(),
		Scope:            action.Scope(),
		Operation:        lock.OperationInstall,
		RouteID:          route.RouteID(),
		RouteRequestHash: route.CanonicalRequestHash(),
		ObservedAt:       observedAt,
		ResultClass:      class,
		Reason:           reason,
		AttemptObserved:  false,
		Observation:      observerelation.ObservationNotObserved,
		Postcondition:    observerelation.PostconditionNotObserved,
	})
}

func hostRouteResultFailed(result assurancehostroute.Result) bool {
	switch result.Class() {
	case assurancehostroute.ResultFailed,
		assurancehostroute.ResultAttemptedObservedAbsent,
		assurancehostroute.ResultAmbiguousObservation,
		assurancehostroute.ResultBlocked:
		return true
	default:
		return false
	}
}

func observedHostRouteAttempt(result subprocess.CommandAttemptResult) assurancehostroute.AttemptFact {
	return assurancehostroute.ObservedAttempt(
		result,
		assurancehostroute.AttemptReason(result.Reason()),
	)
}
