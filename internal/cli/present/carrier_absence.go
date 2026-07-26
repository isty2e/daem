package clipresent

import (
	"fmt"
	"io"
	"strings"

	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
)

type carrierAbsenceActionJSON struct {
	Kind                        string           `json:"kind"`
	Subject                     *planJSONSubject `json:"subject"`
	CarrierSubject              *planJSONSubject `json:"carrier_subject"`
	Target                      string           `json:"target"`
	Scope                       string           `json:"scope"`
	SourceNamespace             string           `json:"source_namespace"`
	RequestedOutcome            string           `json:"requested_outcome"`
	SelectedAction              string           `json:"selected_action"`
	Execution                   string           `json:"execution"`
	CorrelationState            string           `json:"correlation_state,omitempty"`
	CorrelationReason           string           `json:"correlation_reason,omitempty"`
	EvidenceAvailability        string           `json:"evidence_availability,omitempty"`
	EvidenceFreshness           string           `json:"evidence_freshness,omitempty"`
	DaemKnownConsumerCount      int              `json:"daem_known_consumer_count"`
	RemainingDaemKnownConsumers int              `json:"remaining_daem_known_consumers"`
	RouteID                     string           `json:"route_id,omitempty"`
	RouteRequestHash            string           `json:"route_request_hash,omitempty"`
	PostconditionVerification   string           `json:"postcondition_verification,omitempty"`
	RecoveryContract            string           `json:"recovery_contract,omitempty"`
	RemovedEffects              []string         `json:"removed_effects,omitempty"`
	RetainedEffects             []string         `json:"retained_effects,omitempty"`
	NonClaims                   []string         `json:"non_claims"`
	InvokesHostRoute            bool             `json:"invokes_host_route"`
	RetiresClaim                bool             `json:"retires_claim"`
	StateOnly                   bool             `json:"state_only"`
	BlocksOrdinaryApply         bool             `json:"blocks_ordinary_apply"`
}

func carrierAbsenceJSONActions(
	actions []carrierabsence.Action,
) []carrierAbsenceActionJSON {
	result := make([]carrierAbsenceActionJSON, 0, len(actions))
	for _, action := range actions {
		observation, observed := action.Observation()
		route := action.RouteAdmission()
		operation := route.Operation()
		request := route.Request()
		if pending, present := action.PendingRemoval(); present &&
			request.RouteID() == "" {
			request = pending.RemoveRequest()
		}
		row := carrierAbsenceActionJSON{
			Kind:                        "carrier_absence",
			Subject:                     planJSONSubjectFor(action.Subject()),
			CarrierSubject:              planJSONSubjectFor(action.Claim().Identity().CarrierSubject()),
			Target:                      string(action.Target()),
			Scope:                       string(action.Scope()),
			SourceNamespace:             action.Claim().Identity().SourceNamespace(),
			RequestedOutcome:            string(action.Desired()),
			SelectedAction:              string(action.Decision()),
			Execution:                   carrierAbsenceExecution(action),
			DaemKnownConsumerCount:      action.Occupancy().DaemKnownConsumerCount(),
			RemainingDaemKnownConsumers: len(action.RemainingDaemKnownConsumers()),
			RouteID:                     request.RouteID(),
			RouteRequestHash:            request.CanonicalRequestHash(),
			PostconditionVerification:   carrierAbsenceVerification(action, operation),
			RecoveryContract:            carrierAbsenceRecovery(action, operation),
			RemovedEffects:              route.RemovedEffects(),
			RetainedEffects:             route.RetainedEffects(),
			NonClaims:                   action.NonClaims(),
			InvokesHostRoute:            action.InvokesHostRoute(),
			RetiresClaim:                action.RetiresClaim(),
			StateOnly:                   action.StateOnly(),
			BlocksOrdinaryApply:         action.BlocksOrdinaryApply(),
		}
		if observed {
			row.CorrelationState = string(observation.Result.State())
			row.CorrelationReason = string(observation.Result.Reason())
			row.EvidenceAvailability = string(observation.Result.EvidenceAvailability())
			row.EvidenceFreshness = string(observation.Result.EvidenceFreshness())
		}
		result = append(result, row)
	}
	return result
}

