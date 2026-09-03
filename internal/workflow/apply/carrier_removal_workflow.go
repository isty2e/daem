package apply

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/execute"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/subprocess"
)

// CarrierRemovalObserver returns current exact relation and effect facts for
// one durable pending removal. It must not execute the route or treat durable
// state as current host evidence.
type CarrierRemovalObserver func(
	context.Context,
	durablecarrier.PendingCarrierRemoval,
	[]durablecarrier.ManagedCarrierClaim,
) assurancehostroute.ObservationFact

// CarrierRemovalBaselineObserver captures immutable pre-effect facts for one
// fresh invoking action. It must not execute the route or derive facts from a
// previous pending removal.
type CarrierRemovalBaselineObserver func(
	context.Context,
	carrierabsence.Action,
) (durablecarrier.EffectBaselineSet, error)

// GlobalClaimRemover retires one exact global claim only when its dedicated
// compare-and-swap registry still equals the supplied confirmed baseline.
type carrierRemovalGlobalClaimRemover func(
	context.Context,
	durablecarrier.GlobalCarrierClaims,
	durablecarrier.ManagedCarrierClaim,
) (durablecarrier.GlobalCarrierClaims, error)

// Input supplies already-authorized operation facts and effect capabilities.
// It does not perform planning or host-specific route selection.
type carrierRemovalInput struct {
	StatePath                 string
	SelectedRoot              string
	Current                   durable.Snapshot
	GlobalClaims              durablecarrier.GlobalCarrierClaims
	Actions                   []carrierabsence.Action
	RelationAuthorityPaths    []observerelation.AuthorityPath
	ProjectRoot               *rootedpath.CapturedRoot
	Filesystem                mutationfs.RootedStore
	Adapter                   executehostroute.RemovalAdapter
	Executor                  subprocess.CommandExecutor
	Observer                  CarrierRemovalObserver
	BaselineObserver          CarrierRemovalBaselineObserver
	RemoveGlobalClaim         carrierRemovalGlobalClaimRemover
	ValidateBeforeEffects     func(context.Context, mutation.PhysicalAuthoritySet) error
	ValidateStateDir          func(context.Context) error
	ReserveStatefileAuthority reserveStatefileEffectAuthority
	StatefileAuthority        *statefileEffectAuthority
	MarkExecutionAttempted    func()
	Clock                     func() time.Time
}

func (input carrierRemovalInput) markAttempted() {
	if input.MarkExecutionAttempted != nil {
		input.MarkExecutionAttempted()
	}
}

// Result reports the last durably committed state and bounded attempt history.
type carrierRemovalResult struct {
	State        durable.Snapshot
	GlobalClaims durablecarrier.GlobalCarrierClaims
	Attempts     []durableattempt.HostRouteAttempt
	ActionCount  int
}

// Run executes admitted carrier removals in canonical action order and stops
// after the first non-converged or indeterminate action.
func runCarrierRemovals(
	ctx context.Context,
	input carrierRemovalInput,
) (carrierRemovalResult, error) {
	result := carrierRemovalResult{
		State:        input.Current,
		GlobalClaims: input.GlobalClaims,
	}
	if ctx == nil {
		return result, fmt.Errorf("carrier removal context is required")
	}
	for index, action := range input.Actions {
		if err := action.Validate(); err != nil {
			return result, fmt.Errorf("carrier removal action[%d]: %w", index, err)
		}
	}
	stateAuthority := input.StatefileAuthority
	ownedStateAuthority := false
	if stateAuthority == nil {
		plan, err := carrierRemovalStatefileEffectPlan(input.Actions)
		if err != nil {
			return result, err
		}
		stateAuthority, err = newStatefileEffectAuthority(
			input.StatePath,
			plan,
			input.ReserveStatefileAuthority,
		)
		if err != nil {
			return result, err
		}
		ownedStateAuthority = stateAuthority != nil
	}
	input.StatefileAuthority = stateAuthority
	if ownedStateAuthority {
		defer stateAuthority.Close()
	}
	for index, action := range input.Actions {
		if action.VerifiesPendingRemoval() {
			if err := settlePendingRemoval(ctx, input, action, &result); err != nil {
				return result, fmt.Errorf(
					"carrier removal action[%d] %s: %w",
					index,
					action.Subject().String(),
					err,
				)
			}
			result.ActionCount++
			continue
		}
		var err error
		switch {
		case action.InvokesHostRoute():
			err = runOne(ctx, input, action, &result)
		case action.MutatesDirectProjection():
			err = runDirectProjectionRemoval(ctx, input, action, &result)
		default:
			continue
		}
		if err != nil {
			return result, fmt.Errorf(
				"carrier removal action[%d] %s: %w",
				index,
				action.Subject().String(),
				err,
			)
		}
		result.ActionCount++
	}
	return result, nil
}

