package reconcile

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/reconcile/carrieradoption"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// OperationContext identifies the workflow request for which a Result is complete.
type OperationContext string

const (
	ContextInspect OperationContext = "inspect"
	ContextDryRun  OperationContext = "dry-run"
	ContextApply   OperationContext = "apply"
)

// ResultInput contains every planned decision family for one operation context.
type ResultInput struct {
	Context          OperationContext
	ManagedPaths     []ManagedPathDecision
	Aggregates       []AggregateDecision
	Relations        []RelationAction
	CarrierAdoptions []carrieradoption.Action
	CarrierAbsences  []carrierabsence.Action
	Delegates        []DelegateAction
}

// Result is the immutable, canonically ordered reconciliation result.
type Result struct {
	managedPaths []ManagedPathDecision
	aggregates   []AggregateDecision
	relations    []RelationAction
	adoptions    []carrieradoption.Action
	absences     []carrierabsence.Action
	delegates    []DelegateAction
	context      OperationContext
}

// NewResult validates, orders, and defensively copies one complete
// reconciliation result.
func NewResult(input ResultInput) (Result, error) {
	if err := validateOperationContext(input.Context); err != nil {
		return Result{}, err
	}
	canonicalManagedPaths := append([]ManagedPathDecision(nil), input.ManagedPaths...)
	for index, decision := range canonicalManagedPaths {
		if err := validateManagedPathDecision(decision); err != nil {
			return Result{}, fmt.Errorf("managed path decision[%d]: %w", index, err)
		}
	}
	sort.Slice(canonicalManagedPaths, func(left int, right int) bool {
		return compareManagedPathDecisions(canonicalManagedPaths[left], canonicalManagedPaths[right]) < 0
	})
	for index := 1; index < len(canonicalManagedPaths); index++ {
		if compareManagedPathDecisionIdentity(canonicalManagedPaths[index-1], canonicalManagedPaths[index]) == 0 {
			return Result{}, fmt.Errorf(
				"duplicate managed path decision for subject %q and destination %q",
				canonicalManagedPaths[index].Subject(),
				canonicalManagedPaths[index].Destination(),
			)
		}
	}

	canonicalAggregates := append([]AggregateDecision(nil), input.Aggregates...)
	for index, decision := range canonicalAggregates {
		if err := validateAggregateDecision(decision); err != nil {
			return Result{}, fmt.Errorf("aggregate decision[%d]: %w", index, err)
		}
	}
	sort.Slice(canonicalAggregates, func(left int, right int) bool {
		return aggregateDecisionKey(canonicalAggregates[left]) < aggregateDecisionKey(canonicalAggregates[right])
	})
	for index := 1; index < len(canonicalAggregates); index++ {
		if aggregateDecisionKey(canonicalAggregates[index-1]) == aggregateDecisionKey(canonicalAggregates[index]) {
			return Result{}, fmt.Errorf(
				"duplicate aggregate decision for document %q and codec %q",
				canonicalAggregates[index].DocumentAddress().AggregateRoot(),
				canonicalAggregates[index].CodecContractID(),
			)
		}
	}

	canonicalRelations := append([]RelationAction(nil), input.Relations...)
	for index, action := range canonicalRelations {
		if err := validateRelationAction(action); err != nil {
			return Result{}, fmt.Errorf("relation action[%d]: %w", index, err)
		}
	}
	sort.SliceStable(canonicalRelations, func(left int, right int) bool {
		return canonicalRelations[left].Compare(canonicalRelations[right]) < 0
	})
	for index := 1; index < len(canonicalRelations); index++ {
		if canonicalRelations[index-1].Compare(canonicalRelations[index]) == 0 {
			return Result{}, fmt.Errorf("duplicate relation action for subject %q", canonicalRelations[index].Subject())
		}
	}

	canonicalAdoptions := append([]carrieradoption.Action(nil), input.CarrierAdoptions...)
	for index, action := range canonicalAdoptions {
		if err := action.Validate(); err != nil {
			return Result{}, fmt.Errorf("carrier adoption action[%d]: %w", index, err)
		}
	}
	sort.Slice(canonicalAdoptions, func(left int, right int) bool {
		return canonicalAdoptions[left].Compare(canonicalAdoptions[right]) < 0
	})
	for index := 1; index < len(canonicalAdoptions); index++ {
		if canonicalAdoptions[index-1].Compare(canonicalAdoptions[index]) == 0 {
			return Result{}, fmt.Errorf(
				"duplicate carrier adoption action for subject %q",
				canonicalAdoptions[index].Subject(),
			)
		}
	}

	canonicalAbsences := append([]carrierabsence.Action(nil), input.CarrierAbsences...)
	for index, action := range canonicalAbsences {
		if err := action.Validate(); err != nil {
			return Result{}, fmt.Errorf("carrier absence action[%d]: %w", index, err)
		}
	}
	sort.Slice(canonicalAbsences, func(left int, right int) bool {
		return canonicalAbsences[left].Compare(canonicalAbsences[right]) < 0
	})
	for index := 1; index < len(canonicalAbsences); index++ {
		if canonicalAbsences[index-1].Compare(canonicalAbsences[index]) == 0 {
			return Result{}, fmt.Errorf(
				"duplicate carrier absence action for subject %q",
				canonicalAbsences[index].Subject(),
			)
		}
	}

	canonicalDelegates := append([]DelegateAction(nil), input.Delegates...)
	for index, action := range canonicalDelegates {
		if err := validateDelegateAction(action); err != nil {
			return Result{}, fmt.Errorf("delegate action[%d]: %w", index, err)
		}
	}
	sort.SliceStable(canonicalDelegates, func(left int, right int) bool {
		return canonicalDelegates[left].Compare(canonicalDelegates[right]) < 0
	})
	for index := 1; index < len(canonicalDelegates); index++ {
		if canonicalDelegates[index-1].Compare(canonicalDelegates[index]) == 0 {
			return Result{}, fmt.Errorf("duplicate delegate action for subject %q", canonicalDelegates[index].Subject())
		}
	}
	if err := validateContextDecisions(input.Context, canonicalDelegates); err != nil {
		return Result{}, err
	}
	if err := validateDelegateDependencies(input.Context, canonicalManagedPaths, canonicalAggregates, canonicalDelegates); err != nil {
		return Result{}, err
	}

	return Result{
		managedPaths: canonicalManagedPaths,
		aggregates:   canonicalAggregates,
		relations:    canonicalRelations,
		adoptions:    canonicalAdoptions,
		absences:     canonicalAbsences,
		delegates:    canonicalDelegates,
		context:      input.Context,
	}, nil
}

