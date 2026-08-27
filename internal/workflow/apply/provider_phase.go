package apply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

type providerStableFingerprintFacts struct {
	ManifestPath     string
	LockfilePath     string
	LockfileExplicit bool
	Targets          []string
	ManageUnmanaged  bool
	DelegateMode     reconcile.OperationContext
	ManagedPaths     []managedPathFingerprintFacts
	Aggregates       []aggregateFingerprintFacts
	RelationActions  []relationFingerprintFacts
	CarrierAbsences  []carrierAbsenceFingerprintFacts
	DelegateActions  []delegateFingerprintFacts
	Owner            ownershipOwnerFingerprintFacts
	Ownership        []ownershipObservationFingerprintFacts
	Diagnostics      []diagnosticFingerprintFacts
	ProjectRoot      *projectRootFingerprintFacts
}

type providerPhaseExecution struct {
	attempts                      []durableattempt.HostRouteAttempt
	leases                        *mutation.LeaseSet
	firstEffectRevisions          mutation.RevisionSet
	rebound                       bool
	uncompensatedEffectsAttempted bool
}

func providerInstallActions(
	prerequisites []readiness.MCPProviderPrerequisite,
) ([]reconcile.RelationAction, error) {
	result := make([]reconcile.RelationAction, 0, len(prerequisites))
	seen := make(map[string]struct{}, len(prerequisites))
	for _, prerequisite := range prerequisites {
		action, present, err := prerequisite.InstallAction()
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		key := action.Subject().String() + "\x00" + action.RouteRequest().RouteID()
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, action)
	}
	return result, nil
}

func prepareMCPProviderPrerequisiteActions(
	current commandPlan,
	preflight recoveryProvenancePreflight,
) ([]reconcile.RelationAction, error) {
	actions, err := providerInstallActions(current.assessment.MCPProviders)
	if err != nil {
		return nil, fmt.Errorf("resolve MCP provider prerequisite actions: %w", err)
	}
	if len(actions) == 0 {
		return actions, nil
	}
	if err := preflightRecoveryJournalProjectRoot(current, preflight); err != nil {
		return nil, fmt.Errorf("preflight MCP provider recovery authority: %w", err)
	}
	return actions, nil
}

func requireCurrentMCPProviders(
	prerequisites []readiness.MCPProviderPrerequisite,
) error {
	for _, prerequisite := range prerequisites {
		if prerequisite.State() == readiness.MCPProviderCurrent {
			continue
		}
		observation := prerequisite.Observation()
		return fmt.Errorf(
			"MCP provider %q is not current after prerequisite execution: state=%s reason=%s",
			observation.Contribution().SubjectID(),
			prerequisite.State(),
			prerequisite.Reason(),
		)
	}
	return nil
}

// providerStableFingerprint captures every disclosed operation fact that a
// provider prerequisite is not authorized to change. Carrier relation,
// provider-version, attempt-history, adoption, and global-claim facts are
// intentionally excluded because the prerequisite phase owns those changes.
func providerStableFingerprint(
	planned commandPlan,
	operationContext reconcile.OperationContext,
) (mutation.OperationFingerprint, error) {
	projectRoot, err := projectRootFingerprint(planned)
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	targets := planned.context.Selection.Targets()
	targetValues := make([]string, 0, len(targets))
	for _, selected := range targets {
		targetValues = append(targetValues, string(selected))
	}
	canonical, err := json.Marshal(providerStableFingerprintFacts{
		ManifestPath:     planned.result.ManifestPath,
		LockfilePath:     planned.result.LockfilePath,
		LockfileExplicit: planned.result.LockfileExplicit,
		Targets:          targetValues,
		ManageUnmanaged:  planned.context.ManageUnmanagedMatches,
		DelegateMode:     operationContext,
		ManagedPaths: managedPathFingerprintRows(
			planned.assessment.Reconciliation.ManagedPaths(),
		),
		Aggregates: aggregateFingerprintRows(
			planned.assessment.Reconciliation.Aggregates(),
		),
		RelationActions: relationFingerprintRows(
			nonProviderRelationActions(planned),
		),
		CarrierAbsences: carrierAbsenceFingerprintRows(
			nonProviderCarrierAbsences(planned),
		),
		DelegateActions: delegateFingerprintRows(
			planned.result.Reconciliation.Delegates(),
		),
		Owner: ownershipOwnerFingerprintFacts{
			StatefileAuthority: pathAuthorityFingerprintFactsFor(
				planned.assessment.Owner.StatefileAuthority(),
			),
			ManifestPath: planned.assessment.Owner.ManifestPath(),
		},
		Ownership:   ownershipFingerprintFacts(planned.assessment.Ownership),
		Diagnostics: diagnosticFingerprintRows(planned.result.Diagnostics),
		ProjectRoot: projectRoot,
	})
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf(
			"fingerprint post-provider apply plan: %w",
			err,
		)
	}
	return mutation.NewOperationFingerprint(canonical), nil
}

