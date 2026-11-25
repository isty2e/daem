package apply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/execute/nativeoperation"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	ownershipobserve "github.com/isty2e/daem/internal/assurance/observe/ownership"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/diagnose"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	executedelegate "github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/effect/fileset"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/findings"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/recoverygate"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
	"github.com/isty2e/daem/internal/workflow/readiness"
	"github.com/isty2e/daem/internal/workflow/workspacecleanup"
)

var (
	ErrReadLockfile         = errors.New("read lockfile")
	ErrRelationActionBlock  = errors.New("relation action blocked")
	ErrRelationOrderBlock   = errors.New("relation order blocked")
	ErrCarrierAdoptionBlock = errors.New("carrier adoption blocked")
	ErrCarrierAbsenceBlock  = errors.New("carrier absence blocked")
)

// FailureReason is the closed public reason for an unsuccessful apply.
type FailureReason string

const (
	FailureReasonStaleSnapshot                 FailureReason = "stale_snapshot"
	FailureReasonStalePlan                     FailureReason = "stale_plan"
	FailureReasonMutationContended             FailureReason = "mutation_contended"
	FailureReasonCancelled                     FailureReason = "mutation_cancelled"
	FailureReasonLockfileUnavailable           FailureReason = "lockfile_unavailable"
	FailureReasonRelationActionBlocked         FailureReason = "relation_action_blocked"
	FailureReasonRelationOrderBlocked          FailureReason = "relation_order_blocked"
	FailureReasonCarrierAdoptionBlocked        FailureReason = "carrier_adoption_blocked"
	FailureReasonCarrierAbsenceBlocked         FailureReason = "carrier_absence_blocked"
	FailureReasonRelationOrderRiskExpanded     FailureReason = "relation_order_risk_expanded"
	FailureReasonRelationOrderUnauthorized     FailureReason = "relation_order_not_authorized"
	FailureReasonMCPEnvironmentUnavailable     FailureReason = "mcp_environment_unavailable"
	FailureReasonMCPBackendTransition          FailureReason = "mcp_backend_transition_unsupported"
	FailureReasonDelegateAttemptFailed         FailureReason = "delegate_attempt_failed"
	FailureReasonHostRouteAttemptFailed        FailureReason = "host_route_attempt_failed"
	FailureReasonCarrierPostconditionFailed    FailureReason = "carrier_removal_postcondition_failed"
	FailureReasonInterruptedApply              FailureReason = "interrupted_apply"
	FailureReasonInterruptedApplyFileSetFence  FailureReason = "interrupted_apply_file_set_fence"
	FailureReasonJournalCleanupIncomplete      FailureReason = "journal_cleanup_incomplete"
	FailureReasonJournalCleanupFileSetFence    FailureReason = "journal_cleanup_file_set_fence"
	FailureReasonInterruptedFileSetTransaction FailureReason = "interrupted_file_set_transaction"
	FailureReasonFileSetEvidenceInvalid        FailureReason = "file_set_evidence_invalid"
	FailureReasonAbandonedFileSetResidue       FailureReason = "abandoned_file_set_residue"
	FailureReasonFileSetFenceCensusLimit       FailureReason = "file_set_fence_census_limit"
	FailureReasonFileSetAccessUnprovable       FailureReason = "file_set_access_unprovable"
	FailureReasonRecoveryCleanup               FailureReason = "recovery_cleanup_failed"
	FailureReasonApplyRefused                  FailureReason = "apply_refused"
	FailureReasonApplyIncomplete               FailureReason = "apply_incomplete"
)

// FailurePhase identifies where apply stopped relative to its effect boundary.
type FailurePhase string

const (
	FailurePhasePreflight FailurePhase = "preflight"
	FailurePhaseExecution FailurePhase = "execution"
)

// FailureOutcome states whether apply crossed an effect boundary.
type FailureOutcome string

const (
	FailureOutcomeRefused    FailureOutcome = "refused"
	FailureOutcomeIncomplete FailureOutcome = "incomplete"
	FailureOutcomeRolledBack FailureOutcome = "rolled_back"
)

// Failure is a path-neutral public projection of an internal apply error.
type Failure struct {
	reason                 FailureReason
	phase                  FailurePhase
	outcome                FailureOutcome
	barrier                recoverygate.State
	backendTransition      *readiness.MCPBackendTransitionError
	nativeSupport          nativeSupportReason
	documentAdmission      *nativeoperation.DocumentAdmissionError
	staleAgentConfigSource entity.ID
	missingState           *ownershipobserve.MissingStateRefusal
	cleanupAttempted       bool
	cleanupRecordsCleared  bool
}