// ManagedPaths returns a defensive copy of canonical managed-path decisions.
func (result Result) ManagedPaths() []ManagedPathDecision {
	return append([]ManagedPathDecision(nil), result.managedPaths...)
}

// Aggregates returns a defensive copy of canonical aggregate decisions.
func (result Result) Aggregates() []AggregateDecision {
	return append([]AggregateDecision(nil), result.aggregates...)
}

// Relations returns a defensive copy of canonical host-relation decisions.
func (result Result) Relations() []RelationAction {
	return append([]RelationAction(nil), result.relations...)
}

// CarrierAdoptions returns a defensive copy of canonical adoption decisions.
func (result Result) CarrierAdoptions() []carrieradoption.Action {
	return append([]carrieradoption.Action(nil), result.adoptions...)
}

// CarrierAbsences returns a defensive copy of canonical absence decisions.
func (result Result) CarrierAbsences() []carrierabsence.Action {
	return append([]carrierabsence.Action(nil), result.absences...)
}

// Delegates returns a defensive copy of canonical delegated-route decisions.
func (result Result) Delegates() []DelegateAction {
	return append([]DelegateAction(nil), result.delegates...)
}

// Clone returns an independent outer result. Managed decision internals are
// immutable and expose defensive accessors, so copying their outer slices is
// sufficient.
func (result Result) Clone() Result {
	return Result{
		managedPaths: result.ManagedPaths(),
		aggregates:   result.Aggregates(),
		relations:    result.Relations(),
		adoptions:    result.CarrierAdoptions(),
		absences:     result.CarrierAbsences(),
		delegates:    result.Delegates(),
		context:      result.context,
	}
}

