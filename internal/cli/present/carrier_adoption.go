package clipresent

import (
	"fmt"
	"io"
	"strings"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
)

type carrierAdoptionPhase string

const (
	carrierAdoptionPlanned          carrierAdoptionPhase = "planned"
	carrierAdoptionCommitted        carrierAdoptionPhase = "committed"
	carrierAdoptionInstallRecovered carrierAdoptionPhase = "install_recovered"
	carrierAdoptionNotRecorded      carrierAdoptionPhase = "not_recorded"
	carrierAdoptionUnconfirmed      carrierAdoptionPhase = "unconfirmed"
)

type carrierAdoptionActionJSON struct {
	Kind                       string           `json:"kind"`
	Subject                    *planJSONSubject `json:"subject"`
	CarrierSubject             *planJSONSubject `json:"carrier_subject"`
	Target                     string           `json:"target"`
	Scope                      string           `json:"scope"`
	SourceNamespace            string           `json:"source_namespace"`
	SourceNamespaceRedacted    bool             `json:"source_namespace_redacted,omitempty"`
	RelationSubjectKey         string           `json:"relation_subject_key"`
	RelationSubjectKeyRedacted bool             `json:"relation_subject_key_redacted,omitempty"`
	Result                     string           `json:"result"`
	CorrelationState           string           `json:"correlation_state"`
	CorrelationReason          string           `json:"correlation_reason,omitempty"`
	EvidenceAvailability       string           `json:"evidence_availability"`
	EvidenceFreshness          string           `json:"evidence_freshness"`
	ClaimOwner                 string           `json:"claim_owner,omitempty"`
	ClaimStore                 string           `json:"claim_store"`
	CurrentClaimProvenance     string           `json:"current_claim_provenance,omitempty"`
	ProposedClaimProvenance    string           `json:"proposed_claim_provenance,omitempty"`
	FinalClaimProvenance       string           `json:"final_claim_provenance,omitempty"`
	ClaimTransition            string           `json:"claim_transition"`
	LifecycleEligible          bool             `json:"lifecycle_eligible"`
	LifecycleBlocker           string           `json:"lifecycle_blocker,omitempty"`
	DaemKnownConsumerCount     int              `json:"daem_known_consumer_count"`
	ConflictingClaimCount      int              `json:"conflicting_claim_count"`
	InstallRouteStatus         string           `json:"install_route_status"`
	InstallRouteID             string           `json:"install_route_id"`
	InstallRouteRequestHash    string           `json:"install_route_request_hash"`
	RemovalRouteStatus         string           `json:"removal_route_status"`
	RemovalRouteID             string           `json:"removal_route_id,omitempty"`
	RemovalRouteRequestHash    string           `json:"removal_route_request_hash,omitempty"`
	RemovalActuation           string           `json:"removal_actuation,omitempty"`
	LaterOmission              string           `json:"later_omission,omitempty"`
	PreservesSharedCarrier     bool             `json:"preserves_shared_carrier"`
	RemovedEffects             []string         `json:"removed_effects,omitempty"`
	RetainedEffects            []string         `json:"retained_effects,omitempty"`
	NonClaims                  []string         `json:"non_claims"`
	AmbientConsumerAssurance   string           `json:"ambient_consumer_assurance"`
	ManageExisting             bool             `json:"manage_existing"`
	InvokesHostRoute           bool             `json:"invokes_host_route"`
	StateOnly                  bool             `json:"state_only"`
	BlocksOrdinaryApply        bool             `json:"blocks_ordinary_apply"`
}

func carrierAdoptionJSONActions(
	actions []carrieradoption.Action,
	phase carrierAdoptionPhase,
) []carrierAdoptionActionJSON {
	return carrierAdoptionJSONActionsWithResults(actions, phase, nil)
}