func nonProviderRelationActions(planned commandPlan) []reconcile.RelationAction {
	providerCarriers := providerCarrierSubjects(planned)
	result := make([]reconcile.RelationAction, 0)
	for _, action := range planned.assessment.Reconciliation.Relations() {
		if _, provider := providerCarriers[action.CarrierIdentity().CarrierSubject().String()]; provider {
			continue
		}
		result = append(result, action)
	}
	return result
}

func nonProviderCarrierAbsences(planned commandPlan) []carrierabsence.Action {
	providerCarriers := providerCarrierSubjects(planned)
	result := make([]carrierabsence.Action, 0)
	for _, action := range planned.assessment.Reconciliation.CarrierAbsences() {
		carrier := action.Claim().Identity().CarrierSubject().String()
		if _, provider := providerCarriers[carrier]; provider {
			continue
		}
		result = append(result, action)
	}
	return result
}

func providerCarrierSubjects(planned commandPlan) map[string]struct{} {
	result := make(map[string]struct{}, len(planned.assessment.MCPProviders))
	for _, prerequisite := range planned.assessment.MCPProviders {
		result[prerequisite.Observation().Carrier().CarrierSubject().String()] = struct{}{}
	}
	return result
}

func requireSettledMCPProviderRelations(planned commandPlan) error {
	unsettled := providerCarrierSubjects(planned)
	for _, action := range planned.assessment.Reconciliation.Relations() {
		carrier := action.CarrierIdentity().CarrierSubject().String()
		if _, provider := unsettled[carrier]; !provider {
			continue
		}
		if action.Kind() != reconcile.ActionNoOp {
			return fmt.Errorf(
				"MCP provider relation %q is not settled after prerequisite execution: kind=%s reason=%s",
				action.Subject(),
				action.Kind(),
				action.Reason(),
			)
		}
		delete(unsettled, carrier)
	}
	for _, prerequisite := range planned.assessment.MCPProviders {
		carrier := prerequisite.Observation().Carrier().CarrierSubject().String()
		if _, missing := unsettled[carrier]; missing {
			return fmt.Errorf(
				"MCP provider relation %q is absent after prerequisite execution",
				prerequisite.Observation().Carrier().RelationSubject(),
			)
		}
	}
	return nil
}

func delegateFingerprintRows(
	actions []reconcile.DelegateAction,
) []delegateFingerprintFacts {
	rows := make([]delegateFingerprintFacts, 0, len(actions))
	for _, action := range actions {
		rows = append(rows, delegateFingerprintFacts{
			Subject:      action.Subject(),
			Target:       string(action.Target()),
			Scope:        string(action.Scope()),
			Plan:         delegatePlanFingerprint(action.Plan()),
			Status:       action.Disposition(),
			Outcome:      action.PolicyOutcome(),
			Risks:        action.Risks(),
			Dependencies: action.Dependencies(),
		})
	}
	return rows
}

