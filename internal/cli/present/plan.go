package clipresent

import (
	"encoding/json"
	"io"

	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/topology"
)

func PrintDryRunPlanWithOptions(output io.Writer, result reconcile.Result, options HumanOptions) {
	PrintActionPlanWithOptions(output, "dry-run", result, options)
}

func PrintStatusPlanWithOptions(output io.Writer, result reconcile.Result, options HumanOptions) {
	PrintActionPlanWithOptions(output, "status", result, options)
}

type PlanJSONInput struct {
	Command           string
	Mode              string
	LockfilePath      string
	LockfileMissing   bool
	LockOnly          LockOnlyResources
	Reconciliation    reconcile.Result
	HostRouteAttempts []durableattempt.HostRouteAttempt
	Diagnostics       []findings.Diagnostic
	MCPStatuses       []MCPStatus
}

type planJSONOutput struct {
	SchemaVersion     int                         `json:"schema_version"`
	Command           string                      `json:"command"`
	Mode              string                      `json:"mode"`
	Lockfile          planJSONLockfile            `json:"lockfile"`
	LockOnly          LockOnlyResources           `json:"lock_only"`
	ActionCount       int                         `json:"action_count"`
	HasErrors         bool                        `json:"has_errors"`
	Actions           []planJSONAction            `json:"actions"`
	DelegateActions   []delegateActionJSON        `json:"delegate_actions,omitempty"`
	RelationActions   []relationActionJSON        `json:"relation_actions,omitempty"`
	RelationOrders    []relationOrderJSON         `json:"relation_order_actions,omitempty"`
	CarrierAdoptions  []carrierAdoptionActionJSON `json:"carrier_adoption_actions,omitempty"`
	CarrierAbsences   []carrierAbsenceActionJSON  `json:"carrier_absence_actions,omitempty"`
	HostRouteAttempts []hostRouteAttemptJSON      `json:"host_route_attempts,omitempty"`
	Diagnostics       []planJSONDiagnostic        `json:"diagnostics"`
	MCPStatuses       []MCPStatus                 `json:"mcp_statuses,omitempty"`
}

type planJSONLockfile struct {
	Path    string `json:"path"`
	Missing bool   `json:"missing"`
}

type planJSONAction struct {
	Kind             string                 `json:"kind"`
	Reason           string                 `json:"reason"`
	Subject          *planJSONSubject       `json:"subject,omitempty"`
	ResourceID       string                 `json:"resource_id,omitempty"`
	Resource         *planJSONResource      `json:"resource,omitempty"`
	Projection       *planJSONProjection    `json:"projection,omitempty"`
	Target           string                 `json:"target,omitempty"`
	Targets          []string               `json:"targets,omitempty"`
	Scope            string                 `json:"scope"`
	Destination      string                 `json:"destination"`
	ContentPath      string                 `json:"content_path,omitempty"`
	PlacementMode    string                 `json:"placement_mode"`
	ContentKind      string                 `json:"content_kind,omitempty"`
	PermissionPolicy string                 `json:"permission_policy,omitempty"`
	DesiredFileMode  *uint32                `json:"desired_file_mode,omitempty"`
	LiveFileMode     *uint32                `json:"live_file_mode,omitempty"`
	DesiredHash      string                 `json:"desired_hash"`
	LiveHash         string                 `json:"live_hash"`
	StateHash        string                 `json:"state_hash"`
	Detail           string                 `json:"detail"`
	Safety           string                 `json:"safety"`
	PreviousState    *planJSONPreviousState `json:"previous_state,omitempty"`
}

type planJSONResource struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type planJSONSubject struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type planJSONProjection struct {
	Target      string `json:"target"`
	Scope       string `json:"scope"`
	ConfigPath  string `json:"config_path"`
	ContentPath string `json:"content_path"`
}

type planJSONPreviousState struct {
	Subject          *planJSONSubject  `json:"subject,omitempty"`
	ResourceID       string            `json:"resource_id,omitempty"`
	Resource         *planJSONResource `json:"resource,omitempty"`
	Target           string            `json:"target,omitempty"`
	Targets          []string          `json:"targets,omitempty"`
	Scope            string            `json:"scope"`
	Destination      string            `json:"destination"`
	ContentPath      string            `json:"content_path,omitempty"`
	ContentHash      string            `json:"content_hash"`
	ContentKind      string            `json:"content_kind,omitempty"`
	PermissionPolicy string            `json:"permission_policy,omitempty"`
	FileMode         *uint32           `json:"file_mode,omitempty"`
}