func runOne(
	ctx context.Context,
	input carrierRemovalInput,
	action carrierabsence.Action,
	result *carrierRemovalResult,
) error {
	stateAuthority := input.StatefileAuthority
	ensureStateAuthority := func() error {
		if stateAuthority == nil {
			return fmt.Errorf("carrier removal statefile authority is required")
		}
		return stateAuthority.Ensure(ctx)
	}

	command, err := executehostroute.BuildRemovalCommand(executehostroute.RemovalBuildInput{
		Action:  action,
		WorkDir: input.SelectedRoot,
		Adapter: input.Adapter,
	})
	if err != nil {
		record, recordErr := preflightAttempt(action, err, now(input))
		if recordErr != nil {
			return errors.Join(err, recordErr)
		}
		if validationErr := validateBeforeRemovalEffects(
			ctx,
			input,
			mutation.PhysicalAuthoritySet{},
		); validationErr != nil {
			return errors.Join(err, validationErr)
		}
		if authorityErr := ensureStateAuthority(); authorityErr != nil {
			return errors.Join(err, authorityErr)
		}
		if persistErr := persistAttempt(ctx, input, stateAuthority, result, record); persistErr != nil {
			return errors.Join(err, persistErr)
		}
		return errors.Join(
			hostRouteFailuresError([]durableattempt.HostRouteAttempt{record}),
			err,
		)
	}

	binding, err := input.ProjectRoot.AcquireSelectedWorkingDirectory(input.SelectedRoot)
	if err != nil {
		if validationErr := validateBeforeRemovalEffects(
			ctx,
			input,
			mutation.PhysicalAuthoritySet{},
		); validationErr != nil {
			return errors.Join(err, validationErr)
		}
		if authorityErr := ensureStateAuthority(); authorityErr != nil {
			return errors.Join(err, authorityErr)
		}
		attempt := input.Executor.ExecuteInWorkingDirectory(
			ctx,
			command.AttemptRequest(),
			func() (subprocess.WorkingDirectoryBinding, error) { return nil, err },
		)
		classified, classifyErr := classify(
			command,
			attempt,
			assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationUnavailable,
			),
			action.RouteAdmission().Operation().EffectPostconditions(),
		)
		if classifyErr != nil {
			return errors.Join(err, classifyErr)
		}
		record, recordErr := durableAttempt(action, classified, true)
		if recordErr != nil {
			return errors.Join(err, recordErr)
		}
		if persistErr := persistAttempt(ctx, input, stateAuthority, result, record); persistErr != nil {
			return errors.Join(err, persistErr)
		}
		return errors.Join(
			hostRouteFailuresError([]durableattempt.HostRouteAttempt{record}),
			err,
		)
	}

	baselines := durablecarrier.EffectBaselineSet{}
	if input.BaselineObserver != nil {
		baselines, err = input.BaselineObserver(ctx, action)
		if err != nil {
			return errors.Join(
				fmt.Errorf("capture carrier removal baselines: %w", err),
				binding.Close(),
			)
		}
	}
	if err := validateBeforeRemovalEffects(
		ctx,
		input,
		mutation.PhysicalAuthoritySet{},
	); err != nil {
		return errors.Join(err, binding.Close())
	}
	if err := ensureStateAuthority(); err != nil {
		return errors.Join(err, binding.Close())
	}
	entry, err := stateAuthority.EntryForCommit()
	if err != nil {
		return errors.Join(err, binding.Close())
	}
	input.markAttempted()
	next, pending, err := execute.CommitPendingCarrierRemoval(
		ctx,
		filesystem(input),
		entry,
		result.State,
		result.GlobalClaims,
		action,
		baselines,
		statefile.Codec{},
	)
	if err != nil {
		return errors.Join(err, binding.Close())
	}
	result.State = next

	if err := ctx.Err(); err != nil {
		return errors.Join(err, binding.Close())
	}
	if err := stateAuthority.Validate(ctx); err != nil {
		return errors.Join(
			fmt.Errorf("validate StateDir before carrier removal command: %w", err),
			binding.Close(),
		)
	}
	attempt, releaseErr := executeAttempt(ctx, input.Executor, command, binding)
	if authorityErr := stateAuthority.Validate(ctx); authorityErr != nil {
		if errors.Is(authorityErr, context.Canceled) || errors.Is(authorityErr, context.DeadlineExceeded) {
			return errors.Join(authorityErr, releaseErr)
		}
		classified, classifyErr := classify(
			command,
			attempt,
			assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationUnavailable,
			),
			pending.EffectPostconditions(),
		)
		if classifyErr != nil {
			return errors.Join(authorityErr, classifyErr, releaseErr)
		}
		record, recordErr := durableAttempt(action, classified, true)
		if recordErr != nil {
			return errors.Join(authorityErr, recordErr, releaseErr)
		}
		result.Attempts = append(result.Attempts, record)
		return errors.Join(
			hostRouteFailuresError([]durableattempt.HostRouteAttempt{record}),
			authorityErr,
			releaseErr,
		)
	}
	observation := assurancehostroute.ObservationUnavailable(
		assurancehostroute.ResultReasonObservationUnavailable,
	)
	if input.Observer != nil {
		observation = input.Observer(
			ctx,
			pending,
			append(
				result.State.ManagedCarrierClaims(),
				result.GlobalClaims.Claims()...,
			),
		)
	}
	classified, classifyErr := classify(
		command,
		attempt,
		observation,
		pending.EffectPostconditions(),
	)
	if classifyErr != nil {
		classified, err = classify(
			command,
			attempt,
			assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationParseFailed,
			),
			pending.EffectPostconditions(),
		)
		if err != nil {
			return errors.Join(classifyErr, err, releaseErr)
		}
	}
	boundaryErr := validateRetainedRemovalBoundary(ctx, input)
	record, recordErr := durableAttempt(action, classified, boundaryErr != nil)
	if recordErr != nil {
		return errors.Join(classifyErr, recordErr, boundaryErr, releaseErr)
	}
	var attemptFailure error
	if removalAttemptFailed(classified, record) {
		attemptFailure = hostRouteFailuresError([]durableattempt.HostRouteAttempt{record})
	}
	if err := persistAttempt(ctx, input, stateAuthority, result, record); err != nil {
		return errors.Join(attemptFailure, classifyErr, err, boundaryErr, releaseErr)
	}
	if classifyErr != nil || boundaryErr != nil || releaseErr != nil {
		return errors.Join(attemptFailure, classifyErr, boundaryErr, releaseErr)
	}
	if attemptFailure != nil {
		return attemptFailure
	}
	return retireClaim(ctx, input, action, pending, stateAuthority, result)
}

