package apply

import (
	"context"
	"errors"
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/execute"
	executeconfigrelation "github.com/isty2e/daem/internal/effect/execute/configrelation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
)

type relationAuthorityPathFact struct {
	path   string
	target target.Target
	scope  target.Scope
	access mutation.AccessMode
}

type carrierRemovalPostconditionError struct {
	reason assurancehostroute.ResultReasonCode
}

func (err carrierRemovalPostconditionError) Error() string {
	return fmt.Sprintf("pending carrier removal did not converge: %s", err.reason)
}

func relationAuthorityPathFacts(
	actions []carrierabsence.Action,
	paths []observerelation.AuthorityPath,
) []relationAuthorityPathFact {
	type targetScope struct {
		target target.Target
		scope  target.Scope
	}
	exclusive := make(map[targetScope]struct{})
	for _, action := range actions {
		if action.MutatesDirectProjection() {
			exclusive[targetScope{target: action.Target(), scope: action.Scope()}] = struct{}{}
		}
	}
	result := make([]relationAuthorityPathFact, 0, len(paths))
	for _, path := range paths {
		access := mutation.AccessShared
		if _, present := exclusive[targetScope{
			target: path.Target(),
			scope:  path.Scope(),
		}]; present {
			access = mutation.AccessExclusive
		}
		result = append(result, relationAuthorityPathFact{
			path: path.Path(), target: path.Target(), scope: path.Scope(), access: access,
		})
	}
	return result
}

func runDirectProjectionRemoval(
	ctx context.Context,
	input carrierRemovalInput,
	action carrierabsence.Action,
	result *carrierRemovalResult,
	execution *applyContinuationExecution,
	ref string,
) (resultErr error) {
	var removal executeconfigrelation.RemovalPlan
	var physicalAuthority mutation.PhysicalAuthoritySet
	if err := scheduledContinuationCall(
		execution,
		ref+"/prepare-direct",
		operationplan.EffectStepObservation,
		func() error {
			var err error
			removal, err = executeconfigrelation.NewRemovalPlan(
				executeconfigrelation.RemovalInput{
					Target:         action.Target(),
					Scope:          action.Scope(),
					Carrier:        action.Claim().Identity().Carrier().Family(),
					Source:         string(action.Claim().Identity().ExpectedRelation().SubjectKey()),
					AuthorityPaths: input.RelationAuthorityPaths,
				},
			)
			if err != nil {
				return err
			}
			physicalAuthority, err = removal.PhysicalAuthority()
			return err
		},
	); err != nil {
		return err
	}
	var boundRemoval *executeconfigrelation.BoundRemoval
	boundClosed := false
	closeBound := func() error {
		if boundClosed || boundRemoval == nil {
			return nil
		}
		boundClosed = true
		return input.closeBoundRemoval(boundRemoval)
	}
	if err := scheduledCarrierRemovalCallWithFailureCleanup(
		execution,
		ref+"/bind-direct",
		operationplan.EffectStepObservation,
		func() error {
			var err error
			boundRemoval, err = removal.Bind(input.ProjectRoot, input.SelectedRoot)
			return err
		},
		closeBound,
	); err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, closeBound())
	}()
	if err := scheduledCarrierRemovalForward(
		execution,
		ref+"/forward",
		func() error {
			return validateBeforeRemovalEffects(ctx, input, physicalAuthority)
		},
		closeBound,
	); err != nil {
		return err
	}
	stateAuthority := input.StatefileAuthority
	if stateAuthority == nil {
		return errors.Join(
			fmt.Errorf("carrier removal statefile authority is required"),
			closeBound(),
		)
	}
	if err := scheduledCarrierRemovalEnsure(
		ctx,
		execution,
		ref+"/statefile",
		stateAuthority,
		closeBound,
	); err != nil {
		return err
	}
	var baselines durablecarrier.EffectBaselineSet
	if err := scheduledCarrierRemovalCallWithFailureCleanup(
		execution,
		ref+"/effect-baselines",
		operationplan.EffectStepObservation,
		func() error {
			var err error
			baselines, err = durablecarrier.NewEffectBaselineSet(nil)
			return err
		},
		closeBound,
	); err != nil {
		return err
	}
	var pending durablecarrier.PendingCarrierRemoval
	if err := scheduledCarrierRemovalStatefilePublication(
		execution,
		ref+"/statefile/pending",
		func() error {
			entry, err := stateAuthority.EntryForCommit()
			if err != nil {
				return err
			}
			input.markAttempted()
			next, nextPending, err := execute.CommitPendingCarrierRemoval(
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
				return err
			}
			result.State = next
			pending = nextPending
			return nil
		},
		closeBound,
	); err != nil {
		return err
	}
	if err := scheduledCarrierRemovalStatefileValidation(
		ctx,
		execution,
		ref+"/statefile/pre-effect",
		stateAuthority,
		closeBound,
	); err != nil {
		return fmt.Errorf("validate StateDir before direct carrier removal: %w", err)
	}
	if err := scheduledCarrierRemovalCallWithFailureCleanup(
		execution,
		ref+"/effect",
		operationplan.EffectStepPersistence,
		func() error {
			_, err := boundRemoval.Execute(ctx, filesystem(input))
			if err != nil {
				return fmt.Errorf("execute direct config relation removal: %w", err)
			}
			return nil
		},
		closeBound,
	); err != nil {
		return err
	}
	if err := scheduledCarrierRemovalStatefileValidation(
		ctx,
		execution,
		ref+"/statefile/post-effect",
		stateAuthority,
		closeBound,
	); err != nil {
		return err
	}
	if err := scheduledCarrierRemovalCallWithFailureCleanup(
		execution,
		ref+"/retained-boundary",
		operationplan.EffectStepObservation,
		func() error { return validateRetainedRemovalBoundary(ctx, input) },
		closeBound,
	); err != nil {
		return err
	}
	if err := scheduledCarrierRemovalCallWithFailureCleanup(
		execution,
		ref+"/verify-current",
		operationplan.EffectStepObservation,
		func() error {
			return verifyCurrentRemovalPostconditions(
				ctx,
				input,
				action,
				pending,
				result,
			)
		},
		closeBound,
	); err != nil {
		return err
	}
	if err := retireClaim(
		ctx,
		input,
		action,
		pending,
		stateAuthority,
		result,
		execution,
		ref,
		closeBound,
	); err != nil {
		return err
	}
	return scheduledContinuationCall(
		execution,
		ref+"/bound-close",
		operationplan.EffectStepCleanup,
		closeBound,
	)
}

func verifyCurrentRemovalPostconditions(
	ctx context.Context,
	input carrierRemovalInput,
	action carrierabsence.Action,
	pending durablecarrier.PendingCarrierRemoval,
	result *carrierRemovalResult,
) error {
	if input.Observer == nil {
		return fmt.Errorf("current carrier removal observer is required")
	}
	observation := input.Observer(
		ctx,
		pending,
		append(
			result.State.ManagedCarrierClaims(),
			result.GlobalClaims.Claims()...,
		),
	)
	verification, err := assurancehostroute.VerifyCurrentPostconditions(
		assurancehostroute.CurrentVerificationInput{
			Subject:      action.Subject(),
			RouteRequest: pending.RemoveRequest(),
			Observation:  observation,
			RequiredPostcondition: assurancehostroute.RequirePostconditions(
				assurancehostroute.RelationPostconditionAbsent,
				pending.EffectPostconditions(),
			),
		},
	)
	if err != nil {
		return err
	}
	if !verification.Satisfied() {
		return carrierRemovalPostconditionError{reason: verification.Reason()}
	}
	return nil
}