func runMCPProviderPrerequisitePhase(
	ctx context.Context,
	current *commandPlan,
	currentInput CommandInput,
	execution preparedExecution,
	visibleAuthority applyAuthorityEvidence,
	effectPaths daempaths.Paths,
	store mutation.Store,
	leases *mutation.LeaseSet,
	firstEffectRevisions mutation.RevisionSet,
	executionGuard applyExecutionGuard,
	options runOptions,
	planWasDisclosed bool,
) (providerPhaseExecution, error) {
	result := providerPhaseExecution{
		leases:               leases,
		firstEffectRevisions: firstEffectRevisions,
	}
	actions, err := prepareMCPProviderPrerequisiteActions(
		*current,
		options.recoveryProvenancePreflight,
	)
	if err != nil {
		return result, err
	}
	if len(actions) == 0 {
		return result, requireCurrentMCPProviders(current.assessment.MCPProviders)
	}
	stableBefore, err := providerStableFingerprint(*current, execution.operationContext)
	if err != nil {
		return result, err
	}
	providerState := current.assessment.CurrentState
	providerClaims := current.assessment.GlobalCarrierClaims
	providerOptions := options
	providerOptions.markExecutionAttempted = func() {
		result.uncompensatedEffectsAttempted = true
		options.markAttempted()
	}
	for _, action := range actions {
		nextState, nextClaims, attempts, routeErr := runHostRoutesAndPersistAttemptRecords(
			ctx,
			effectPaths,
			current.context.Lockfile,
			current.assessment.StatePath,
			providerState,
			current.assessment.Owner,
			providerClaims,
			[]reconcile.RelationAction{action},
			providerOptions,
		)
		providerState = nextState
		providerClaims = nextClaims
		result.attempts = append(result.attempts, attempts...)
		if routeErr != nil {
			return result, routeErr
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := leases.Release(); err != nil {
		return result, fmt.Errorf("release pre-provider mutation leases: %w", err)
	}
	result.rebound = true

	// Explicit test observations are intentionally discarded here. The
	// post-effect plan must come from the host rather than replaying
	// caller-supplied pre-effect evidence.
	currentInput.RelationObservations = nil
	if err := executionGuard.requireDeclarationsCurrent(
		ctx,
		"post-provider replan",
	); err != nil {
		return result, err
	}
	refreshed, err := planReadinessAtPathsWithBarrier(
		ctx,
		currentInput,
		execution.operationContext,
		current.context.Paths,
		&current.barrier,
	)
	if err != nil {
		return result, providerPhaseStale(
			planWasDisclosed,
			"replan after MCP provider prerequisite",
			err,
		)
	}
	refreshed.projectRoot = current.projectRoot
	if err := validatePostProviderPlan(refreshed); err != nil {
		return result, err
	}
	if err := requireCurrentMCPProviders(refreshed.assessment.MCPProviders); err != nil {
		return result, err
	}
	stableAfter, err := providerStableFingerprint(refreshed, execution.operationContext)
	if err != nil || !stableBefore.Equal(stableAfter) {
		return result, providerPhaseStale(
			planWasDisclosed,
			"disclosed non-provider plan changed after MCP provider prerequisite",
			err,
		)
	}
	refreshedAuthority, err := buildApplyAuthorityEvidence(ctx, refreshed)
	if err != nil {
		return result, fmt.Errorf("derive post-provider apply authority: %w", err)
	}
	if !authorityFactsCover(visibleAuthority.facts, refreshedAuthority.facts) {
		return result, providerPhaseStale(
			planWasDisclosed,
			"MCP provider prerequisite expanded apply authority",
			nil,
		)
	}
	reboundLeases, err := store.Acquire(ctx, refreshedAuthority.domains...)
	if err != nil {
		return result, err
	}
	releaseRebound := true
	defer func() {
		if releaseRebound {
			_ = reboundLeases.Release()
		}
	}()
	if err := refreshed.barrier.Validate(ctx); err != nil {
		return result, err
	}
	if _, err := projectRootFingerprint(refreshed); err != nil {
		return result, providerPhaseStale(
			planWasDisclosed,
			"project root changed after MCP provider prerequisite",
			err,
		)
	}
	reboundFirstEffectRevisions, err := mutation.CaptureRevisionSet(
		ctx,
		refreshedAuthority.firstEffectRevisions...,
	)
	if err != nil {
		return result, err
	}
	if err := executionGuard.requireDeclarationsCurrent(
		ctx,
		"post-provider leased replan",
	); err != nil {
		return result, err
	}

	underLease, err := planReadinessAtPathsWithBarrier(
		ctx,
		currentInput,
		execution.operationContext,
		refreshed.context.Paths,
		&refreshed.barrier,
	)
	if err != nil {
		return result, providerPhaseStale(
			planWasDisclosed,
			"replan under post-provider leases",
			err,
		)
	}
	underLease.projectRoot = current.projectRoot
	if err := validatePostProviderPlan(underLease); err != nil {
		return result, err
	}
	if err := requireCurrentMCPProviders(underLease.assessment.MCPProviders); err != nil {
		return result, err
	}
	underLeaseOperation, err := applyOperationFingerprint(
		underLease,
		execution.operationContext,
	)
	if err != nil {
		return result, err
	}
	refreshedOperation, err := applyOperationFingerprint(
		refreshed,
		execution.operationContext,
	)
	if err != nil || !refreshedOperation.Equal(underLeaseOperation) {
		return result, providerPhaseStale(
			planWasDisclosed,
			"post-provider plan changed while acquiring leases",
			err,
		)
	}
	underLeaseAuthority, err := buildApplyAuthorityEvidence(ctx, underLease)
	if err != nil {
		return result, fmt.Errorf("derive leased post-provider apply authority: %w", err)
	}
	if !refreshedAuthority.authorityFingerprint.Equal(
		underLeaseAuthority.authorityFingerprint,
	) {
		return result, providerPhaseStale(
			planWasDisclosed,
			"post-provider authority changed while acquiring leases",
			nil,
		)
	}
	if matches, err := reboundFirstEffectRevisions.MatchesCurrent(ctx); err != nil {
		return result, err
	} else if !matches {
		return result, providerPhaseStale(
			planWasDisclosed,
			"post-provider revisions changed before execution",
			nil,
		)
	}
	if matches, err := reboundLeases.DomainsMatchCurrent(ctx); err != nil {
		return result, err
	} else if !matches {
		return result, providerPhaseStale(
			planWasDisclosed,
			"post-provider mutation domains changed before execution",
			nil,
		)
	}
	if _, err := projectRootFingerprint(underLease); err != nil {
		return result, providerPhaseStale(
			planWasDisclosed,
			"project root changed under post-provider leases",
			err,
		)
	}
	if err := preflightMCPEnvironmentSources(
		ctx,
		underLease.context.RuntimeEnvironment,
		underLease.context.Selection,
		execution.request.environmentPresent,
	); err != nil {
		return result, err
	}
	if err := executionGuard.requireDeclarationsCurrent(
		ctx,
		"final MCP provider replan",
	); err != nil {
		return result, err
	}
	*current = underLease
	result.leases = reboundLeases
	result.firstEffectRevisions = reboundFirstEffectRevisions
	releaseRebound = false
	return result, nil
}

func providerPhaseStale(disclosed bool, message string, cause error) error {
	if cause == nil {
		cause = errors.New(message)
	} else {
		cause = fmt.Errorf("%s: %w", message, cause)
	}
	return staleApplyError(disclosed, cause)
}

func validatePostProviderPlan(planned commandPlan) error {
	if err := rejectLocalSourceMutationOverlap(planned); err != nil {
		return err
	}
	if err := execute.RejectUnsupportedActions(planned.assessment.Reconciliation); err != nil {
		return err
	}
	if err := rejectBlockedRelationActions(planned.assessment.Reconciliation); err != nil {
		return err
	}
	if err := requireSettledMCPProviderRelations(planned); err != nil {
		return err
	}
	if err := rejectBlockedCarrierAdoptions(planned.assessment.Reconciliation); err != nil {
		return err
	}
	return rejectBlockedCarrierAbsences(
		planned.assessment.Reconciliation.CarrierAbsences(),
	)
}