func carrierAdoptionJSONActionsWithResults(
	actions []carrieradoption.Action,
	phase carrierAdoptionPhase,
	results []durablecarrier.ManagedCarrierClaim,
) []carrierAdoptionActionJSON {
	result := make([]carrierAdoptionActionJSON, 0, len(actions))
	for _, action := range actions {
		actionPhase, finalClaim, hasFinalClaim := carrierAdoptionResult(action, phase, results)
		identity := action.CarrierIdentity()
		disclosure := carrierIdentityDisclosureFor(identity)
		observation := action.Observation()
		lifecycle := action.Lifecycle()
		installRequest := action.AcquisitionRequest()
		removal := lifecycle.RemovalRoute()
		removalRequest := removal.Request()
		removalOperation := removal.Operation()
		currentProvenance := ""
		proposedProvenance := ""
		finalProvenance := ""
		claimOwner := ""
		if current, present := action.CurrentClaim(); present {
			currentProvenance = string(current.Provenance())
			claimOwner = "selected_manifest"
		}
		if proposed, present := action.ProposedClaim(); present {
			proposedProvenance = string(proposed.Provenance())
			claimOwner = "selected_manifest"
		}
		if hasFinalClaim {
			finalProvenance = string(finalClaim.Provenance())
		}
		laterOmission := ""
		if removal.Status() == carrierabsence.RouteAdmitted {
			laterOmission = "requests_managed_relation_absence"
		}
		result = append(result, carrierAdoptionActionJSON{
			Kind:                       "carrier_adoption",
			Subject:                    planJSONSubjectFor(action.Subject()),
			CarrierSubject:             disclosure.carrierSubject,
			Target:                     string(action.Target()),
			Scope:                      string(action.Scope()),
			SourceNamespace:            disclosure.sourceNamespace.Value(),
			SourceNamespaceRedacted:    disclosure.sourceNamespace.Redacted(),
			RelationSubjectKey:         disclosure.relationSubjectKey.Value(),
			RelationSubjectKeyRedacted: disclosure.relationSubjectKey.Redacted(),
			Result:                     string(action.Result()),
			CorrelationState:           string(observation.State()),
			CorrelationReason:          string(observation.Reason()),
			EvidenceAvailability:       string(observation.EvidenceAvailability()),
			EvidenceFreshness:          string(observation.EvidenceFreshness()),
			ClaimOwner:                 claimOwner,
			ClaimStore:                 string(lifecycle.ClaimStore()),
			CurrentClaimProvenance:     currentProvenance,
			ProposedClaimProvenance:    proposedProvenance,
			FinalClaimProvenance:       finalProvenance,
			ClaimTransition:            carrierAdoptionClaimTransition(action, actionPhase),
			LifecycleEligible:          lifecycle.Eligible(),
			LifecycleBlocker:           string(lifecycle.Blocker()),
			DaemKnownConsumerCount:     action.Occupancy().DaemKnownConsumerCount(),
			ConflictingClaimCount:      len(action.ConflictingClaims()),
			InstallRouteStatus:         string(lifecycle.InstallRouteStatus()),
			InstallRouteID:             installRequest.RouteID(),
			InstallRouteRequestHash:    installRequest.CanonicalRequestHash(),
			RemovalRouteStatus:         string(removal.Status()),
			RemovalRouteID:             removalRequest.RouteID(),
			RemovalRouteRequestHash:    removalRequest.CanonicalRequestHash(),
			RemovalActuation:           string(removalOperation.Actuation()),
			LaterOmission:              laterOmission,
			PreservesSharedCarrier:     removal.PreservesSharedCarrier(),
			RemovedEffects:             removal.RemovedEffects(),
			RetainedEffects:            removal.RetainedEffects(),
			NonClaims:                  removal.NonClaims(),
			AmbientConsumerAssurance:   "not_proven",
			ManageExisting:             action.ManageExisting(),
			InvokesHostRoute:           action.InvokesHostRoute(),
			StateOnly:                  action.StateOnly(),
			BlocksOrdinaryApply:        action.BlocksOrdinaryApply(),
		})
	}
	return result
}

// PrintCarrierAdoptionActionsWithOptions writes current adoption facts and
// planned state-only transitions without implying that a claim was committed.
func PrintCarrierAdoptionActionsWithOptions(
	output io.Writer,
	actions []carrieradoption.Action,
	options HumanOptions,
) {
	if len(actions) == 0 {
		return
	}
	fmt.Fprintf(output, "carrier adoption actions: %d subjects\n", len(actions))
	for _, action := range actions {
		if !options.Verbose {
			printCarrierAdoptionSummary(output, action)
			continue
		}
		printCarrierAdoptionVerbose(output, action, carrierAdoptionPlanned, nil)
	}
}