func validateOperationContext(context OperationContext) error {
	switch context {
	case ContextInspect, ContextDryRun, ContextApply:
		return nil
	default:
		return fmt.Errorf("reconciliation operation context %q is unsupported", context)
	}
}

func validateContextDecisions(context OperationContext, delegates []DelegateAction) error {
	if context == ContextInspect && len(delegates) != 0 {
		return fmt.Errorf("inspect reconciliation result must not contain delegate actions")
	}
	for index, action := range delegates {
		switch context {
		case ContextDryRun:
			if action.Disposition() != DelegateSkipped {
				return fmt.Errorf("dry-run delegate action[%d] must be skipped", index)
			}
		case ContextApply:
			if action.Disposition() == DelegateSkipped {
				return fmt.Errorf("apply delegate action[%d] must be scheduled or blocked", index)
			}
		}
	}
	return nil
}

func validateRelationAction(action RelationAction) error {
	if err := action.Subject().Validate(); err != nil {
		return err
	}
	if _, err := target.ParseTarget(string(action.Target())); err != nil {
		return err
	}
	if _, err := target.ParseScope(string(action.Scope())); err != nil {
		return err
	}
	switch action.Basis() {
	case ActionBasisLockedRelation:
		if action.RelationSubjectKey() == "" {
			return fmt.Errorf("locked relation action requires relation subject key")
		}
		return validateRouteAdmission(action.RouteAdmission())
	default:
		return fmt.Errorf("relation action basis %q is unsupported", action.Basis())
	}
}

func validateDelegateAction(action DelegateAction) error {
	_, err := NewDelegateAction(DelegateActionInput{
		Subject:      action.Subject(),
		Target:       action.Target(),
		Scope:        action.Scope(),
		Plan:         action.Plan(),
		Disposition:  action.Disposition(),
		Risks:        action.Risks(),
		Dependencies: action.Dependencies(),
	})
	return err
}

func validateDelegateDependencies(
	context OperationContext,
	managedPaths []ManagedPathDecision,
	aggregates []AggregateDecision,
	delegates []DelegateAction,
) error {
	projectionBlocks := make(map[topology.SubjectID]bool)
	for _, decision := range managedPaths {
		projectionBlocks[decision.Subject()] = decision.IsBlocked()
	}
	for _, aggregate := range aggregates {
		for _, delta := range aggregate.subjectDeltas() {
			subject := delta.subject
			if _, conflict := projectionBlocks[subject]; conflict {
				return fmt.Errorf("conflicting projection decisions for %q", subject)
			}
			projectionBlocks[subject] = aggregate.IsBlocked() || delta.kind == AggregateBlocked
		}
	}
	for delegateIndex, action := range delegates {
		blockedDependencies := make(map[string]struct{})
		for dependencyIndex, dependency := range action.Dependencies() {
			if dependency.Kind != DelegateDependencyProjection {
				return fmt.Errorf("delegate action[%d] dependency[%d] has unsupported kind %q", delegateIndex, dependencyIndex, dependency.Kind)
			}
			blocked, present := projectionBlocks[dependency.Subject]
			if !present {
				return fmt.Errorf("delegate action[%d] dependency[%d] references missing projection %q", delegateIndex, dependencyIndex, dependency.Subject)
			}
			if blocked {
				blockedDependencies[delegateDependencyRiskSubject(dependency)] = struct{}{}
			}
		}

		preconditionRisks := make(map[string]struct{})
		for _, risk := range action.Risks() {
			if risk.Code == DelegateRiskPreconditionBlocked {
				preconditionRisks[risk.Subject] = struct{}{}
			}
		}
		for subject := range blockedDependencies {
			if _, present := preconditionRisks[subject]; !present {
				return fmt.Errorf("delegate action[%d] blocked dependency %q has no precondition-blocked risk", delegateIndex, subject)
			}
		}
		for subject := range preconditionRisks {
			if _, present := blockedDependencies[subject]; !present {
				return fmt.Errorf("delegate action[%d] precondition-blocked risk %q has no blocked dependency", delegateIndex, subject)
			}
		}
		if len(blockedDependencies) != 0 {
			switch context {
			case ContextApply:
				if action.Disposition() != DelegateBlocked {
					return fmt.Errorf("delegate action[%d] with a blocked dependency must be blocked during apply", delegateIndex)
				}
			case ContextDryRun:
				if action.Disposition() != DelegateSkipped {
					return fmt.Errorf("delegate action[%d] with a blocked dependency must be skipped during dry-run", delegateIndex)
				}
			}
		}
	}
	return nil
}