// ClassifyFailure derives public failure facts without copying internal error text.
func ClassifyFailure(err error, result CommandResult) Failure {
	phase := FailurePhasePreflight
	outcome := FailureOutcomeRefused
	if result.ExecutionAttempted {
		phase = FailurePhaseExecution
		outcome = FailureOutcomeIncomplete
		if applyprogram.HostChangesRolledBack(err) && !result.UncompensatedEffectsAttempted {
			outcome = FailureOutcomeRolledBack
		}
	}

	failure := Failure{
		reason:  classifyFailureReason(err, result.ExecutionAttempted),
		phase:   phase,
		outcome: outcome,
		barrier: recoverygate.StateOf(err),
	}
	if phase == FailurePhasePreflight && failure.reason != FailureReasonCancelled {
		errors.As(err, &failure.missingState)
	}
	failure.cleanupRecordsCleared = len(result.RecoveryCleanup) != 0
	for _, step := range result.RecoveryCleanup {
		if step.Outcome != "" && step.Outcome != workspacecleanup.NotAttempted {
			failure.cleanupAttempted = true
		}
		failure.cleanupRecordsCleared = failure.cleanupRecordsCleared && step.Outcome == workspacecleanup.Retired
	}
	switch failure.reason {
	case FailureReasonMCPBackendTransition:
		errors.As(err, &failure.backendTransition)
	case FailureReasonApplyRefused:
		var nativeSupport nativeSupportError
		errors.As(err, &nativeSupport)
		failure.nativeSupport = nativeSupport.reason
		errors.As(err, &failure.documentAdmission)

		if phase == FailurePhasePreflight && !result.ReconciliationReady && !failure.barrier.Observed() &&
			failure.nativeSupport == 0 && failure.documentAdmission == nil {
			var stale lockobserve.NativeSourceStaleError
			if errors.As(err, &stale) && stale.DeclarationID.Kind() == entity.KindAgentConfig {
				failure.staleAgentConfigSource = stale.DeclarationID
			}
		}
	}
	return failure
}

func classifyFailureReason(err error, executionAttempted bool) FailureReason {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return FailureReasonCancelled
	}
	state := recoverygate.StateOf(err)
	switch state.FileSet() {
	case fileset.FileSetFenceAccessUnprovable:
		return FailureReasonFileSetAccessUnprovable
	case fileset.FileSetFenceInvalidEvidence:
		return FailureReasonFileSetEvidenceInvalid
	}
	if state.JournalKnown() && state.Journal() == journal.InterruptionActiveApply && state.HasContinuingFileSetFence() {
		return FailureReasonInterruptedApplyFileSetFence
	}
	if state.JournalKnown() && state.Journal() == journal.InterruptionCleanupOnly && state.HasContinuingFileSetFence() {
		return FailureReasonJournalCleanupFileSetFence
	}
	if state.JournalKnown() {
		switch state.Journal() {
		case journal.InterruptionActiveApply:
			return FailureReasonInterruptedApply
		case journal.InterruptionCleanupOnly:
			return FailureReasonJournalCleanupIncomplete
		}
	}
	if state.FileSetKnown() {
		switch state.FileSet() {
		case fileset.FileSetFencePublishedTransaction:
			return FailureReasonInterruptedFileSetTransaction
		case fileset.FileSetFenceAbandonedResidue:
			return FailureReasonAbandonedFileSetResidue
		case fileset.FileSetFenceCensusLimit:
			return FailureReasonFileSetFenceCensusLimit
		}
	}
	if reason, ok := mutation.ReasonCodeOf(err); ok {
		switch reason {
		case mutation.ReasonStaleSnapshot:
			return FailureReasonStaleSnapshot
		case mutation.ReasonStalePlan:
			return FailureReasonStalePlan
		case mutation.ReasonContention:
			return FailureReasonMutationContended
		case mutation.ReasonCanceled:
			return FailureReasonCancelled
		}
	}
	switch {
	case errors.Is(err, errRecoveryCleanupFailed) && !recoverygate.IsGlobalMetadataRefusal(err):
		return FailureReasonRecoveryCleanup
	case errors.Is(err, ErrReadLockfile):
		return FailureReasonLockfileUnavailable
	case errors.Is(err, ErrRelationActionBlock):
		return FailureReasonRelationActionBlocked
	case errors.Is(err, ErrRelationOrderBlock):
		return FailureReasonRelationOrderBlocked
	case errors.Is(err, ErrCarrierAdoptionBlock):
		return FailureReasonCarrierAdoptionBlocked
	case errors.Is(err, ErrCarrierAbsenceBlock):
		return FailureReasonCarrierAbsenceBlocked
	case errors.Is(err, ErrRelationOrderRiskExpansion):
		return FailureReasonRelationOrderRiskExpanded
	case errors.Is(err, ErrRelationOrderNotAuthorized):
		return FailureReasonRelationOrderUnauthorized
	}

	var missingEnvironment missingMCPEnvironmentSourcesError
	var transition *readiness.MCPBackendTransitionError
	if errors.As(err, &transition) {
		return FailureReasonMCPBackendTransition
	}
	if errors.As(err, &missingEnvironment) {
		return FailureReasonMCPEnvironmentUnavailable
	}
	var delegateFailure executedelegate.ExecutionError
	if errors.As(err, &delegateFailure) {
		return FailureReasonDelegateAttemptFailed
	}
	var hostRouteFailure hostRouteExecutionError
	if errors.As(err, &hostRouteFailure) {
		return FailureReasonHostRouteAttemptFailed
	}
	var carrierPostconditionFailure carrierRemovalPostconditionError
	if errors.As(err, &carrierPostconditionFailure) {
		return FailureReasonCarrierPostconditionFailed
	}
	if executionAttempted {
		return FailureReasonApplyIncomplete
	}
	return FailureReasonApplyRefused
}

func (failure Failure) Reason() FailureReason   { return failure.reason }
func (failure Failure) Phase() FailurePhase     { return failure.phase }
func (failure Failure) Outcome() FailureOutcome { return failure.outcome }

