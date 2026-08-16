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
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
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
) (resultErr error) {
	removal, err := executeconfigrelation.NewRemovalPlan(
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
	physicalAuthority, err := removal.PhysicalAuthority()
	if err != nil {
		return err
	}
	boundRemoval, err := removal.Bind(input.ProjectRoot, input.SelectedRoot)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, boundRemoval.Close())
	}()
	stateAuthority, err := rootedpath.BindSelectedEntryAuthority(
		input.ProjectRoot,
		input.SelectedRoot,
		input.StatePath,
	)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, stateAuthority.Close())
	}()
	if err := validateBeforeRemovalEffects(ctx, input, physicalAuthority); err != nil {
		return err
	}

	baselines, err := durablecarrier.NewEffectBaselineSet(nil)
	if err != nil {
		return err
	}
	input.markAttempted()
	next, pending, err := execute.CommitPendingCarrierRemoval(
		ctx,
		filesystem(input),
		stateAuthority,
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

	if _, err := boundRemoval.Execute(ctx, filesystem(input)); err != nil {
		return fmt.Errorf("execute direct config relation removal: %w", err)
	}
	if err := validateRetainedRemovalBoundary(ctx, input); err != nil {
		return err
	}
	if err := verifyCurrentRemovalPostconditions(ctx, input, action, pending, result); err != nil {
		return err
	}
	return retireClaim(ctx, input, action, pending, stateAuthority, result)
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
