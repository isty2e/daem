package clipresent

import (
	"fmt"
	"io"
	"strings"

	reconciliation "github.com/isty2e/daem/internal/reconcile"
)

type relationActionJSON struct {
	Kind                 string           `json:"kind"`
	Subject              *planJSONSubject `json:"subject,omitempty"`
	Target               string           `json:"target"`
	Scope                string           `json:"scope"`
	SourceNamespace      string           `json:"source_namespace"`
	SourceKind           string           `json:"source_kind"`
	SourceRef            string           `json:"source_ref"`
	RelationSubjectKey   string           `json:"relation_subject_key"`
	EvidenceSource       string           `json:"evidence_source"`
	EvidenceAvailability string           `json:"evidence_availability"`
	EvidenceFreshness    string           `json:"evidence_freshness"`
	RouteID              string           `json:"route_id"`
	RouteRequestHash     string           `json:"route_request_hash"`
	RouteAdmissionRow    string           `json:"route_admission_row"`
	RequestedOutcome     string           `json:"requested_outcome"`
	SelectedOutcome      string           `json:"selected_outcome"`
	CorrelationState     string           `json:"correlation_state"`
	CorrelationReason    string           `json:"correlation_reason,omitempty"`
	Reason               string           `json:"reason,omitempty"`
	Execution            string           `json:"execution"`
	Watchpoints          []string         `json:"watchpoints,omitempty"`
	ReplayBoundary       string           `json:"replay_boundary"`
	RetainedEffects      []string         `json:"retained_effects"`
	NonClaims            []string         `json:"non_claims"`
	InvokesHostRoute     bool             `json:"invokes_host_route"`
	AllowsHostRoute      bool             `json:"allows_host_route_invocation"`
	BlocksOrdinaryApply  bool             `json:"blocks_ordinary_apply"`
}

var relationActionRetainedEffects = []string{
	"host_selected_artifacts",
	"provider_contributions",
	"package_cache",
	"credentials",
	"trust_session_state",
	"runtime_state",
	"logs",
}

var relationActionNonClaims = []string{
	"host_route_invocation",
	"exact_artifact_replay",
	"current_contribution_inventory",
	"runtime_readiness",
	"tool_inventory",
	"auth_trust_state",
	"package_cache_convergence",
	"carrier_removal",
	"destructive_cleanup",
	"future_skip_authority",
}

func relationJSONActions(actions []reconciliation.RelationAction) []relationActionJSON {
	result := make([]relationActionJSON, 0, len(actions))
	for _, action := range actions {
		route := action.RouteRequest()
		admission := action.RouteAdmission()
		sourceKind, sourceRef := splitRelationSourceNamespace(action.SourceNamespace())
		result = append(result, relationActionJSON{
			Kind:                 string(action.Kind()),
			Subject:              planJSONSubjectFor(action.Subject()),
			Target:               string(action.Target()),
			Scope:                string(action.Scope()),
			SourceNamespace:      action.SourceNamespace(),
			SourceKind:           sourceKind,
			SourceRef:            sourceRef,
			RelationSubjectKey:   action.RelationSubjectKey(),
			EvidenceSource:       action.EvidenceSource(),
			EvidenceAvailability: string(action.EvidenceAvailability()),
			EvidenceFreshness:    string(action.EvidenceFreshness()),
			RouteID:              route.RouteID(),
			RouteRequestHash:     route.CanonicalRequestHash(),
			RouteAdmissionRow:    string(admission.Row()),
			RequestedOutcome:     string(admission.RequestedOutcome()),
			SelectedOutcome:      string(admission.SelectedOutcome()),
			CorrelationState:     string(action.CorrelationState()),
			CorrelationReason:    string(action.CorrelationReason()),
			Reason:               string(action.Reason()),
			Execution:            string(action.Execution()),
			Watchpoints:          relationWatchpoints(action),
			ReplayBoundary:       action.ReplayBoundary(),
			RetainedEffects:      relationRetainedEffects(),
			NonClaims:            relationNonClaimsForAction(action),
			InvokesHostRoute:     action.InvokesHostRoute(),
			AllowsHostRoute:      admission.AllowsHostRouteInvocation(),
			BlocksOrdinaryApply:  action.BlocksOrdinaryApply(),
		})
	}
	return result
}

func relationRetainedEffects() []string {
	return append([]string(nil), relationActionRetainedEffects...)
}