// RecoveryBarrier returns the independently preserved journal and file-set
// evidence associated with this failure.
func (failure Failure) RecoveryBarrier() recoverygate.State { return failure.barrier }

// StaleAgentConfigSource returns the declaration for an otherwise unclassified
// stale-source refusal before reconciliation.
func (failure Failure) StaleAgentConfigSource() (entity.ID, bool) {
	return failure.staleAgentConfigSource, failure.staleAgentConfigSource != (entity.ID{})
}

// Detail derives bounded public prose only from closed failure facts.
func (failure Failure) Detail() string {
	detail := failure.withOutcomeFacts(failure.reasonDetail())
	if failure.missingState != nil {
		detail += "; " + failure.missingState.Error()
	}
	return detail
}

// PrimaryDetail retains failure facts without presenter-owned repair advice.
// Detail remains the ordinary apply result's compatibility projection.
func (failure Failure) PrimaryDetail() string {
	var detail string
	switch failure.reason {
	case FailureReasonMCPBackendTransition:
		detail = "Pi MCP backend transition is unsupported"
		if failure.backendTransition != nil {
			detail = failure.backendTransition.Summary()
		}
	case FailureReasonInterruptedApply, FailureReasonInterruptedApplyFileSetFence:
		detail = "interrupted apply journal is present"
	case FailureReasonJournalCleanupIncomplete, FailureReasonJournalCleanupFileSetFence:
		detail = "journal cleanup is incomplete"
	case FailureReasonInterruptedFileSetTransaction:
		detail = "an interrupted file-set transaction is present"
	case FailureReasonFileSetEvidenceInvalid:
		detail = "file-set transaction evidence is incomplete or invalid"
	case FailureReasonAbandonedFileSetResidue:
		detail = "abandoned file-set residue remains"
	case FailureReasonFileSetFenceCensusLimit:
		detail = "the bounded file-set census could not prove the fence clean"
	case FailureReasonFileSetAccessUnprovable:
		detail = "StateDir access or identity could not be proven"
	default:
		detail = failure.reasonDetail()
	}
	return failure.withOutcomeFacts(detail)
}

func (failure Failure) MCPBackendRepairInstruction() string {
	if failure.reason != FailureReasonMCPBackendTransition {
		return ""
	}
	if failure.backendTransition != nil {
		return failure.backendTransition.RepairInstruction()
	}
	return "retire the existing binding before re-authoring"
}

func (failure Failure) NativeSupportRepairInstruction() string {
	if failure.reason != FailureReasonApplyRefused {
		return ""
	}
	return failure.nativeSupport.repairInstruction()
}

func (failure Failure) withOutcomeFacts(detail string) string {
	if failure.barrier.JournalObserved() && !failure.barrier.JournalKnown() {
		detail += "; journal recovery authority could not be classified"
	}
	if failure.barrier.FileSetObserved() && !failure.barrier.FileSetKnown() {
		detail += "; file-set fence could not be classified"
	}
	if failure.outcome == FailureOutcomeRolledBack {
		return detail + "; host changes were rolled back"
	}
	return detail
}

func (failure Failure) reasonDetail() string {
	if failure.reason == FailureReasonApplyRefused && failure.documentAdmission != nil {
		return failure.documentAdmission.Error()
	}
	if failure.reason == FailureReasonApplyRefused && failure.nativeSupport != 0 {
		return failure.nativeSupport.detail()
	}
	switch failure.reason {
	case FailureReasonStaleSnapshot:
		return "authoritative inputs changed before apply completed"
	case FailureReasonStalePlan:
		return "the authorized apply plan changed before apply completed"
	case FailureReasonMutationContended:
		return "required mutation authority is busy"
	case FailureReasonCancelled:
		if failure.outcome != FailureOutcomeRefused {
			return "apply was cancelled after an effect boundary was crossed"
		}
		if failure.cleanupAttempted {
			return "cancelled after recovery cleanup began, before normal apply effects"
		}
		return "apply was cancelled before effects"
	case FailureReasonLockfileUnavailable:
		return "the selected lockfile is unavailable"
	case FailureReasonRelationActionBlocked:
		return "a selected extension relation is blocked"
	case FailureReasonRelationOrderBlocked:
		return "a selected extension order is blocked"
	case FailureReasonCarrierAdoptionBlocked:
		return "a selected carrier adoption is blocked"
	case FailureReasonCarrierAbsenceBlocked:
		return "a selected carrier removal is blocked"
	case FailureReasonRelationOrderRiskExpanded:
		return "extension order risk expanded after carrier changes"
	case FailureReasonRelationOrderUnauthorized:
		return "the updated extension order was not authorized"
	case FailureReasonMCPEnvironmentUnavailable:
		return "required MCP environment sources are unavailable"
	case FailureReasonMCPBackendTransition:
		if failure.backendTransition != nil {
			return failure.backendTransition.Error()
		}
		return "Pi MCP backend transition is unsupported; retire the existing binding before re-authoring"
	case FailureReasonDelegateAttemptFailed:
		return "a delegated host command attempt failed"
	case FailureReasonHostRouteAttemptFailed:
		return "host route attempt failed"
	case FailureReasonCarrierPostconditionFailed:
		return "pending carrier removal postconditions are not satisfied"
	case FailureReasonInterruptedApply:
		return "interrupted apply journal is present; run daem recover --dry-run first"
	case FailureReasonInterruptedApplyFileSetFence:
		return "interrupted apply journal is present; run daem recover --dry-run first; the file-set fence remains after recover and is not cleared by it"
	case FailureReasonJournalCleanupIncomplete:
		return "journal cleanup is incomplete; run daem recover --dry-run first"
	case FailureReasonJournalCleanupFileSetFence:
		return "journal cleanup is incomplete; run daem recover --dry-run first; the file-set fence remains afterward"
	case FailureReasonInterruptedFileSetTransaction:
		return "an interrupted file-set transaction requires its owning workflow to recover it before apply"
	case FailureReasonFileSetEvidenceInvalid:
		return "file-set transaction evidence is incomplete or invalid; preserve and repair it before apply or recover"
	case FailureReasonAbandonedFileSetResidue:
		return "abandoned file-set residue remains; preserve it for analysis; do not retry apply or delete it from its name prefix"
	case FailureReasonFileSetFenceCensusLimit:
		return "the bounded file-set census could not prove the fence clean; inspect or reduce StateDir entries before apply"
	case FailureReasonFileSetAccessUnprovable:
		return "StateDir access or identity could not be proven; restore access before apply or recover"
	case FailureReasonRecoveryCleanup:
		if failure.cleanupRecordsCleared {
			return "the approved recovery records were cleared, but cleanup ended with an error; normal apply did not start"
		}
		return "approved recovery cleanup did not complete; normal apply did not start"
	case FailureReasonApplyIncomplete:
		return "apply did not complete after an effect boundary was crossed"
	default:
		if failure.cleanupAttempted {
			return "normal apply was refused after recovery cleanup began"
		}
		return "apply was refused before effects"
	}
}