type planJSONDiagnostic struct {
	Severity      string           `json:"severity"`
	Code          string           `json:"code"`
	ResourceID    string           `json:"resource_id"`
	Resource      planJSONResource `json:"resource"`
	Target        string           `json:"target"`
	Scope         string           `json:"scope"`
	Event         string           `json:"event"`
	Command       string           `json:"command"`
	Detail        string           `json:"detail"`
	Repairability string           `json:"repairability,omitempty"`
	RepairActions []string         `json:"repair_actions,omitempty"`
	ManualReasons []string         `json:"manual_reasons,omitempty"`
	NextStep      string           `json:"next_step,omitempty"`
}

func PrintPlanJSON(output io.Writer, input PlanJSONInput) error {
	hostRouteAttempts := hostRouteJSONAttempts(input.HostRouteAttempts)
	reconciliation := input.Reconciliation
	payload := planJSONOutput{
		SchemaVersion: contractversion.ReconciliationPlanJSON,
		Command:       input.Command,
		Mode:          input.Mode,
		Lockfile: planJSONLockfile{
			Path:    input.LockfilePath,
			Missing: input.LockfileMissing,
		},
		LockOnly:    input.LockOnly,
		ActionCount: reconciliation.ProjectionDecisionCount(),
		HasErrors: input.LockfileMissing ||
			reconciliation.HasErrors() ||
			hasErrorDiagnostics(input.Diagnostics),
		Actions:           planJSONActionsForPlan(reconciliation),
		DelegateActions:   delegateJSONActions(reconciliation.Delegates()),
		RelationActions:   relationJSONActions(reconciliation.Relations()),
		RelationOrders:    relationOrderJSONActions(reconciliation.RelationOrders()),
		CarrierAdoptions:  carrierAdoptionJSONActions(reconciliation.CarrierAdoptions(), carrierAdoptionPlanned),
		CarrierAbsences:   carrierAbsenceJSONActions(reconciliation.CarrierAbsences()),
		HostRouteAttempts: hostRouteAttempts,
		Diagnostics:       planJSONDiagnostics(input.Diagnostics),
		MCPStatuses:       input.MCPStatuses,
	}

	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func planJSONActionsForPlan(planResult reconcile.Result) []planJSONAction {
	result := make([]planJSONAction, 0, planResult.ProjectionDecisionCount())
	for _, decision := range planResult.Decisions() {
		if managedPath, ok := decision.ManagedPath(); ok {
			result = append(result, planJSONManagedPathAction(managedPath))
			continue
		}
		aggregate, _ := decision.Aggregate()
		result = append(result, planJSONAggregateAction(aggregate))
	}
	return result
}

func hasErrorDiagnostics(diagnostics []findings.Diagnostic) bool {
	return findings.HasDiagnosticErrors(diagnostics)
}

func planJSONDiagnostics(diagnostics []findings.Diagnostic) []planJSONDiagnostic {
	result := make([]planJSONDiagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		result = append(result, planJSONDiagnostic{
			Severity:   string(diagnostic.Severity),
			Code:       diagnostic.Code,
			ResourceID: resourceIDString(diagnostic.EntityID),
			Resource: planJSONResource{
				Kind: string(diagnostic.EntityID.Kind()),
				Name: diagnostic.EntityID.Name(),
			},
			Target:        string(diagnostic.Target),
			Scope:         string(diagnostic.Scope),
			Event:         diagnostic.Event,
			Command:       diagnostic.Command,
			Detail:        diagnostic.Detail,
			Repairability: diagnostic.Repairability,
			RepairActions: append([]string(nil), diagnostic.RepairActions...),
			ManualReasons: append([]string(nil), diagnostic.ManualReasons...),
			NextStep:      diagnostic.NextStep,
		})
	}

	return result
}

func resourceIDString(id entity.ID) string {
	return string(id.Kind()) + "/" + id.Name()
}

func planJSONSubjectFor(subject topology.SubjectID) *planJSONSubject {
	if subject.IsZero() {
		return nil
	}
	return &planJSONSubject{
		Kind:      string(subject.Kind()),
		Namespace: subject.Namespace(),
		Name:      subject.Key(),
	}
}

func subjectString(subject planJSONSubject) string {
	return subject.Kind + "/" + subject.Namespace + "/" + subject.Name
}

func managedPathSafetyState(decision reconcile.ManagedPathDecision) (string, bool) {
	if decision.Kind() == reconcile.ManagedPathRemove && decision.Reason() == reconcile.ReasonRemovedFromManifest {
		return "deletable", true
	}
	if decision.Kind() != reconcile.ManagedPathBlocked {
		return "", false
	}
	if _, hasPrevious := decision.PreviousState(); !hasPrevious {
		return "", false
	}
	switch decision.Reason() {
	case reconcile.ReasonDriftedOutput:
		return "drift_blocked", true
	case reconcile.ReasonMissingLiveObservation, reconcile.ReasonMissingOutput:
		return "missing_evidence", true
	default:
		return "", false
	}
}