// settlePendingRemoval verifies and retires one previously attempted removal
// from fresh current evidence without manufacturing or repeating an attempt.
func settlePendingRemoval(
	ctx context.Context,
	input carrierRemovalInput,
	action carrierabsence.Action,
	result *carrierRemovalResult,
) error {
	pending, present := action.PendingRemoval()
	if !present {
		return fmt.Errorf("pending-removal verification has no exact durable fact")
	}
	if err := verifyCurrentRemovalPostconditions(
		ctx,
		input,
		action,
		pending,
		result,
	); err != nil {
		return err
	}
	if err := validateBeforeRemovalEffects(
		ctx,
		input,
		mutation.PhysicalAuthoritySet{},
	); err != nil {
		return err
	}
	stateAuthority := input.StatefileAuthority
	if stateAuthority == nil {
		return fmt.Errorf("carrier removal statefile authority is required")
	}
	if err := stateAuthority.Ensure(ctx); err != nil {
		return err
	}
	return retireClaim(ctx, input, action, pending, stateAuthority, result)
}

func validateBeforeRemovalEffects(
	ctx context.Context,
	input carrierRemovalInput,
	authority mutation.PhysicalAuthoritySet,
) error {
	if err := validateRetainedRemovalBoundary(ctx, input); err != nil {
		return err
	}
	if input.ValidateBeforeEffects != nil {
		return input.ValidateBeforeEffects(ctx, authority)
	}
	return nil
}

func validateRetainedRemovalBoundary(ctx context.Context, input carrierRemovalInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if input.ProjectRoot == nil {
		return rootedpath.NewBoundaryFailure(
			rootedpath.FailureRootUnavailable,
			input.SelectedRoot,
			"carrier removal requires retained project-root authority",
			nil,
		)
	}
	if err := input.ProjectRoot.ValidateSelection(input.SelectedRoot); err != nil {
		return err
	}
	return nil
}

