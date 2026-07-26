package apply

import (
	"encoding/json"
	"fmt"
	"sort"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/effect/mutation"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/topology"
)

type applyFingerprintFacts struct {
	ManifestPath        string
	LockfilePath        string
	LockfileExplicit    bool
	Targets             []string
	ManageUnmanaged     bool
	DelegateMode        reconcile.OperationContext
	ManagedPaths        []managedPathFingerprintFacts
	Aggregates          []aggregateFingerprintFacts
	RelationActions     []relationFingerprintFacts
	CarrierAdoptions    []carrierAdoptionFingerprintFacts
	CarrierAbsences     []carrierAbsenceFingerprintFacts
	DelegateActions     []delegateFingerprintFacts
	Owner               ownershipOwnerFingerprintFacts
	Ownership           []ownershipObservationFingerprintFacts
	GlobalCarrierClaims []carrierClaimFingerprintFacts
	ProjectRoot         *projectRootFingerprintFacts
}

type ownershipOwnerFingerprintFacts struct {
	StatefileKey string
	ManifestPath string
}

type ownershipObservationFingerprintFacts struct {
	Destination       string
	ContentPath       string
	Path              string
	ClaimPresent      bool
	ClaimStatefileKey string
	ClaimManifestPath string
	ClaimState        string
	ClaimOperationID  string
}

type carrierClaimFingerprintFacts struct {
	StatefileKey       string
	ManifestPath       string
	CarrierSubject     topology.SubjectID
	RelationSubject    topology.SubjectID
	RelationSubjectKey string
	ManagedInstanceKey string
	InstallRequest     realizationdelegate.Request
	Provenance         durablecarrier.ClaimProvenance
}

type relationFingerprintFacts struct {
	Basis                reconcile.RelationActionBasis
	Kind                 reconcile.RelationActionKind
	Subject              topology.SubjectID
	Target               string
	Scope                string
	SourceNamespace      string
	RelationSubjectKey   string
	RouteRequest         realizationdelegate.Request
	CorrelationState     observerelation.CorrelationState
	CorrelationReason    observerelation.ReasonCode
	EvidenceAvailability observerelation.InventoryAvailability
	EvidenceFreshness    observerelation.EvidenceFreshness
	Watchpoints          []observerelation.Watchpoint
	Reason               reconcile.RelationReasonCode
	Execution            reconcile.RelationExecutionClass
	AdmissionRow         reconcile.RelationRouteAdmissionRow
	RequestedOutcome     reconcile.RelationAdmissionOutcome
	SelectedOutcome      reconcile.RelationAdmissionOutcome
	ObservationPolicy    reconcile.RelationObservationPolicy
}

type carrierAdoptionFingerprintFacts struct {
	Subject      topology.SubjectID
	Target       string
	Scope        string
	Result       carrieradoption.Result
	Blocker      carrieradoption.LifecycleBlocker
	PlanIdentity carrieradoption.PlanIdentity
}

type delegateFingerprintFacts struct {
	Subject      topology.SubjectID
	Target       string
	Scope        string
	PlanIdentity lock.DelegatePlanIdentity
	Status       reconcile.DelegateDisposition
	Outcome      reconcile.DelegatePolicyOutcome
	Disclosure   reconcile.DelegateDisclosure
	Risks        []reconcile.DelegateRisk
	Dependencies []reconcile.DelegateDependency
}