type CommandInput struct {
	ManifestPath           string
	LockfilePath           string
	TargetValues           []string
	RelationObservations   *relationobserve.Batch
	ManageUnmanagedMatches bool
	environmentPresent     environmentSourcePresence
	nativeReplacement      *topologyprojection.AgentConfigKeySelection
	persistenceBudget      *physicalwork.Budget
	ownedRequalification   bool
}

type CommandResult struct {
	CarrierHandoffProgress  *CarrierHandoffProgress
	RecoveryCleanup         []workspacecleanup.Step
	PermissionProtection    ownershipobserve.ProtectionObservation
	ManifestPath            string
	LockfilePath            string
	LockfileExplicit        bool
	StatefilePath           string
	Reconciliation          reconcile.Result
	ReconciliationReady     bool
	AgentConfigDeclarations []entity.ID // nil until a complete assessment is available
	DelegateAttempts        []DelegateAttemptResult
	RelationOrderResults    []RelationOrderExecutionResult
	HostRouteAttempts       []durableattempt.HostRouteAttempt
	CarrierAdoptionResults  []durablecarrier.ManagedCarrierClaim
	Diagnostics             []findings.Diagnostic
	LockOnly                []readiness.UnsupportedProjection
	MCPProjections          []mcpobserve.LockedProjectionObservation
	ActionCount             int
	// ExecutionAttempted reports whether normal apply crossed a durable mutation
	// or delegated command-runner boundary. Independent format maintenance is
	// not an attempt to execute the disclosed operation.
	ExecutionAttempted bool
	// UncompensatedEffectsAttempted reports that an effect outside the managed
	// journal rollback domain was attempted during this apply.
	UncompensatedEffectsAttempted bool
}

// HasBlockedRelationActions reports whether relation planning blocks ordinary apply.
func (result CommandResult) HasBlockedRelationActions() bool {
	return result.Reconciliation.HasBlockedRelations()
}

type commandContext struct {
	Paths                        daempaths.Paths
	RuntimeEnvironment           desired.Environment
	Lockfile                     lock.File
	Selection                    targetselection.Selection
	SourceEpoch                  lockobserve.SourceEpoch
	PersistenceEpoch             readiness.PersistenceEpoch
	physicalBudget               *physicalwork.Budget
	RelationObservationsExplicit bool
	ManageUnmanagedMatches       bool
	nativeReplacement            *topologyprojection.AgentConfigKeySelection
}

type commandPlan struct {
	result             CommandResult
	context            commandContext
	assessment         readiness.Assessment
	projectRoot        *rootedpath.CapturedRoot
	barrier            recoverygate.EffectAuthority
	cleanup            *workspacecleanup.Plan
	nativePlan         *nativePlanInputs
	nativeOperation    *nativeconfig.AssessmentOperation
	handoffReservation []carrierabsence.Action
}

func (planned commandPlan) stateAdmission() ownershipobserve.StateAdmission {
	return ownershipobserve.ForPlannedState(planned.context.PersistenceEpoch.MissingState())
}

// DryRunPlan is a capability-free dry-run result plus the immutable inputs
// needed to render optional diffs without reloading command state.
type DryRunPlan struct {
	CommandResult
	planned commandPlan
}

func PlanDryRun(ctx context.Context, input CommandInput) (DryRunPlan, error) {
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return DryRunPlan{}, err
	}
	planned, err := planReadinessAtPaths(ctx, input, reconcile.ContextDryRun, paths)
	if !planned.result.ReconciliationReady && canPlanRecoveryCleanup(input, paths, planned.result.LockfilePath) && workspacecleanup.MayQualify(err) {
		return planDryRunWithRecoveryCleanup(ctx, input, paths, planned, err)
	}
	if err != nil {
		return newDryRunPlan(planned), err
	}
	if err := rejectLocalSourceMutationOverlap(planned); err != nil {
		return newDryRunPlan(planned), err
	}
	if err := rejectUnsupportedPreviewActions(planned.assessment); err != nil {
		return newDryRunPlan(planned), err
	}

	return newDryRunPlan(planned), nil
}