func relationNonClaims() []string {
	return append([]string(nil), relationActionNonClaims...)
}

func relationNonClaimsForAction(action reconciliation.RelationAction) []string {
	claims := relationNonClaims()
	if !action.InvokesHostRoute() {
		return claims
	}
	result := claims[:0]
	for _, claim := range claims {
		if claim != "host_route_invocation" {
			result = append(result, claim)
		}
	}
	return result
}

func relationWatchpoints(action reconciliation.RelationAction) []string {
	watchpoints := action.Watchpoints()
	result := make([]string, 0, len(watchpoints))
	for _, watchpoint := range watchpoints {
		result = append(result, string(watchpoint))
	}
	return result
}

func splitRelationSourceNamespace(namespace string) (string, string) {
	kind, ref, ok := strings.Cut(namespace, ":")
	if !ok {
		return namespace, ""
	}
	return kind, ref
}

// PrintRelationActions writes relation action rows without implying host route
// execution unless the underlying action explicitly says so.
func PrintRelationActionsWithOptions(output io.Writer, actions []reconciliation.RelationAction, options HumanOptions) {
	if len(actions) == 0 {
		return
	}
	fmt.Fprintf(output, "relation actions: %d subjects\n", len(actions))
	for _, action := range actions {
		if !options.Verbose {
			printRelationActionSummary(output, action)
			continue
		}
		route := action.RouteRequest()
		admission := action.RouteAdmission()
		subject := ""
		if renderedSubject := planJSONSubjectFor(action.Subject()); renderedSubject != nil {
			subject = subjectString(*renderedSubject)
		}
		sourceKind, sourceRef := splitRelationSourceNamespace(action.SourceNamespace())
		fmt.Fprintf(
			output,
			"  - kind=%s subject=%q target=%s scope=%s source_kind=%q source_ref=%q source_namespace=%q relation_subject_key=%q evidence_source=%s evidence_availability=%s evidence_freshness=%s execution=%s reason=%s correlation_state=%s correlation_reason=%s route_id=%q route_request_hash=%q route_admission_row=%q requested_outcome=%s selected_outcome=%s replay_boundary=%s retained_effects=%q non_claims=%q invokes_host_route=%t allows_host_route_invocation=%t blocks_ordinary_apply=%t\n",
			action.Kind(),
			subject,
			action.Target(),
			action.Scope(),
			sourceKind,
			sourceRef,
			action.SourceNamespace(),
			action.RelationSubjectKey(),
			action.EvidenceSource(),
			action.EvidenceAvailability(),
			action.EvidenceFreshness(),
			action.Execution(),
			action.Reason(),
			action.CorrelationState(),
			action.CorrelationReason(),
			route.RouteID(),
			route.CanonicalRequestHash(),
			admission.Row(),
			admission.RequestedOutcome(),
			admission.SelectedOutcome(),
			action.ReplayBoundary(),
			relationRetainedEffects(),
			relationNonClaimsForAction(action),
			action.InvokesHostRoute(),
			admission.AllowsHostRouteInvocation(),
			action.BlocksOrdinaryApply(),
		)
	}
}

func printRelationActionSummary(output io.Writer, action reconciliation.RelationAction) {
	subject := ""
	if renderedSubject := planJSONSubjectFor(action.Subject()); renderedSubject != nil {
		subject = subjectString(*renderedSubject)
	}
	if action.InvokesHostRoute() {
		fmt.Fprintf(output, "  - install extension through host subject=%q target=%s scope=%s\n", subject, action.Target(), action.Scope())
		fmt.Fprintln(output, "    host may retain packages, caches, credentials, trust data, or logs")
		return
	}
	if action.Reason() == reconciliation.ReasonPresentUnclaimed {
		fmt.Fprintf(
			output,
			"  - external carrier present but unclaimed subject=%q target=%s scope=%s\n",
			subject,
			action.Target(),
			action.Scope(),
		)
		return
	}
	if action.BlocksOrdinaryApply() {
		fmt.Fprintf(output, "  - blocked subject=%q target=%s scope=%s: %s\n", subject, action.Target(), action.Scope(), strings.ReplaceAll(string(action.Reason()), "_", " "))
		return
	}
	fmt.Fprintf(output, "  - remove managed extension binding subject=%q target=%s scope=%s\n", subject, action.Target(), action.Scope())
}
