package clipresent

import (
	"encoding/json"
	"io"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/recoverygate"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

type recoveryBarrierJSON struct {
	Journal string `json:"journal,omitempty"`
	FileSet string `json:"file_set,omitempty"`
}

func recoveryBarrierJSONFor(state recoverygate.State) *recoveryBarrierJSON {
	if !state.Observed() {
		return nil
	}
	result := recoveryBarrierJSON{}
	if state.JournalObserved() {
		result.Journal = "unknown"
		if state.JournalKnown() {
			switch state.Journal() {
			case journal.InterruptionClear:
				result.Journal = "clear"
			case journal.InterruptionActiveApply:
				result.Journal = "active_apply"
			case journal.InterruptionCleanupOnly:
				result.Journal = "cleanup_only"
			case journal.InterruptionInvalid:
				result.Journal = "invalid"
			}
		}
	}
	if state.FileSetObserved() {
		result.FileSet = "unknown"
		if state.FileSetKnown() {
			switch state.FileSet() {
			case fileset.FileSetFenceClear:
				result.FileSet = "clear"
			case fileset.FileSetFencePublishedTransaction:
				result.FileSet = "published_transaction"
			case fileset.FileSetFenceInvalidEvidence:
				result.FileSet = "invalid_evidence"
			case fileset.FileSetFenceAbandonedResidue:
				result.FileSet = "abandoned_residue"
			case fileset.FileSetFenceCensusLimit:
				result.FileSet = "census_limit"
			case fileset.FileSetFenceAccessUnprovable:
				result.FileSet = "access_unprovable"
			}
		}
	}
	return &result
}

type ApplyResultJSONInput struct {
	ActionCount            int
	StatefilePath          string
	LockOnly               LockOnlyResources
	Reconciliation         reconcile.Result
	ExecutionAttempted     bool
	DelegateAttempts       []DelegateAttemptInput
	HostRouteAttempts      []durableattempt.HostRouteAttempt
	CarrierAdoptionResults []durablecarrier.ManagedCarrierClaim
	RelationOrderResults   []applyworkflow.RelationOrderExecutionResult
	MCPStatuses            []MCPStatus
	Diagnostics            []findings.Diagnostic
	Failure                *applyworkflow.Failure
}

type applyResultJSONOutput struct {
	SchemaVersion        int                         `json:"schema_version"`
	Command              string                      `json:"command"`
	Mode                 string                      `json:"mode"`
	ActionCount          int                         `json:"action_count"`
	StatefilePath        string                      `json:"statefile_path"`
	LockOnly             LockOnlyResources           `json:"lock_only"`
	Actions              []planJSONAction            `json:"actions"`
	DelegateActions      []delegateActionJSON        `json:"delegate_actions,omitempty"`
	RelationActions      []relationActionJSON        `json:"relation_actions,omitempty"`
	RelationOrders       []relationOrderJSON         `json:"relation_order_actions,omitempty"`
	RelationOrderResults []relationOrderResultJSON   `json:"relation_order_results,omitempty"`
	CarrierAdoptions     []carrierAdoptionActionJSON `json:"carrier_adoption_actions,omitempty"`
	CarrierAbsences      []carrierAbsenceActionJSON  `json:"carrier_absence_actions,omitempty"`
	DelegateAttempts     []delegateAttemptJSON       `json:"delegate_attempts,omitempty"`
	HostRouteAttempts    []hostRouteAttemptJSON      `json:"host_route_attempts,omitempty"`
	MCPStatuses          []MCPStatus                 `json:"mcp_statuses,omitempty"`
	Diagnostics          []planJSONDiagnostic        `json:"diagnostics"`
	HasErrors            bool                        `json:"has_errors"`
	Errors               []applyResultJSONError      `json:"errors"`
}

type applyResultJSONError struct {
	Code            applyworkflow.FailureReason  `json:"code"`
	Phase           applyworkflow.FailurePhase   `json:"phase"`
	Outcome         applyworkflow.FailureOutcome `json:"outcome"`
	Message         string                       `json:"message"`
	RecoveryBarrier *recoveryBarrierJSON         `json:"recovery_barrier,omitempty"`
}

func PrintApplyResultJSON(output io.Writer, input ApplyResultJSONInput) error {
	hostRouteAttempts := hostRouteJSONAttempts(input.HostRouteAttempts)
	relations := input.Reconciliation.Relations()
	payload := applyResultJSONOutput{
		SchemaVersion:        contractversion.ApplyResultJSON,
		Command:              "apply",
		Mode:                 "write",
		ActionCount:          input.ActionCount,
		StatefilePath:        input.StatefilePath,
		LockOnly:             input.LockOnly,
		Actions:              planJSONActionsForPlan(input.Reconciliation),
		DelegateActions:      delegateJSONActions(input.Reconciliation.Delegates()),
		RelationActions:      relationJSONActions(relations),
		RelationOrders:       relationOrderJSONActions(input.Reconciliation.RelationOrders(), relations),
		RelationOrderResults: relationOrderResultJSONRows(input.RelationOrderResults),
		CarrierAdoptions: carrierAdoptionJSONActionsWithResults(
			input.Reconciliation.CarrierAdoptions(),
			applyResultCarrierAdoptionPhase(input.Failure != nil, input.ExecutionAttempted),
			input.CarrierAdoptionResults,
		),
		CarrierAbsences:   carrierAbsenceJSONActions(input.Reconciliation.CarrierAbsences()),
		DelegateAttempts:  delegateJSONAttempts(input.DelegateAttempts),
		HostRouteAttempts: hostRouteAttempts,
		MCPStatuses:       input.MCPStatuses,
		Diagnostics:       planJSONDiagnostics(input.Diagnostics),
		HasErrors: hasErrorDiagnostics(input.Diagnostics) ||
			input.Reconciliation.HasBlockedRelations() ||
			input.Reconciliation.HasBlockedRelationOrders() ||
			input.Reconciliation.HasBlockedCarrierAdoptions() ||
			input.Reconciliation.HasBlockedCarrierAbsences(),
		Errors: []applyResultJSONError{},
	}
	if input.Failure != nil {
		payload.HasErrors = true
		payload.Errors = append(payload.Errors, applyResultJSONError{
			Code:            input.Failure.Reason(),
			Phase:           input.Failure.Phase(),
			Outcome:         input.Failure.Outcome(),
			Message:         input.Failure.Detail(),
			RecoveryBarrier: recoveryBarrierJSONFor(input.Failure.RecoveryBarrier()),
		})
	}

	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func applyResultCarrierAdoptionPhase(failed bool, executionAttempted bool) carrierAdoptionPhase {
	if failed {
		if executionAttempted {
			return carrierAdoptionUnconfirmed
		}
		return carrierAdoptionNotRecorded
	}
	return carrierAdoptionCommitted
}
