package reconcile

import (
	"cmp"
	"fmt"
	"strings"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// RelationActionKind classifies a managed-relation planner fact.
type RelationActionKind string

const (
	ActionCreate          RelationActionKind = "create"
	ActionAttempt         RelationActionKind = "attempt"
	ActionNoOp            RelationActionKind = "no_op"
	ActionBlock           RelationActionKind = "block"
	ActionObserveOnly     RelationActionKind = "observe_only"
	ActionAssistCandidate RelationActionKind = "assist_candidate"
)

// RelationObservationPolicy controls how an admitted route treats the absence of a
// passive observer. It does not change supported, unavailable, stale, or
// conflicting observation states.
type RelationObservationPolicy string

const (
	ObservationRequireCurrent         RelationObservationPolicy = "require_current"
	ObservationAttemptWhenUnsupported RelationObservationPolicy = "attempt_when_unsupported"
)

// RelationExecutionClass describes the strongest effect this planner fact may request.
type RelationExecutionClass string

const (
	ExecutionNoMutation  RelationExecutionClass = "no_mutation"
	ExecutionBlocked     RelationExecutionClass = "blocked"
	ExecutionObserveOnly RelationExecutionClass = "observe_only"
	ExecutionHostRoute   RelationExecutionClass = "host_route"
	ExecutionAssisted    RelationExecutionClass = "assisted"
)

// RelationReasonCode is a stable reason for non-nominal managed-relation actions.
type RelationReasonCode string

const (
	ReasonNone                        RelationReasonCode = ""
	ReasonRouteNotAdmitted            RelationReasonCode = "route_not_admitted"
	ReasonRouteRequiresAssistance     RelationReasonCode = "route_requires_assistance"
	ReasonUnsupportedPassiveInventory RelationReasonCode = "unsupported_passive_inventory"
	ReasonStaleEvidence               RelationReasonCode = "stale_evidence"
	ReasonPresentUnclaimed            RelationReasonCode = "present_unclaimed"
	ReasonUnkeyedSameSubject          RelationReasonCode = "unkeyed_same_subject"
	ReasonSameSubjectShadow           RelationReasonCode = "same_name_shadow"
	ReasonManagedKeyDrift             RelationReasonCode = "managed_key_drift"
	ReasonAmbiguousRelation           RelationReasonCode = "ambiguous_relation"
	ReasonRelationEvidenceUnavailable RelationReasonCode = "relation_evidence_unavailable"
)

// RelationRouteAdmissionRow identifies the route-admission contract row selected for an action.
type RelationRouteAdmissionRow string

const (
	// RouteAdmissionRowInstallCarrier cites RA-01, the install/create carrier
	// relation row in the route-admission contract.
	RouteAdmissionRowInstallCarrier RelationRouteAdmissionRow = "RA-01"
)

// RelationAdmissionOutcome is the selected route-admission outcome vocabulary consumed by the planner.
type RelationAdmissionOutcome string

const (
	AdmissionOutcomeOrdinaryMutation RelationAdmissionOutcome = "ordinary-mutation"
	AdmissionOutcomeHostDelegated    RelationAdmissionOutcome = "host-delegated"
	AdmissionOutcomeAssisted         RelationAdmissionOutcome = "assisted"
	AdmissionOutcomeExplicitAttempt  RelationAdmissionOutcome = "explicit-attempt"
	AdmissionOutcomeObserveOnly      RelationAdmissionOutcome = "observe-only"
	AdmissionOutcomeBlocked          RelationAdmissionOutcome = "blocked"
)

// RelationRouteAdmissionSpec contains selected route-admission facts.
type RelationRouteAdmissionSpec struct {
	Row               RelationRouteAdmissionRow
	RequestedOutcome  RelationAdmissionOutcome
	SelectedOutcome   RelationAdmissionOutcome
	ObservationPolicy RelationObservationPolicy
}

// RelationRouteAdmissionDecision records selected route-admission facts. It consumes
// route admission; it does not own route evidence or execute routes.
type RelationRouteAdmissionDecision struct {
	row               RelationRouteAdmissionRow
	requestedOutcome  RelationAdmissionOutcome
	selectedOutcome   RelationAdmissionOutcome
	observationPolicy RelationObservationPolicy
}

// NewRouteAdmissionDecision validates and constructs selected route-admission facts.
func NewRelationRouteAdmissionDecision(spec RelationRouteAdmissionSpec) (RelationRouteAdmissionDecision, error) {
	if err := validateRouteAdmissionRow(spec.Row); err != nil {
		return RelationRouteAdmissionDecision{}, err
	}
	if err := validateAdmissionOutcome("requested outcome", spec.RequestedOutcome); err != nil {
		return RelationRouteAdmissionDecision{}, err
	}
	if err := validateAdmissionOutcome("selected outcome", spec.SelectedOutcome); err != nil {
		return RelationRouteAdmissionDecision{}, err
	}
	if err := validateObservationPolicy(spec.ObservationPolicy); err != nil {
		return RelationRouteAdmissionDecision{}, err
	}
	if spec.ObservationPolicy == ObservationAttemptWhenUnsupported &&
		spec.SelectedOutcome != AdmissionOutcomeOrdinaryMutation &&
		spec.SelectedOutcome != AdmissionOutcomeHostDelegated {
		return RelationRouteAdmissionDecision{}, fmt.Errorf(
			"observation policy %q requires a host-route admission outcome",
			spec.ObservationPolicy,
		)
	}
	return RelationRouteAdmissionDecision{
		row:               spec.Row,
		requestedOutcome:  spec.RequestedOutcome,
		selectedOutcome:   spec.SelectedOutcome,
		observationPolicy: spec.ObservationPolicy,
	}, nil
}

func validateRouteAdmissionRow(row RelationRouteAdmissionRow) error {
	trimmed := strings.TrimSpace(string(row))
	if trimmed == "" {
		return fmt.Errorf("route admission row is required")
	}
	if trimmed != string(row) {
		return fmt.Errorf("route admission row must be trimmed")
	}
	return nil
}

// Row returns the route-admission contract row this decision cites.
func (decision RelationRouteAdmissionDecision) Row() RelationRouteAdmissionRow {
	return decision.row
}

// RequestedOutcome returns the stronger route outcome requested by the dossier.
func (decision RelationRouteAdmissionDecision) RequestedOutcome() RelationAdmissionOutcome {
	return decision.requestedOutcome
}

// SelectedOutcome returns the honest outcome admitted for implementation.
func (decision RelationRouteAdmissionDecision) SelectedOutcome() RelationAdmissionOutcome {
	return decision.selectedOutcome
}

// ObservationPolicy returns the locked-current-evidence posture selected for
// this route.
func (decision RelationRouteAdmissionDecision) ObservationPolicy() RelationObservationPolicy {
	return decision.observationPolicy
}

// AllowsHostRouteInvocation reports whether the selected outcome admits host
// route execution through the normal mutating apply path.
func (decision RelationRouteAdmissionDecision) AllowsHostRouteInvocation() bool {
	switch decision.selectedOutcome {
	case AdmissionOutcomeOrdinaryMutation, AdmissionOutcomeHostDelegated:
		return true
	default:
		return false
	}
}

// RelationActionInput contains locked desired relation facts and current passive correlation.
type RelationActionInput struct {
	CarrierIdentity       durablecarrier.ManagedCarrierIdentity
	RouteRequest          realizationdelegate.Request
	Correlation           observerelation.CorrelationResult
	RouteAdmission        RelationRouteAdmissionDecision
	PendingInstallPresent bool
	ManagedClaimPresent   bool
}

// RelationAction is a planner fact for one managed relation.
type RelationAction struct {
	basis           RelationActionBasis
	kind            RelationActionKind
	carrierIdentity durablecarrier.ManagedCarrierIdentity
	routeRequest    realizationdelegate.Request
	correlation     observerelation.CorrelationResult
	reason          RelationReasonCode
	execution       RelationExecutionClass
	admission       RelationRouteAdmissionDecision
}

// Compare returns the canonical ordering of two managed-relation decisions.
// It preserves the stable planner order that predates the aggregate Result.
func (action RelationAction) Compare(other RelationAction) int {
	if order := cmp.Compare(action.Target(), other.Target()); order != 0 {
		return order
	}
	if order := cmp.Compare(action.Scope(), other.Scope()); order != 0 {
		return order
	}
	if order := cmp.Compare(action.Subject().Namespace(), other.Subject().Namespace()); order != 0 {
		return order
	}
	if order := cmp.Compare(action.Subject().Key(), other.Subject().Key()); order != 0 {
		return order
	}
	if order := cmp.Compare(action.RelationSubjectKey(), other.RelationSubjectKey()); order != 0 {
		return order
	}
	return cmp.Compare(action.basis, other.basis)
}

// NewRelationAction classifies locked relation and observation facts without invoking a host route.
func NewRelationAction(input RelationActionInput) (RelationAction, error) {
	if err := input.CarrierIdentity.Validate(); err != nil {
		return RelationAction{}, fmt.Errorf("relation action carrier identity: %w", err)
	}
	if err := validateRouteRequest(input.RouteRequest); err != nil {
		return RelationAction{}, err
	}
	if err := validateRouteAdmission(input.RouteAdmission); err != nil {
		return RelationAction{}, err
	}
	if err := validateCorrelationMatchesRelation(input.CarrierIdentity.ExpectedRelation(), input.Correlation); err != nil {
		return RelationAction{}, err
	}
	evidenceClass, err := input.CarrierIdentity.Carrier().RelationEvidence()
	if err != nil {
		return RelationAction{}, fmt.Errorf("relation action evidence class: %w", err)
	}

	kind, execution, reason, err := classify(
		input.Correlation.State(),
		input.RouteAdmission,
		input.PendingInstallPresent,
		input.ManagedClaimPresent,
		evidenceClass,
	)
	if err != nil {
		return RelationAction{}, err
	}
	return RelationAction{
		basis:           ActionBasisLockedRelation,
		kind:            kind,
		carrierIdentity: input.CarrierIdentity,
		routeRequest:    input.RouteRequest,
		correlation:     input.Correlation,
		reason:          reason,
		execution:       execution,
		admission:       input.RouteAdmission,
	}, nil
}

// Kind returns the planner-visible action kind.
func (action RelationAction) Kind() RelationActionKind {
	return action.kind
}

// Subject returns the locked subject this action describes.
func (action RelationAction) Subject() topology.SubjectID {
	return action.carrierIdentity.RelationSubject()
}

// Target returns the host target selected for this action.
func (action RelationAction) Target() target.Target {
	return action.carrierIdentity.Target()
}

// Scope returns the host scope selected for this action.
func (action RelationAction) Scope() target.Scope {
	return action.carrierIdentity.Scope()
}

// SourceNamespace returns the locked source/provenance namespace for this relation.
func (action RelationAction) SourceNamespace() string {
	return action.carrierIdentity.SourceNamespace()
}

// RelationSubjectKey returns the locked host-visible relation key.
func (action RelationAction) RelationSubjectKey() string {
	return string(action.carrierIdentity.ExpectedRelation().SubjectKey())
}

// CarrierIdentity returns the canonical structural carrier identity.
func (action RelationAction) CarrierIdentity() durablecarrier.ManagedCarrierIdentity {
	return action.carrierIdentity
}

// ExpectedRelation returns the exact host-visible structural correlation identity.
func (action RelationAction) ExpectedRelation() hostrelation.ExpectedRelation {
	return action.carrierIdentity.ExpectedRelation()
}

// RouteRequest returns the durable host route request identity, not authority to execute it.
func (action RelationAction) RouteRequest() realizationdelegate.Request {
	return action.routeRequest
}

// RouteAdmission returns the selected route-admission decision.
func (action RelationAction) RouteAdmission() RelationRouteAdmissionDecision {
	return action.admission
}

// CorrelationState returns the passive correlation state consumed by this action.
func (action RelationAction) CorrelationState() observerelation.CorrelationState {
	return action.correlation.State()
}

// CorrelationReason returns the passive correlation reason consumed by this action.
func (action RelationAction) CorrelationReason() observerelation.ReasonCode {
	return action.correlation.Reason()
}

// EvidenceAvailability returns the passive relation inventory availability
// consumed by this action.
func (action RelationAction) EvidenceAvailability() observerelation.InventoryAvailability {
	return action.correlation.EvidenceAvailability()
}

// EvidenceFreshness returns the passive relation inventory freshness consumed by
// this action.
func (action RelationAction) EvidenceFreshness() observerelation.EvidenceFreshness {
	return action.correlation.EvidenceFreshness()
}

// Watchpoints returns passive follow-up guidance preserved from correlation.
func (action RelationAction) Watchpoints() []observerelation.Watchpoint {
	return action.correlation.Watchpoints()
}

// Correlation returns the complete passive fact for a locked relation action.
func (action RelationAction) Correlation() (observerelation.CorrelationResult, bool) {
	return action.correlation, true
}

// Reason returns the planner-owned reason for blocked or observe-only actions.
func (action RelationAction) Reason() RelationReasonCode {
	return action.reason
}

// Execution returns the strongest effect class this planner fact may request.
func (action RelationAction) Execution() RelationExecutionClass {
	return action.execution
}

// BlocksOrdinaryApply reports whether ordinary status/apply must fail instead
// of treating this relation fact as converged or executable.
func (action RelationAction) BlocksOrdinaryApply() bool {
	return action.execution == ExecutionBlocked || action.execution == ExecutionAssisted
}

// InvokesHostRoute reports whether this action itself invokes an admitted host route.
func (action RelationAction) InvokesHostRoute() bool {
	return action.execution == ExecutionHostRoute && action.admission.AllowsHostRouteInvocation()
}

func validateRouteRequest(request realizationdelegate.Request) error {
	return request.Validate()
}

func validateRouteAdmission(decision RelationRouteAdmissionDecision) error {
	if err := validateRouteAdmissionRow(decision.Row()); err != nil {
		return fmt.Errorf("relation action: %w", err)
	}
	if err := validateAdmissionOutcome("requested outcome", decision.RequestedOutcome()); err != nil {
		return err
	}
	if err := validateAdmissionOutcome("selected outcome", decision.SelectedOutcome()); err != nil {
		return err
	}
	return validateObservationPolicy(decision.ObservationPolicy())
}

func validateAdmissionOutcome(label string, outcome RelationAdmissionOutcome) error {
	switch outcome {
	case AdmissionOutcomeOrdinaryMutation,
		AdmissionOutcomeHostDelegated,
		AdmissionOutcomeAssisted,
		AdmissionOutcomeExplicitAttempt,
		AdmissionOutcomeObserveOnly,
		AdmissionOutcomeBlocked:
		return nil
	default:
		return fmt.Errorf("%s %q is unsupported", label, outcome)
	}
}

func validateObservationPolicy(policy RelationObservationPolicy) error {
	switch policy {
	case ObservationRequireCurrent, ObservationAttemptWhenUnsupported:
		return nil
	default:
		return fmt.Errorf("observation policy %q is unsupported", policy)
	}
}

func validateCorrelationMatchesRelation(
	relation hostrelation.ExpectedRelation,
	correlation observerelation.CorrelationResult,
) error {
	switch correlation.State() {
	case observerelation.StateExactCorrelation:
		return validateExactCorrelationMatchesRelation(relation, correlation)
	case observerelation.StateUnkeyedSameSubject:
		return validateUnkeyedCorrelationMatchesRelation(relation, correlation)
	default:
		return nil
	}
}

func validateExactCorrelationMatchesRelation(
	relation hostrelation.ExpectedRelation,
	correlation observerelation.CorrelationResult,
) error {
	sameSubjectRows := correlation.SameSubjectRows()
	if len(sameSubjectRows) != 1 || sameSubjectRows[0].SubjectKey() != relation.SubjectKey() {
		return fmt.Errorf("exact relation correlation does not match locked relation subject key")
	}
	managedKeyRows := correlation.ManagedKeyRows()
	if len(managedKeyRows) != 1 {
		return fmt.Errorf("exact relation correlation does not match locked managed instance key")
	}
	managedKey, ok := managedKeyRows[0].ManagedInstanceKey()
	if !ok || managedKey != relation.ManagedInstanceKey() {
		return fmt.Errorf("exact relation correlation does not match locked managed instance key")
	}
	return nil
}

func validateUnkeyedCorrelationMatchesRelation(
	relation hostrelation.ExpectedRelation,
	correlation observerelation.CorrelationResult,
) error {
	sameSubjectRows := correlation.SameSubjectRows()
	if len(sameSubjectRows) != 1 || sameSubjectRows[0].SubjectKey() != relation.SubjectKey() {
		return fmt.Errorf("unkeyed relation correlation does not match locked relation subject key")
	}
	if len(correlation.ManagedKeyRows()) != 0 {
		return fmt.Errorf("unkeyed relation correlation must not contain a managed instance key")
	}
	if _, present := sameSubjectRows[0].ManagedInstanceKey(); present {
		return fmt.Errorf("unkeyed relation correlation must not contain a managed instance key")
	}
	return nil
}