func delegateDependencyRiskSubject(dependency DelegateDependency) string {
	return string(dependency.Kind) + ":" + dependency.Subject.String()
}

func compareManagedPathDecisions(left ManagedPathDecision, right ManagedPathDecision) int {
	if identity := compareManagedPathDecisionIdentity(left, right); identity != 0 {
		return identity
	}
	switch {
	case left.Kind() < right.Kind():
		return -1
	case left.Kind() > right.Kind():
		return 1
	default:
		return 0
	}
}

func compareManagedPathDecisionIdentity(left ManagedPathDecision, right ManagedPathDecision) int {
	if subject := topology.CompareSubjectID(left.Subject(), right.Subject()); subject != 0 {
		return subject
	}
	switch {
	case left.Destination().String() < right.Destination().String():
		return -1
	case left.Destination().String() > right.Destination().String():
		return 1
	default:
		return 0
	}
}

func validateAggregateDecision(decision AggregateDecision) error {
	if err := validateAggregateDecisionKind(decision.kind); err != nil {
		return err
	}
	if err := decision.documentAddress.Validate(); err != nil {
		return fmt.Errorf("document: %w", err)
	}
	if len(decision.projections) == 0 {
		return fmt.Errorf("requires at least one projection")
	}
	seenProjections := make(map[string]struct{}, len(decision.projections))
	seenSubjects := make(map[topology.SubjectID]struct{})
	for index, projection := range decision.projections {
		contract := projection.contract
		if err := contract.Validate(); err != nil {
			return fmt.Errorf("projection[%d]: %w", index, err)
		}
		address := contract.Address().Document()
		if address.Target() != decision.documentAddress.Target() ||
			address.Scope() != decision.documentAddress.Scope() ||
			address.AggregateRoot() != decision.documentAddress.AggregateRoot() {
			return fmt.Errorf("projection[%d] belongs to another document", index)
		}
		if contract.CodecContractID() != decision.codecContractID {
			return fmt.Errorf(
				"projection[%d] codec %q does not match decision codec %q",
				index,
				contract.CodecContractID(),
				decision.codecContractID,
			)
		}
		projectionKey := aggregateAddressKey(contract.Address())
		if _, duplicate := seenProjections[projectionKey]; duplicate {
			return fmt.Errorf("duplicate projection address at projection[%d]", index)
		}
		seenProjections[projectionKey] = struct{}{}
		for _, subject := range projection.Subjects() {
			if _, duplicate := seenSubjects[subject]; duplicate {
				return fmt.Errorf("duplicate aggregate subject %q", subject)
			}
			seenSubjects[subject] = struct{}{}
		}
	}
	return nil
}

func validateAggregateDecisionKind(kind AggregateDecisionKind) error {
	switch kind {
	case AggregateCreate,
		AggregateReplace,
		AggregateRemove,
		AggregateRecord,
		AggregateNoOp,
		AggregateBlocked:
		return nil
	default:
		return fmt.Errorf("unsupported variant %q", kind)
	}
}
