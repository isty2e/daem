package apply

import (
	"encoding/json"
	"fmt"
	"sort"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/findings"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/internal/workflow/readiness"
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
	MCPProviders        []mcpProviderFingerprintFacts
	RelationActions     []relationFingerprintFacts
	CarrierAdoptions    []carrierAdoptionFingerprintFacts
	CarrierAbsences     []carrierAbsenceFingerprintFacts
	DelegateActions     []delegateFingerprintFacts
	Owner               ownershipOwnerFingerprintFacts
	Ownership           []ownershipObservationFingerprintFacts
	GlobalCarrierClaims []carrierClaimFingerprintFacts
	Diagnostics         []diagnosticFingerprintFacts
	ProjectRoot         *projectRootFingerprintFacts
}

type mcpProviderFingerprintFacts struct {
	Contribution  topology.SubjectID
	Carrier       topology.SubjectID
	Relation      topology.SubjectID
	Consumers     []topology.SubjectID
	State         string
	Reason        string
	Version       string
	MappedCodec   string
	RequiredCodec string
	FailureDetail string
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
	Plan         delegatePlanFingerprintFacts
	Status       reconcile.DelegateDisposition
	Outcome      reconcile.DelegatePolicyOutcome
	Risks        []reconcile.DelegateRisk
	Dependencies []reconcile.DelegateDependency
}

type delegatePlanFingerprintFacts struct {
	IdentityKey string
	RunnerKind  realizationdelegate.RunnerKind
	Command     string
	Args        []string
	Env         []delegateEnvFingerprintFacts
	Package     *delegatePackageFingerprintFacts
	PinPolicy   realizationdelegate.PinPolicy
}

type delegateEnvFingerprintFacts struct {
	Name       string
	SourceName string
}

type delegatePackageFingerprintFacts struct {
	Ecosystem realizationdelegate.PackageEcosystem
	Name      string
	Selector  string
}

type diagnosticFingerprintFacts struct {
	Severity      findings.Severity
	Code          string
	EntityKind    string
	EntityName    string
	Target        string
	Scope         string
	Event         string
	Command       string
	Detail        string
	Repairability string
	RepairActions []string
	ManualReasons []string
	NextStep      string
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
	relations := relationFingerprintRows(planned.assessment.Reconciliation.Relations())
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

	delegates := delegateFingerprintRows(result.Reconciliation.Delegates())

	targets := planned.context.Selection.Targets()
	targetValues := make([]string, 0, len(targets))
	for _, selected := range targets {
		targetValues = append(targetValues, string(selected))
	}
	ownershipFacts := ownershipFingerprintFacts(planned.assessment.Ownership)
	carrierClaims := carrierClaimFingerprintRows(planned.assessment.GlobalCarrierClaims)
	managedPaths := managedPathFingerprintRows(planned.assessment.Reconciliation.ManagedPaths())
	aggregates := aggregateFingerprintRows(planned.assessment.Reconciliation.Aggregates())
	mcpProviders := mcpProviderFingerprintRows(planned.assessment.MCPProviders)
	diagnostics := diagnosticFingerprintRows(result.Diagnostics)
	canonical, err := json.Marshal(applyFingerprintFacts{
		ManifestPath:     result.ManifestPath,
		LockfilePath:     result.LockfilePath,
		LockfileExplicit: result.LockfileExplicit,
		Targets:          targetValues,
		ManageUnmanaged:  planned.context.ManageUnmanagedMatches,
		DelegateMode:     operationContext,
		ManagedPaths:     managedPaths,
		Aggregates:       aggregates,
		MCPProviders:     mcpProviders,
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
		Diagnostics:         diagnostics,
		ProjectRoot:         projectRoot,
	})
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint apply plan: %w", err)
	}
	return mutation.NewOperationFingerprint(canonical), nil
}

func relationFingerprintRows(
	actions []reconcile.RelationAction,
) []relationFingerprintFacts {
	relations := make([]relationFingerprintFacts, 0, len(actions))
	for _, action := range actions {
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
	return relations
}

func mcpProviderFingerprintRows(
	prerequisites []readiness.MCPProviderPrerequisite,
) []mcpProviderFingerprintFacts {
	rows := make([]mcpProviderFingerprintFacts, 0, len(prerequisites))
	for _, prerequisite := range prerequisites {
		observation := prerequisite.Observation()
		rows = append(rows, mcpProviderFingerprintFacts{
			Contribution:  observation.Contribution().SubjectID(),
			Carrier:       observation.Carrier().CarrierSubject(),
			Relation:      observation.Carrier().RelationSubject(),
			Consumers:     observation.Consumers(),
			State:         string(prerequisite.State()),
			Reason:        string(prerequisite.Reason()),
			Version:       observation.Version(),
			MappedCodec:   string(observation.MappedCodec()),
			RequiredCodec: string(prerequisite.RequiredCodec()),
			FailureDetail: prerequisite.Detail(),
		})
	}
	sort.Slice(rows, func(left int, right int) bool {
		return topology.CompareSubjectID(rows[left].Contribution, rows[right].Contribution) < 0
	})
	return rows
}

func diagnosticFingerprintRows(values []findings.Diagnostic) []diagnosticFingerprintFacts {
	rows := make([]diagnosticFingerprintFacts, 0, len(values))
	for _, diagnostic := range values {
		rows = append(rows, diagnosticFingerprintFacts{
			Severity:      diagnostic.Severity,
			Code:          diagnostic.Code,
			EntityKind:    string(diagnostic.EntityID.Kind()),
			EntityName:    diagnostic.EntityID.Name(),
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
	sort.Slice(rows, func(left int, right int) bool {
		leftBytes, _ := json.Marshal(rows[left])
		rightBytes, _ := json.Marshal(rows[right])
		return string(leftBytes) < string(rightBytes)
	})
	return rows
}

func delegatePlanFingerprint(plan realizationdelegate.DelegatePlan) delegatePlanFingerprintFacts {
	command := plan.Command()
	envBindings := plan.Env().Bindings()
	env := make([]delegateEnvFingerprintFacts, 0, len(envBindings))
	for _, binding := range envBindings {
		env = append(env, delegateEnvFingerprintFacts{
			Name:       binding.Name(),
			SourceName: binding.SourceName(),
		})
	}
	facts := delegatePlanFingerprintFacts{
		IdentityKey: plan.IdentityKey(),
		RunnerKind:  plan.Runner().Kind(),
		Command:     command.Executable(),
		Args:        command.Args(),
		Env:         env,
		PinPolicy:   plan.PinPolicy(),
	}
	if packageRef, present := plan.PackageRef(); present {
		facts.Package = &delegatePackageFingerprintFacts{
			Ecosystem: packageRef.Ecosystem(),
			Name:      packageRef.Name(),
			Selector:  packageRef.Selector(),
		}
	}
	return facts
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
			Destination: observation.Destination.String(),
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