func applyOperationFingerprint(
	planned commandPlan,
	operationContext reconcile.OperationContext,
) (mutation.OperationFingerprint, error) {
	projectRoot, err := projectRootFingerprint(planned)
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	result := planned.result
	relations := make([]relationFingerprintFacts, 0, len(planned.assessment.Reconciliation.Relations()))
	for _, action := range planned.assessment.Reconciliation.Relations() {
		admission := action.RouteAdmission()
		relations = append(relations, relationFingerprintFacts{
			Basis:                action.Basis(),
			Kind:                 action.Kind(),
			Subject:              action.Subject(),
			Target:               string(action.Target()),
			Scope:                string(action.Scope()),
			SourceNamespace:      action.SourceNamespace(),
			RelationSubjectKey:   action.RelationSubjectKey(),
			RouteRequest:         action.RouteRequest(),
			CorrelationState:     action.CorrelationState(),
			CorrelationReason:    action.CorrelationReason(),
			EvidenceAvailability: action.EvidenceAvailability(),
			EvidenceFreshness:    action.EvidenceFreshness(),
			Watchpoints:          action.Watchpoints(),
			Reason:               action.Reason(),
			Execution:            action.Execution(),
			AdmissionRow:         admission.Row(),
			RequestedOutcome:     admission.RequestedOutcome(),
			SelectedOutcome:      admission.SelectedOutcome(),
			ObservationPolicy:    admission.ObservationPolicy(),
		})
	}
	carrierAbsences := carrierAbsenceFingerprintRows(
		planned.assessment.Reconciliation.CarrierAbsences(),
	)
	carrierAdoptions := make(
		[]carrierAdoptionFingerprintFacts,
		0,
		len(planned.assessment.Reconciliation.CarrierAdoptions()),
	)
	for _, action := range planned.assessment.Reconciliation.CarrierAdoptions() {
		carrierAdoptions = append(carrierAdoptions, carrierAdoptionFingerprintFacts{
			Subject:      action.Subject(),
			Target:       string(action.Target()),
			Scope:        string(action.Scope()),
			Result:       action.Result(),
			Blocker:      action.Lifecycle().Blocker(),
			PlanIdentity: action.PlanIdentity(),
		})
	}

	delegates := make([]delegateFingerprintFacts, 0, len(result.Reconciliation.Delegates()))
	for _, action := range result.Reconciliation.Delegates() {
		delegates = append(delegates, delegateFingerprintFacts{
			Subject:      action.Subject(),
			Target:       string(action.Target()),
			Scope:        string(action.Scope()),
			PlanIdentity: action.PlanIdentity(),
			Status:       action.Disposition(),
			Outcome:      action.PolicyOutcome(),
			Disclosure:   action.Disclosure(),
			Risks:        action.Risks(),
			Dependencies: action.Dependencies(),
		})
	}

	targets := planned.context.Selection.Targets()
	targetValues := make([]string, 0, len(targets))
	for _, selected := range targets {
		targetValues = append(targetValues, string(selected))
	}
	ownershipFacts := ownershipFingerprintFacts(planned.assessment.Ownership)
	carrierClaims := carrierClaimFingerprintRows(planned.assessment.GlobalCarrierClaims)
	managedPaths := managedPathFingerprintRows(planned.assessment.Reconciliation.ManagedPaths())
	aggregates := aggregateFingerprintRows(planned.assessment.Reconciliation.Aggregates())
	canonical, err := json.Marshal(applyFingerprintFacts{
		ManifestPath:     result.ManifestPath,
		LockfilePath:     result.LockfilePath,
		LockfileExplicit: result.LockfileExplicit,
		Targets:          targetValues,
		ManageUnmanaged:  planned.context.ManageUnmanagedMatches,
		DelegateMode:     operationContext,
		ManagedPaths:     managedPaths,
		Aggregates:       aggregates,
		RelationActions:  relations,
		CarrierAdoptions: carrierAdoptions,
		CarrierAbsences:  carrierAbsences,
		DelegateActions:  delegates,
		Owner: ownershipOwnerFingerprintFacts{
			StatefileKey: planned.assessment.Owner.StatefileKey(),
			ManifestPath: planned.assessment.Owner.ManifestPath(),
		},
		Ownership:           ownershipFacts,
		GlobalCarrierClaims: carrierClaims,
		ProjectRoot:         projectRoot,
	})
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint apply plan: %w", err)
	}
	return mutation.NewOperationFingerprint(canonical), nil
}

func carrierClaimFingerprintRows(
	registry durablecarrier.GlobalCarrierClaims,
) []carrierClaimFingerprintFacts {
	claims := registry.Claims()
	rows := make([]carrierClaimFingerprintFacts, 0, len(claims))
	for _, claim := range claims {
		identity := claim.Identity()
		relation := identity.ExpectedRelation()
		rows = append(rows, carrierClaimFingerprintFacts{
			StatefileKey:       claim.Owner().StatefileKey(),
			ManifestPath:       claim.Owner().ManifestPath(),
			CarrierSubject:     identity.CarrierSubject(),
			RelationSubject:    identity.RelationSubject(),
			RelationSubjectKey: string(relation.SubjectKey()),
			ManagedInstanceKey: string(relation.ManagedInstanceKey()),
			InstallRequest:     claim.InstallRequest(),
			Provenance:         claim.Provenance(),
		})
	}
	return rows
}

func ownershipFingerprintFacts(
	observations []observe.OwnershipObservation,
) []ownershipObservationFingerprintFacts {
	facts := make([]ownershipObservationFingerprintFacts, 0, len(observations))
	for _, observation := range observations {
		fact := ownershipObservationFingerprintFacts{
			Destination: string(observation.Destination),
			ContentPath: string(observation.ContentPath),
			Path:        observation.Address.Path(),
		}
		if claim, present := observation.Claim.Get(); present {
			fact.ClaimPresent = true
			fact.ClaimStatefileKey = claim.Owner().StatefileKey()
			fact.ClaimManifestPath = claim.Owner().ManifestPath()
			fact.ClaimState = string(claim.State())
			fact.ClaimOperationID = claim.OperationID()
		}
		facts = append(facts, fact)
	}
	sort.Slice(facts, func(left int, right int) bool {
		if facts[left].Path != facts[right].Path {
			return facts[left].Path < facts[right].Path
		}
		return facts[left].ContentPath < facts[right].ContentPath
	})
	return facts
}