func rejectUnsupportedPreviewActions(assessment readiness.Assessment) error {
	if err := checkNativeCommandSupport(assessment); err != nil {
		return err
	}
	if len(assessment.NativeSources) == 0 && !assessment.CurrentState.ContainsAgentConfig() && !assessment.Reconciliation.ContainsAgentConfig() {
		return execute.RejectUnsupportedExecutableActions(assessment.Reconciliation)
	}
	var paths []reconcile.ManagedPathDecision
	for _, decision := range assessment.Reconciliation.ManagedPaths() {
		if !decision.IsBlocked() && !decision.IsNoOp() && !aggregate.IsAgentConfigDocumentSubject(decision.Subject()) {
			paths = append(paths, decision)
		}
	}
	if _, err := effectpreparation.ManagedPathEffects(paths); err != nil {
		return err
	}
	var aggregates []reconcile.AggregateDecision
	for _, decision := range assessment.Reconciliation.Aggregates() {
		if _, native := aggregate.AgentConfigSyntaxForCodec(decision.CodecContractID()); !native && !decision.IsBlocked() && !decision.IsNoOp() {
			aggregates = append(aggregates, decision)
		}
	}
	_, err := effectpreparation.AggregateEffects(aggregates)
	return err
}

// PlanWrite builds an executable apply plan and retains its selected physical
// project-root witness. The caller must call PreparedWrite.Close unless it
// passes the result to Execute or ExecuteWithOptions, which consume and close it.
func PlanWrite(ctx context.Context, input CommandInput) (prepared *PreparedWrite, returnErr error) {
	operationContext := reconcile.ContextApply
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return unavailablePreparedWrite(CommandResult{}), err
	}
	// Early barrier and declaration refusals precede normal input observation.
	// Retain diagnostic facts without granting an execution capability.
	defer func() {
		if returnErr != nil && prepared != nil && prepared.PermissionProtection.State() == ownershipobserve.ProtectionUnobserved {
			prepared.ManifestPath = paths.ManifestPath
			prepared.StatefilePath = paths.StatefilePath
			prepared.PermissionProtection = ownershipobserve.ObservePendingProtections(ctx, paths)
		}
	}()
	barrier, err := recoverygate.NewEffectAuthority(ctx, paths)
	if err != nil {
		return unavailablePreparedWrite(CommandResult{}), err
	}
	// Root authority must precede every declaration read, including revision
	// hashing, so planning never adopts a replacement project root.
	root, rootCaptureErr := captureProjectRootAuthorityBeforeLoad(paths)
	defer func() {
		if root != nil {
			if err := root.Close(); err != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("close pre-load apply project-root witness: %w", err))
			}
		}
	}()
	lockfilePath, err := selectedLockfilePath(paths, input.LockfilePath)
	if err != nil {
		return unavailablePreparedWrite(CommandResult{}), err
	}
	declarationRevisions, err := captureDeclarationRevisions(
		ctx,
		paths.ManifestPath,
		lockfilePath,
	)
	if err != nil {
		if barrierErr := barrier.Validate(ctx); barrierErr != nil {
			return unavailablePreparedWrite(CommandResult{}), errors.Join(err, barrierErr)
		}
		return unavailablePreparedWrite(CommandResult{}), fmt.Errorf(
			"capture pre-plan apply declaration revisions: %w",
			commandInputError{cause: err},
		)
	}
	var pending *workspacecleanup.Pending
	var pendingFence error
	var validate func(context.Context) error
	if canPlanRecoveryCleanup(input, paths, lockfilePath) {
		validate = func(ctx context.Context) error {
			fenceErr := barrier.Validate(ctx)
			var err error
			pending, err = workspacecleanup.Observe(ctx, barrier, fenceErr)
			if pending != nil {
				pendingFence = fenceErr
			}
			return recoveryCleanupPlanningError(fenceErr, err)
		}
	}
	planned, err := planReadinessAtPathsWithBarrierValidation(ctx, input, operationContext, paths, &barrier, validate)
	if err != nil {
		return unavailablePreparedWrite(planned.result), pendingRecoveryInputError(pendingFence, err)
	}
	if err := rejectUnsupportedCommandPlan(planned); err != nil {
		return unavailablePreparedWrite(planned.result), err
	}
	if err := rejectBlockedCarrierHandoffPlan(planned.assessment.Reconciliation); err != nil {
		return unavailablePreparedWrite(planned.result), err
	}
	if err := rejectBlockedRelationOrders(planned.assessment.ReservationReconciliation()); err != nil {
		return unavailablePreparedWrite(planned.result), err
	}
	if err := preflightMCPEnvironmentSources(
		ctx,
		planned.context.RuntimeEnvironment,
		planned.context.Selection,
		input.environmentPresent,
	); err != nil {
		return unavailablePreparedWrite(planned.result), err
	}
	if err := retainProjectRootAuthority(&planned, root, rootCaptureErr); err != nil {
		return unavailablePreparedWrite(planned.result), err
	}
	if pending != nil {
		if rootCaptureErr != nil {
			return unavailablePreparedWrite(planned.result), rootCaptureErr
		}
		if err := root.ValidateSelection(paths.ManifestRoot); err != nil {
			return unavailablePreparedWrite(planned.result), err
		}
		planned.projectRoot = root
	}
	if planned.projectRoot != nil {
		root = nil
	}
	operationEvidence, err := applyOperationFingerprint(planned, operationContext)
	if err != nil {
		closeErr := closeCommandPlan(&planned)
		return unavailablePreparedWrite(planned.result), errors.Join(err, closeErr)
	}
	authorityEvidence, err := buildApplyAuthorityEvidence(ctx, planned)
	if err != nil {
		closeErr := closeCommandPlan(&planned)
		return unavailablePreparedWrite(planned.result), errors.Join(
			fmt.Errorf("derive apply mutation authority: %w", err),
			closeErr,
		)
	}
	if err := rejectLocalSourceMutationOverlap(planned); err != nil {
		closeErr := closeCommandPlan(&planned)
		return unavailablePreparedWrite(planned.result), errors.Join(err, closeErr)
	}
	declarationsCurrent, err := declarationRevisions.MatchesCurrent(ctx)
	if err != nil {
		closeErr := closeCommandPlan(&planned)
		return unavailablePreparedWrite(planned.result), errors.Join(
			fmt.Errorf("revalidate apply declarations after planning: %w", err),
			closeErr,
		)
	}
	if !declarationsCurrent {
		closeErr := closeCommandPlan(&planned)
		return unavailablePreparedWrite(planned.result), errors.Join(
			staleApplyError(
				false,
				errors.New("manifest or selected lockfile changed while planning"),
			),
			closeErr,
		)
	}
	if pending != nil {
		if err := sealRecoveryCleanup(ctx, &planned, pending, authorityEvidence); err != nil {
			return unavailablePreparedWrite(planned.result), errors.Join(err, closeCommandPlan(&planned))
		}
		if err := newApplyExecutionGuard(declarationRevisions, false).requireDeclarationsCurrent(ctx, "seal apply cleanup plan"); err != nil {
			planned.result.RecoveryCleanup = nil
			return unavailablePreparedWrite(planned.result), errors.Join(err, closeCommandPlan(&planned))
		}
	}

	return newPreparedWrite(
		planned,
		cloneCommandInput(input),
		operationContext,
		operationEvidence,
		authorityEvidence,
		declarationRevisions,
	), nil
}

