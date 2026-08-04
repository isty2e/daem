package clipresent

import (
	"encoding/json"
	"io"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/reconcile"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

const applyResultJSONSchemaVersion = 16

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
	Err                    error
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
	Code    mutation.ReasonCode `json:"code,omitempty"`
	Message string              `json:"message"`
}

func PrintApplyResultJSON(output io.Writer, input ApplyResultJSONInput) error {
	hostRouteAttempts := hostRouteJSONAttempts(input.HostRouteAttempts)
	relations := input.Reconciliation.Relations()
	payload := applyResultJSONOutput{
		SchemaVersion:        applyResultJSONSchemaVersion,
		Command:              "apply",
		Mode:                 "write",
		ActionCount:          input.ActionCount,
		StatefilePath:        input.StatefilePath,
		LockOnly:             input.LockOnly,
		Actions:              planJSONActionsForPlan(input.Reconciliation),
		DelegateActions:      delegateJSONActions(input.Reconciliation.Delegates()),
		RelationActions:      relationJSONActions(relations),
		RelationOrders:       relationOrderJSONActions(input.Reconciliation.RelationOrders()),
		RelationOrderResults: relationOrderResultJSONRows(input.RelationOrderResults),
		CarrierAdoptions: carrierAdoptionJSONActionsWithResults(
			input.Reconciliation.CarrierAdoptions(),
			applyResultCarrierAdoptionPhase(input.Err, input.ExecutionAttempted),
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
	if input.Err != nil {
		payload.HasErrors = true
		reason, _ := mutation.ReasonCodeOf(input.Err)
		payload.Errors = append(payload.Errors, applyResultJSONError{Code: reason, Message: input.Err.Error()})
	}

	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func applyResultCarrierAdoptionPhase(err error, executionAttempted bool) carrierAdoptionPhase {
	if err != nil {
		if executionAttempted {
			return carrierAdoptionUnconfirmed
		}
		return carrierAdoptionNotRecorded
	}
	return carrierAdoptionCommitted
}