// PrintCarrierAdoptionResultsWithOptions writes only completed or unconfirmed
// claim transitions. Passive and no-op adoption facts are not repeated.
func PrintCarrierAdoptionResultsWithOptions(
	output io.Writer,
	actions []carrieradoption.Action,
	results []durablecarrier.ManagedCarrierClaim,
	executionErr error,
	executionAttempted bool,
	options HumanOptions,
) {
	stateOnly := make([]carrieradoption.Action, 0, len(actions))
	for _, action := range actions {
		if action.StateOnly() {
			stateOnly = append(stateOnly, action)
		}
	}
	if len(stateOnly) == 0 {
		return
	}
	fmt.Fprintf(output, "carrier adoption results: %d subjects\n", len(stateOnly))
	phase := applyResultCarrierAdoptionPhase(executionErr != nil, executionAttempted)
	for _, action := range stateOnly {
		actionPhase, _, _ := carrierAdoptionResult(action, phase, results)
		if options.Verbose {
			printCarrierAdoptionVerbose(output, action, phase, results)
			continue
		}
		row := carrierAdoptionJSONActionsWithResults(
			[]carrieradoption.Action{action},
			phase,
			results,
		)[0]
		switch actionPhase {
		case carrierAdoptionCommitted:
			fmt.Fprintf(
				output,
				"  - recorded external carrier claim subject=%q target=%s scope=%s source=%q provenance=%s\n",
				subjectString(*row.Subject),
				row.Target,
				row.Scope,
				row.SourceNamespace,
				row.FinalClaimProvenance,
			)
			continue
		case carrierAdoptionInstallRecovered:
			fmt.Fprintf(
				output,
				"  - completed pending carrier install recovery subject=%q target=%s scope=%s source=%q provenance=%s\n",
				subjectString(*row.Subject),
				row.Target,
				row.Scope,
				row.SourceNamespace,
				row.FinalClaimProvenance,
			)
			continue
		case carrierAdoptionNotRecorded:
			fmt.Fprintf(
				output,
				"  - external carrier claim not recorded before apply effects subject=%q target=%s scope=%s source=%q\n",
				subjectString(*row.Subject),
				row.Target,
				row.Scope,
				row.SourceNamespace,
			)
			continue
		}
		fmt.Fprintf(
			output,
			"  - external carrier claim outcome unconfirmed after apply error subject=%q target=%s scope=%s source=%q\n",
			subjectString(*row.Subject),
			row.Target,
			row.Scope,
			row.SourceNamespace,
		)
		fmt.Fprintln(output, "    next: daem status")
	}
}

func printCarrierAdoptionSummary(output io.Writer, action carrieradoption.Action) {
	row := carrierAdoptionJSONActions(
		[]carrieradoption.Action{action},
		carrierAdoptionPlanned,
	)[0]
	subject := subjectString(*row.Subject)
	switch action.Result() {
	case carrieradoption.ResultEligibleExactRelation:
		fmt.Fprintf(
			output,
			"  - would record external carrier claim subject=%q target=%s scope=%s source=%q\n",
			subject,
			row.Target,
			row.Scope,
			row.SourceNamespace,
		)
		fmt.Fprintf(
			output,
			"    owner=selected manifest provenance=%s; adoption invokes no host command\n",
			row.ProposedClaimProvenance,
		)
		fmt.Fprintf(
			output,
			"    later omission requests managed-relation removal; removed_effects=%q retained_effects=%q non_claims=%q; ambient consumers are not proven\n",
			row.RemovedEffects,
			row.RetainedEffects,
			row.NonClaims,
		)
	case carrieradoption.ResultAlreadyClaimedCurrent:
		fmt.Fprintf(
			output,
			"  - external carrier already claimed subject=%q target=%s scope=%s source=%q provenance=%s\n",
			subject,
			row.Target,
			row.Scope,
			row.SourceNamespace,
			row.CurrentClaimProvenance,
		)
	case carrieradoption.ResultPresentUnclaimed:
		fmt.Fprintf(
			output,
			"  - carrier adoption available subject=%q target=%s scope=%s source=%q\n",
			subject,
			row.Target,
			row.Scope,
			row.SourceNamespace,
		)
		fmt.Fprintln(output, "    next: daem apply --manage-existing --dry-run")
	case carrieradoption.ResultPresentUnclaimedIneligible:
		fmt.Fprintf(
			output,
			"  - carrier adoption unavailable subject=%q target=%s scope=%s source=%q: %s\n",
			subject,
			row.Target,
			row.Scope,
			row.SourceNamespace,
			strings.ReplaceAll(row.LifecycleBlocker, "_", " "),
		)
	case carrieradoption.ResultClaimConflict:
		fmt.Fprintf(
			output,
			"  - blocked carrier adoption subject=%q target=%s scope=%s: %d conflicting claims\n",
			subject,
			row.Target,
			row.Scope,
			row.ConflictingClaimCount,
		)
	case carrieradoption.ResultMissingRelation:
		fmt.Fprintf(
			output,
			"  - external carrier relation missing subject=%q target=%s scope=%s; acquisition remains a separate host action\n",
			subject,
			row.Target,
			row.Scope,
		)
	case carrieradoption.ResultInexactRelation:
		fmt.Fprintf(
			output,
			"  - carrier adoption refused subject=%q target=%s scope=%s: relation is not source-exact\n",
			subject,
			row.Target,
			row.Scope,
		)
	case carrieradoption.ResultObservationBlocked:
		fmt.Fprintf(
			output,
			"  - carrier adoption refused subject=%q target=%s scope=%s: observation %s\n",
			subject,
			row.Target,
			row.Scope,
			strings.ReplaceAll(row.CorrelationReason, "_", " "),
		)
	}
}