func BuildDiffs(ctx context.Context, result DryRunPlan) (DryRunDiffCollection, error) {
	return BuildDryRunDiffs(
		ctx,
		result.planned.context.Paths,
		result.planned.context.Lockfile,
		result.planned.context.SourceEpoch,
		result.planned.result.Reconciliation,
		nil,
		result.planned.context.physicalBudget,
		result.planned.assessment.NativeApproval,
	)
}

func planReadiness(ctx context.Context, input CommandInput, operationContext reconcile.OperationContext) (commandPlan, error) {
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return commandPlan{}, err
	}
	return planReadinessAtPaths(ctx, input, operationContext, paths)
}

func planReadinessAtPaths(
	ctx context.Context,
	input CommandInput,
	operationContext reconcile.OperationContext,
	paths daempaths.Paths,
) (commandPlan, error) {
	return planReadinessAtPathsWithBarrier(ctx, input, operationContext, paths, nil)
}

func planReadinessAtPathsWithBarrier(
	ctx context.Context,
	input CommandInput,
	operationContext reconcile.OperationContext,
	paths daempaths.Paths,
	barrier *recoverygate.EffectAuthority,
) (commandPlan, error) {
	return planReadinessAtPathsWithBarrierValidation(
		ctx,
		input,
		operationContext,
		paths,
		barrier,
		nil,
	)
}

func planReadinessAtPathsWithAssessor(
	ctx context.Context,
	input CommandInput,
	operationContext reconcile.OperationContext,
	paths daempaths.Paths,
	barrier *recoverygate.EffectAuthority,
	validateBarrier func(context.Context) error,
	assessor readinessAssessor,
) (commandPlan, error) {
	loaded, result, err := loadCommandInputsAtPaths(ctx, input, paths, barrier, validateBarrier)
	planned := commandPlan{result: result, context: loaded}
	if barrier != nil {
		planned.barrier = *barrier
	}
	if err != nil {
		return planned, err
	}

	planning, observation, err := assessor(ctx, readiness.Input{
		Context:                 operationContext,
		Paths:                   loaded.Paths,
		Resolver:                liveobserve.DestinationResolver(destinationResolver(loaded.Paths).ForBudget(loaded.physicalBudget).Resolve),
		Environment:             loaded.RuntimeEnvironment,
		Lockfile:                loaded.Lockfile,
		Selection:               loaded.Selection,
		SourceEpoch:             &loaded.SourceEpoch,
		PersistenceEpoch:        &loaded.PersistenceEpoch,
		RelationObservations:    input.RelationObservations,
		ManageUnmanagedMatches:  input.ManageUnmanagedMatches,
		NativeReplacement:       input.nativeReplacement,
		Codecs:                  aggregatecodec.Catalog(),
		HookContributionEncoder: hookcodec.CanonicalHookContribution,
		MCPContributionEncoder:  mcpcodec.CanonicalMCPBindingContribution,
	})
	planned.nativeOperation = observation
	if err != nil {
		return planned, err
	}

	result.Reconciliation = planning.Reconciliation
	result.ReconciliationReady = true
	result.AgentConfigDeclarations = loaded.RuntimeEnvironment.AgentConfigIDs()
	result.Diagnostics = append(
		result.Diagnostics,
		diagnose.RetainedSkillDiscoveryDiagnostics(
			ctx,
			loaded.Paths,
			loaded.RuntimeEnvironment.Skills(),
			loaded.Selection,
			planning.Reconciliation,
		)...,
	)
	if err := ctx.Err(); err != nil {
		planned.result = result
		planned.assessment = planning
		return planned, err
	}
	result.MCPProjections = planning.MCPProjections

	planned.result = result
	planned.assessment = planning
	if operationContext == reconcile.ContextApply || operationContext == reconcile.ContextDryRun {
		if err := admitNativeCommandPlan(ctx, &planned); err != nil {
			return planned, err
		}
	}
	return planned, nil
}