func classify(
	command executehostroute.Command,
	attempt subprocess.CommandAttemptResult,
	observation assurancehostroute.ObservationFact,
	effectPostconditions effectpostcondition.Set,
) (assurancehostroute.Result, error) {
	return assurancehostroute.ClassifyResult(assurancehostroute.ResultInput{
		Subject:      command.Subject(),
		RouteRequest: command.RouteRequest(),
		Attempt:      assurancehostroute.ObservedAttempt(attempt, assurancehostroute.AttemptReason(attempt.Reason())),
		Observation:  observation,
		RequiredPostcondition: assurancehostroute.RequirePostconditions(
			assurancehostroute.RelationPostconditionAbsent,
			effectPostconditions,
		),
	})
}

func executeAttempt(
	ctx context.Context,
	executor subprocess.CommandExecutor,
	command executehostroute.Command,
	binding subprocess.WorkingDirectoryBinding,
) (subprocess.CommandAttemptResult, error) {
	transferred := false
	attempt := executor.ExecuteInWorkingDirectory(
		ctx,
		command.AttemptRequest(),
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

func durableAttempt(
	action carrierabsence.Action,
	result assurancehostroute.Result,
	workDirAuthorityLost bool,
) (durableattempt.HostRouteAttempt, error) {
	return assurancehostroute.NewDurableAttempt(assurancehostroute.DurableAttemptInput{
		Result:               result,
		Target:               action.Target(),
		Scope:                action.Scope(),
		Operation:            lock.OperationRemove,
		WorkDirAuthorityLost: workDirAuthorityLost,
	})
}

func preflightAttempt(
	action carrierabsence.Action,
	err error,
	observedAt time.Time,
) (durableattempt.HostRouteAttempt, error) {
	class := durableattempt.HostRouteResultBlockedPreflight
	reason := durableattempt.HostRouteReasonPreflightFailed
	var validation *executehostroute.ValidationError
	if errors.As(err, &validation) {
		reason = durableattempt.HostRouteResultReason(validation.Code())
		switch validation.Code() {
		case executehostroute.ReasonUnsupportedSource:
			class = durableattempt.HostRouteResultUnsupportedSource
		case executehostroute.ReasonUnsupportedScope:
			class = durableattempt.HostRouteResultUnsupportedScope
		}
	}
	request := action.RouteAdmission().Request()
	return durableattempt.NewHostRouteAttempt(durableattempt.HostRouteAttemptInput{
		Subject:          action.Subject(),
		Target:           action.Target(),
		Scope:            action.Scope(),
		Operation:        lock.OperationRemove,
		RouteID:          request.RouteID(),
		RouteRequestHash: request.CanonicalRequestHash(),
		ObservedAt:       observedAt,
		ResultClass:      class,
		Reason:           reason,
		Observation:      observerelation.ObservationNotObserved,
		Postcondition:    observerelation.PostconditionNotObserved,
	})
}

func persistAttempt(
	ctx context.Context,
	input carrierRemovalInput,
	authority *statefileEffectAuthority,
	result *carrierRemovalResult,
	record durableattempt.HostRouteAttempt,
) error {
	if err := authority.Validate(ctx); err != nil {
		return err
	}
	entry, err := authority.EntryForCommit()
	if err != nil {
		return err
	}
	input.markAttempted()
	next, err := execute.CommitHostRouteAttempts(
		ctx,
		filesystem(input),
		entry,
		result.State,
		[]durableattempt.HostRouteAttempt{record},
		statefile.Codec{},
	)
	if err != nil {
		return fmt.Errorf("persist carrier removal attempt: %w", err)
	}
	result.State = next
	result.Attempts = append(result.Attempts, record)
	return authority.Validate(ctx)
}

func removalVerified(result assurancehostroute.Result) bool {
	return result.PostconditionsSatisfied()
}

func removalAttemptFailed(
	result assurancehostroute.Result,
	record durableattempt.HostRouteAttempt,
) bool {
	return !removalVerified(result) ||
		record.Reason() == durableattempt.HostRouteReasonWorkDirAuthority
}

func now(input carrierRemovalInput) time.Time {
	if input.Clock != nil {
		return input.Clock().UTC()
	}
	return time.Now().UTC()
}

func filesystem(input carrierRemovalInput) mutationfs.RootedStore {
	if input.Filesystem != nil {
		return input.Filesystem
	}
	return storagecommit.Adapter{}
}