func printCarrierAdoptionVerbose(
	output io.Writer,
	action carrieradoption.Action,
	phase carrierAdoptionPhase,
	results []durablecarrier.ManagedCarrierClaim,
) {
	row := carrierAdoptionJSONActionsWithResults(
		[]carrieradoption.Action{action},
		phase,
		results,
	)[0]
	disclosure := carrierIdentityDisclosureFor(action.CarrierIdentity())
	fmt.Fprintf(
		output,
		"  - kind=%s subject=%q carrier=%q target=%s scope=%s source_namespace=%q relation_subject_key=%q result=%s correlation_state=%s correlation_reason=%s evidence_availability=%s evidence_freshness=%s claim_owner=%q claim_store=%s current_claim_provenance=%q proposed_claim_provenance=%q final_claim_provenance=%q claim_transition=%s lifecycle_eligible=%t lifecycle_blocker=%q daem_known_consumers=%d conflicting_claims=%d install_route_status=%s install_route_id=%q install_route_request_hash=%q removal_route_status=%s removal_route_id=%q removal_route_request_hash=%q removal_actuation=%q later_omission=%q preserves_shared_carrier=%t removed_effects=%q retained_effects=%q non_claims=%q ambient_consumer_assurance=%s manage_existing=%t invokes_host_route=%t state_only=%t blocks_ordinary_apply=%t\n",
		row.Kind,
		subjectString(*row.Subject),
		subjectString(*row.CarrierSubject),
		row.Target,
		row.Scope,
		disclosure.verboseSourceNamespace.Value(),
		disclosure.verboseRelationSubjectKey.Value(),
		row.Result,
		row.CorrelationState,
		row.CorrelationReason,
		row.EvidenceAvailability,
		row.EvidenceFreshness,
		row.ClaimOwner,
		row.ClaimStore,
		row.CurrentClaimProvenance,
		row.ProposedClaimProvenance,
		row.FinalClaimProvenance,
		row.ClaimTransition,
		row.LifecycleEligible,
		row.LifecycleBlocker,
		row.DaemKnownConsumerCount,
		row.ConflictingClaimCount,
		row.InstallRouteStatus,
		row.InstallRouteID,
		row.InstallRouteRequestHash,
		row.RemovalRouteStatus,
		row.RemovalRouteID,
		row.RemovalRouteRequestHash,
		row.RemovalActuation,
		row.LaterOmission,
		row.PreservesSharedCarrier,
		row.RemovedEffects,
		row.RetainedEffects,
		row.NonClaims,
		row.AmbientConsumerAssurance,
		row.ManageExisting,
		row.InvokesHostRoute,
		row.StateOnly,
		row.BlocksOrdinaryApply,
	)
}

func carrierAdoptionClaimTransition(
	action carrieradoption.Action,
	phase carrierAdoptionPhase,
) string {
	if !action.StateOnly() {
		return "none"
	}
	switch phase {
	case carrierAdoptionPlanned:
		return "would_record"
	case carrierAdoptionCommitted:
		return "recorded"
	case carrierAdoptionInstallRecovered:
		return "completed_by_install_recovery"
	case carrierAdoptionNotRecorded:
		return "not_recorded"
	case carrierAdoptionUnconfirmed:
		return "unknown_after_error"
	default:
		return "none"
	}
}

func carrierAdoptionResult(
	action carrieradoption.Action,
	phase carrierAdoptionPhase,
	results []durablecarrier.ManagedCarrierClaim,
) (carrierAdoptionPhase, durablecarrier.ManagedCarrierClaim, bool) {
	if !action.StateOnly() ||
		(phase != carrierAdoptionCommitted && phase != carrierAdoptionUnconfirmed) {
		return phase, durablecarrier.ManagedCarrierClaim{}, false
	}
	proposed, present := action.ProposedClaim()
	if !present {
		return carrierAdoptionUnconfirmed, durablecarrier.ManagedCarrierClaim{}, false
	}
	var matched durablecarrier.ManagedCarrierClaim
	matchCount := 0
	for _, claim := range results {
		if claim.SameAcquisition(proposed) {
			matched = claim
			matchCount++
		}
	}
	if matchCount != 1 {
		return carrierAdoptionUnconfirmed, durablecarrier.ManagedCarrierClaim{}, false
	}
	switch matched.Provenance() {
	case durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved:
		return carrierAdoptionCommitted, matched, true
	case durablecarrier.ClaimProvenanceInstalledObserved:
		return carrierAdoptionInstallRecovered, matched, true
	default:
		return carrierAdoptionUnconfirmed, durablecarrier.ManagedCarrierClaim{}, false
	}
}