func rejectBlockedRelationActions(result reconcile.Result) error {
	action, blocked := result.FirstBlockedRelation()
	if !blocked {
		return nil
	}
	return fmt.Errorf(
		"%w: subject=%s/%s/%s kind=%s reason=%s",
		ErrRelationActionBlock,
		action.Subject().Kind(),
		action.Subject().Namespace(),
		action.Subject().Key(),
		action.Kind(),
		action.Reason(),
	)
}

func rejectBlockedRelationOrders(result reconcile.Result) error {
	decision, blocked := result.FirstBlockedRelationOrder()
	if !blocked {
		return nil
	}
	return fmt.Errorf(
		"%w: target=%s scope=%s class=%s sequence=%s reason=%s detail=%s",
		ErrRelationOrderBlock,
		decision.Target(),
		decision.Scope(),
		decision.ClassID(),
		decision.SequenceID(),
		decision.Reason(),
		decision.PublicDetail(),
	)
}

func rejectBlockedCarrierAdoptions(result reconcile.Result) error {
	action, blocked := result.FirstBlockedCarrierAdoption()
	if !blocked {
		return nil
	}
	return fmt.Errorf(
		"%w: subject=%s/%s/%s target=%s scope=%s result=%s",
		ErrCarrierAdoptionBlock,
		action.Subject().Kind(),
		action.Subject().Namespace(),
		action.Subject().Key(),
		action.Target(),
		action.Scope(),
		action.Result(),
	)
}

func rejectBlockedCarrierAbsences(actions []carrierabsence.Action) error {
	for _, action := range actions {
		if !action.BlocksOrdinaryApply() {
			continue
		}
		return fmt.Errorf(
			"%w: subject=%s/%s/%s target=%s scope=%s decision=%s",
			ErrCarrierAbsenceBlock,
			action.Subject().Kind(),
			action.Subject().Namespace(),
			action.Subject().Key(),
			action.Target(),
			action.Scope(),
			action.Decision(),
		)
	}
	return nil
}