// PrintCarrierAbsenceActionsWithOptions writes carrier-removal planning without
// implying that route admission or invocation proves the postcondition.
func PrintCarrierAbsenceActionsWithOptions(
	output io.Writer,
	actions []carrierabsence.Action,
	options HumanOptions,
) {
	if len(actions) == 0 {
		return
	}
	fmt.Fprintf(output, "carrier absence actions: %d subjects\n", len(actions))
	for _, action := range actions {
		if !options.Verbose {
			printCarrierAbsenceSummary(output, action)
			continue
		}
		row := carrierAbsenceJSONActions([]carrierabsence.Action{action})[0]
		fmt.Fprintf(
			output,
			"  - kind=%s subject=%q carrier=%q target=%s scope=%s source_namespace=%q requested_outcome=%s selected_action=%s execution=%s correlation_state=%s correlation_reason=%s evidence_availability=%s evidence_freshness=%s daem_known_consumers=%d remaining_daem_known_consumers=%d route_id=%q route_request_hash=%q postcondition_verification=%q recovery_contract=%q removed_effects=%q retained_effects=%q non_claims=%q invokes_host_route=%t retires_claim=%t state_only=%t blocks_ordinary_apply=%t\n",
			row.Kind,
			subjectString(*row.Subject),
			subjectString(*row.CarrierSubject),
			row.Target,
			row.Scope,
			row.SourceNamespace,
			row.RequestedOutcome,
			row.SelectedAction,
			row.Execution,
			row.CorrelationState,
			row.CorrelationReason,
			row.EvidenceAvailability,
			row.EvidenceFreshness,
			row.DaemKnownConsumerCount,
			row.RemainingDaemKnownConsumers,
			row.RouteID,
			row.RouteRequestHash,
			row.PostconditionVerification,
			row.RecoveryContract,
			row.RemovedEffects,
			row.RetainedEffects,
			row.NonClaims,
			row.InvokesHostRoute,
			row.RetiresClaim,
			row.StateOnly,
			row.BlocksOrdinaryApply,
		)
	}
}

func printCarrierAbsenceSummary(output io.Writer, action carrierabsence.Action) {
	subject := subjectString(*planJSONSubjectFor(action.Subject()))
	switch {
	case action.BlocksOrdinaryApply():
		fmt.Fprintf(
			output,
			"  - blocked carrier removal subject=%q target=%s scope=%s: %s\n",
			subject,
			action.Target(),
			action.Scope(),
			strings.ReplaceAll(string(action.Decision()), "_", " "),
		)
	case action.StateOnly():
		fmt.Fprintf(
			output,
			"  - retire already-absent carrier claim subject=%q target=%s scope=%s\n",
			subject,
			action.Target(),
			action.Scope(),
		)
	case action.VerifiesPendingRemoval():
		fmt.Fprintf(
			output,
			"  - verify pending carrier removal subject=%q target=%s scope=%s\n",
			subject,
			action.Target(),
			action.Scope(),
		)
	case action.InvokesHostRoute():
		fmt.Fprintf(
			output,
			"  - remove managed carrier relation through host subject=%q target=%s scope=%s\n",
			subject,
			action.Target(),
			action.Scope(),
		)
	case action.MutatesDirectProjection():
		fmt.Fprintf(
			output,
			"  - remove managed carrier relation from host config subject=%q target=%s scope=%s\n",
			subject,
			action.Target(),
			action.Scope(),
		)
	default:
		fmt.Fprintf(
			output,
			"  - retain managed carrier relation subject=%q target=%s scope=%s\n",
			subject,
			action.Target(),
			action.Scope(),
		)
	}
}

func carrierAbsenceExecution(action carrierabsence.Action) string {
	switch {
	case action.StateOnly():
		return "state_only"
	case action.VerifiesPendingRemoval():
		return "observation_only"
	case action.InvokesHostRoute():
		return "host_route"
	case action.MutatesDirectProjection():
		return "direct_config"
	default:
		return "none"
	}
}

func carrierAbsenceVerification(
	action carrierabsence.Action,
	operation lock.OperationContract,
) string {
	if action.StateOnly() {
		return "fresh_exact_absence"
	}
	if action.VerifiesPendingRemoval() {
		return "fresh_pending_removal_postconditions"
	}
	return string(operation.Verification())
}

func carrierAbsenceRecovery(
	action carrierabsence.Action,
	operation lock.OperationContract,
) string {
	if action.StateOnly() {
		return "state_only_claim_retirement"
	}
	if action.VerifiesPendingRemoval() {
		return "observation_only_pending_settlement"
	}
	return string(operation.Recovery())
}