func loadCommandInputsAtPaths(
	ctx context.Context,
	input CommandInput,
	paths daempaths.Paths,
	barrier *recoverygate.EffectAuthority,
	validateBarrier func(context.Context) error,
) (commandContext, CommandResult, error) {
	result := CommandResult{
		ManifestPath:         paths.ManifestPath,
		StatefilePath:        paths.StatefilePath,
		LockfileExplicit:     input.LockfilePath != "",
		PermissionProtection: ownershipobserve.ObservePendingProtections(ctx, paths),
	}
	lockfilePath, err := selectedLockfilePath(paths, input.LockfilePath)
	if err != nil {
		return commandContext{}, result, err
	}
	result.LockfilePath = lockfilePath

	if barrier != nil {
		if validateBarrier == nil {
			validateBarrier = barrier.Validate
		}
		if err := validateBarrier(ctx); err != nil {
			return commandContext{}, result, fmt.Errorf("validate recovery barrier before apply planning: %w", err)
		}
	} else if err := refuseJournalAndFileSet(ctx, paths); err != nil {
		return commandContext{}, result, err
	}

	environment, err := declarationmanifest.LoadSelected(ctx, paths)
	if err != nil {
		if _, input := declarationmanifest.InputErrorCause(err); input {
			return commandContext{}, result, commandInputError{cause: fmt.Errorf("invalid manifest: %w", err)}
		}
		return commandContext{}, result, fmt.Errorf("resolve selected manifest context: %w", err)
	}

	if input.nativeReplacement != nil {
		environment, err = environment.AgentConfigOnly(input.nativeReplacement.Declaration())
		if err != nil {
			return commandContext{}, result, err
		}
		input.TargetValues = []string{string(environment.AgentConfigs()[0].Target())}
	}
	locked, lockfileErr := lockfile.Load(ctx, result.LockfilePath)
	if lockfileErr == nil && input.nativeReplacement != nil {
		var selected []lock.LockedSubjectContract
		for _, contract := range locked.Locked.Subjects() {
			if contract.EntityID() == input.nativeReplacement.Declaration() {
				selected = append(selected, contract)
			}
		}
		locked.Locked, err = lock.NewLockedSection(selected, nil)
		if err != nil {
			return commandContext{}, result, err
		}
	}
	lockfileMissing := false
	if lockfileErr != nil {
		if !os.IsNotExist(lockfileErr) {
			return commandContext{}, result, commandInputError{cause: fmt.Errorf("%w: %w", ErrReadLockfile, lockfileErr)}
		}
		lockfileMissing = true
	} else if err := validateSelectedExtensionOrder(
		environment.Extensions(),
		locked,
		aggregatecodec.ExtensionOrderIdentityResolver(paths),
	); err != nil {
		return commandContext{}, result, err
	}

	// Each planning pass owns one persistence epoch. Execute performs a new pass
	// after acquiring mutation leases rather than reusing the disclosed plan.
	var persistenceEpoch readiness.PersistenceEpoch
	if input.persistenceBudget == nil {
		persistenceEpoch, err = nativeconfig.LoadPersistenceEpoch(ctx, paths)
	} else {
		persistenceEpoch, err = nativeconfig.LoadPersistenceEpochWithBudget(ctx, paths, input.persistenceBudget)
	}
	if err != nil {
		return commandContext{}, result, err
	}
	if !input.ownedRequalification {
		budget, err := persistenceEpoch.PhysicalBudget()
		if err != nil {
			return commandContext{}, result, err
		}
		if err := ownershipobserve.RequireMissingStateClear(ctx, paths, persistenceEpoch.MissingState(), budget); err != nil {
			return commandContext{}, result, err
		}
	}
	currentState, err := persistenceEpoch.Snapshot()
	if err != nil {
		return commandContext{}, result, err
	}
	physicalBudget, err := persistenceEpoch.PhysicalBudget()
	if err != nil {
		return commandContext{}, result, err
	}
	globalCarrierClaims, err := persistenceEpoch.CarrierClaims()
	if err != nil {
		return commandContext{}, result, err
	}

	availableTargets, err := readiness.FromManifestLockAndState(
		environment,
		locked,
		currentState,
		globalCarrierClaims,
	)
	if err != nil {
		return commandContext{}, result, fmt.Errorf("%w: %w", targetselection.ErrInvalid, err)
	}
	selection, err := targetselection.ForAvailableTargets(availableTargets, input.TargetValues)
	if err != nil {
		return commandContext{}, result, fmt.Errorf("%w: %w", targetselection.ErrInvalid, err)
	}

	if lockfileMissing {
		return commandContext{}, result, commandInputError{cause: fmt.Errorf("%w: %w", ErrReadLockfile, lockfileErr)}
	}

	generatedSkills, err := locked.Locked.SkillSetChildren(environment.Skills(), environment.SkillSets())
	if err != nil {
		return commandContext{}, result, fmt.Errorf("expand skill groups from lockfile: %w", err)
	}
	runtimeEnvironment, err := environment.WithGeneratedSkills(generatedSkills)
	if err != nil {
		return commandContext{}, result, fmt.Errorf("build runtime desired environment: %w", err)
	}

	settings, hookLocation, err := nativeconfig.SelectClaudeHookSettings(paths, currentState, environment.Hooks(), selection, physicalBudget)
	if err != nil {
		return commandContext{}, result, err
	}
	paths.ClaudeHookSettings = settings
	sourceEpoch, err := nativeconfig.ResolveSourceEpoch(
		ctx,
		paths,
		runtimeEnvironment,
		locked,
		selection,
	)
	if err != nil {
		return commandContext{}, result, fmt.Errorf("resolve lock observation sources: %w", err)
	}
	context := commandContext{
		Paths:                        paths,
		RuntimeEnvironment:           runtimeEnvironment,
		Lockfile:                     locked,
		Selection:                    selection,
		SourceEpoch:                  sourceEpoch,
		PersistenceEpoch:             persistenceEpoch,
		physicalBudget:               physicalBudget,
		RelationObservationsExplicit: input.RelationObservations != nil,
		ManageUnmanagedMatches:       input.ManageUnmanagedMatches,
		nativeReplacement:            input.nativeReplacement,
	}
	skillDiagnostics := diagnose.SkillRepairDiagnosticsFromSourceEpoch(
		ctx,
		paths,
		runtimeEnvironment.Skills(),
		selection,
		sourceEpoch,
	)
	if err := ctx.Err(); err != nil {
		return commandContext{}, result, err
	}
	piDiagnostics, err := diagnose.PiPackageSourceDiagnostics(environment.Extensions(), selection)
	if err != nil {
		return commandContext{}, result, err
	}
	result.Diagnostics = diagnose.HookCommandDiagnostics(environment.Hooks(), selection)
	result.Diagnostics = append(result.Diagnostics, hookLocation...)
	result.Diagnostics = append(result.Diagnostics, piDiagnostics...)
	result.Diagnostics = append(result.Diagnostics, skillDiagnostics...)
	result.LockOnly = readiness.SelectedUnsupportedProjections(runtimeEnvironment, selection)

	return context, result, nil
}

func selectedLockfilePath(paths daempaths.Paths, lockfilePath string) (string, error) {
	if lockfilePath != "" {
		absolute, err := filepath.Abs(lockfilePath)
		if err != nil {
			return "", fmt.Errorf("resolve lockfile path %q: %w", lockfilePath, err)
		}
		return filepath.Clean(absolute), nil
	}

	return paths.LockfilePath, nil
}

func cloneCommandInput(input CommandInput) CommandInput {
	cloned := input
	cloned.TargetValues = append([]string(nil), input.TargetValues...)
	return cloned
}

func refuseJournalAndFileSet(ctx context.Context, paths daempaths.Paths) error {
	return recoverygate.RequireClear(ctx, paths)
}
