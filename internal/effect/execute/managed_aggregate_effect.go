package execute

import (
	"fmt"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// AggregateEffectKind is the closed executable/state-only aggregate result.
type AggregateEffectKind string

const (
	AggregateEffectCreate  AggregateEffectKind = "create"
	AggregateEffectReplace AggregateEffectKind = "replace"
	AggregateEffectRemove  AggregateEffectKind = "remove"
	AggregateEffectRecord  AggregateEffectKind = "record"
)

// AggregateEffect is execution's validated view of one aggregate decision.
// It carries a candidate but no mutation authority.
type AggregateEffect struct {
	kind               AggregateEffectKind
	documentAddress    aggregate.DocumentAddress
	codecContractID    aggregate.CodecContractID
	projections        []AggregateProjectionEffect
	before             aggregate.Document
	snapshot           aggregate.Snapshot
	codecPlan          aggregate.Plan
	rendered           aggregate.RenderedDocument
	evidence           observe.AggregateEvidence
	preconditions      []aggregate.OperationPrecondition
	nativeOperation    profile.NativeAggregateOperationContract
	hasNativeOperation bool
}

// AggregateProjectionEffect is execution's immutable semantic transition for
// one selected projection inside a document batch.
type AggregateProjectionEffect struct {
	kind     AggregateEffectKind
	contract aggregate.ProjectionContract
	subjects []AggregateSubjectEffect
	desired  *aggregate.ContributionSet
	previous []durable.ManagedAggregateState
	before   aggregate.ProjectionState
	expected aggregate.ProjectionState
}

// AggregateSubjectEffect is one state-mutating subject transition. No-op
// subjects never enter execution.
type AggregateSubjectEffect struct {
	kind     AggregateEffectKind
	subject  topology.SubjectID
	contract aggregate.ProjectionContract
}

// AggregateEffects admits ordinary executable and state-recording decisions.
func AggregateEffects(decisions []reconcile.AggregateDecision) ([]AggregateEffect, error) {
	for _, decision := range decisions {
		if _, native := aggregate.HostConfigSyntaxForCodec(decision.CodecContractID()); native {
			return nil, ErrNativeOperationRequired
		}
	}
	return aggregateEffects(decisions)
}

// AggregateEffectsForNativeOperation converts actual effects with their static
// admission. No-op contracts remain with native state preparation, not Effects.
func AggregateEffectsForNativeOperation(decisions []reconcile.AggregateDecision) ([]AggregateEffect, error) {
	return aggregateEffects(decisions)
}

func aggregateEffects(decisions []reconcile.AggregateDecision) ([]AggregateEffect, error) {
	effects := make([]AggregateEffect, 0, len(decisions))
	for index, decision := range decisions {
		var kind AggregateEffectKind
		switch decision.Kind() {
		case reconcile.AggregateCreate:
			kind = AggregateEffectCreate
		case reconcile.AggregateReplace:
			kind = AggregateEffectReplace
		case reconcile.AggregateRemove:
			kind = AggregateEffectRemove
		case reconcile.AggregateRecord:
			kind = AggregateEffectRecord
		case reconcile.AggregateNoOp:
			continue
		case reconcile.AggregateBlocked:
			return nil, fmt.Errorf("aggregate decision[%d] is blocked: %s", index, decision.Reason())
		default:
			return nil, fmt.Errorf("aggregate decision[%d] has invalid kind %q", index, decision.Kind())
		}
		projections := make([]AggregateProjectionEffect, 0, len(decision.Projections()))
		for projectionIndex, projection := range decision.Projections() {
			effectProjection, err := aggregateProjectionEffect(projection)
			if err != nil {
				return nil, fmt.Errorf(
					"aggregate effect[%d] projection[%d]: %w",
					index,
					projectionIndex,
					err,
				)
			}
			projections = append(projections, effectProjection)
		}
		nativeOperation, hasNativeOperation := decision.NativeOperationContract()
		effect := AggregateEffect{
			kind: kind, documentAddress: decision.DocumentAddress(),
			codecContractID: decision.CodecContractID(), projections: projections,
			before: decision.BeforeDocument(), snapshot: decision.BeforeSnapshot(), codecPlan: decision.CodecPlan(),
			rendered: decision.Rendered(), evidence: decision.Evidence(),
			preconditions:   decision.OperationPreconditions(),
			nativeOperation: nativeOperation, hasNativeOperation: hasNativeOperation,
		}
		if err := effect.validate(); err != nil {
			return nil, fmt.Errorf("aggregate effect[%d]: %w", index, err)
		}
		effects = append(effects, effect)
	}
	return effects, nil
}

func aggregateProjectionEffect(
	projection reconcile.AggregateProjectionDecision,
) (AggregateProjectionEffect, error) {
	var kind AggregateEffectKind
	switch projection.Kind() {
	case reconcile.AggregateCreate:
		kind = AggregateEffectCreate
	case reconcile.AggregateReplace:
		kind = AggregateEffectReplace
	case reconcile.AggregateRemove:
		kind = AggregateEffectRemove
	case reconcile.AggregateRecord, reconcile.AggregateNoOp:
		kind = AggregateEffectRecord
	case reconcile.AggregateBlocked:
		return AggregateProjectionEffect{}, fmt.Errorf("projection is blocked: %s", projection.Reason())
	default:
		return AggregateProjectionEffect{}, fmt.Errorf("projection has invalid kind %q", projection.Kind())
	}
	subjects := make([]AggregateSubjectEffect, 0)
	for _, decision := range projection.SubjectDecisions() {
		if !decision.MutatesState() {
			continue
		}
		subjectKind, err := aggregateSubjectEffectKind(decision.Kind())
		if err != nil {
			return AggregateProjectionEffect{}, fmt.Errorf("subject %q: %w", decision.Subject(), err)
		}
		subjects = append(subjects, AggregateSubjectEffect{
			kind: subjectKind, subject: decision.Subject(), contract: decision.Contract(),
		})
	}
	effect := AggregateProjectionEffect{
		kind: kind, contract: projection.Contract(), subjects: subjects,
		previous: projection.PreviousStates(), before: projection.Before(), expected: projection.Expected(),
	}
	if desired, present := projection.Desired(); present {
		effect.desired = &desired
	}
	return effect, nil
}

func aggregateSubjectEffectKind(kind reconcile.AggregateDecisionKind) (AggregateEffectKind, error) {
	switch kind {
	case reconcile.AggregateCreate:
		return AggregateEffectCreate, nil
	case reconcile.AggregateReplace:
		return AggregateEffectReplace, nil
	case reconcile.AggregateRemove:
		return AggregateEffectRemove, nil
	case reconcile.AggregateRecord:
		return AggregateEffectRecord, nil
	default:
		return "", fmt.Errorf("subject decision kind %q is not executable", kind)
	}
}

func (effect AggregateEffect) validate() error {
	if len(effect.projections) == 0 {
		return fmt.Errorf("aggregate effect requires at least one projection")
	}
	subjects := effect.SubjectEffects()
	if len(subjects) == 0 {
		return fmt.Errorf("aggregate effect requires at least one subject")
	}
	for index, subject := range subjects {
		if err := subject.subject.Validate(); err != nil {
			return fmt.Errorf("subject[%d]: %w", index, err)
		}
		if err := subject.contract.Validate(); err != nil {
			return fmt.Errorf("subject[%d] contract: %w", index, err)
		}
	}
	selection, err := effect.snapshot.Selection()
	if err != nil {
		return err
	}
	if selection.DocumentAddress() != effect.documentAddress ||
		selection.CodecContractID() != effect.codecContractID {
		return fmt.Errorf("aggregate effect selection differs from document identity")
	}
	if effect.evidence.Address() != effect.documentAddress ||
		!effect.evidence.Document().Equal(effect.before) ||
		!effect.evidence.Snapshot().Equal(effect.snapshot) {
		return fmt.Errorf("aggregate effect evidence differs from planned before state")
	}
	if _, native := aggregate.HostConfigSyntaxForCodec(effect.codecContractID); native {
		if !effect.hasNativeOperation || len(effect.preconditions) != 0 {
			return fmt.Errorf("native aggregate effect requires its exact static operation contract")
		}
		if err := effect.nativeOperation.ValidateSelection(selection); err != nil {
			return err
		}
	} else {
		if effect.hasNativeOperation {
			return fmt.Errorf("ordinary aggregate effect carries a native operation contract")
		}
		expectedPreconditions, admitted, err := aggregate.OperationPreconditionsForSelection(selection)
		if err != nil {
			return err
		}
		if !admitted || len(expectedPreconditions) != len(effect.preconditions) {
			return fmt.Errorf("aggregate effect operation preconditions differ from codec profile")
		}
		for index := range expectedPreconditions {
			if expectedPreconditions[index] != effect.preconditions[index] {
				return fmt.Errorf("aggregate effect operation precondition[%d] differs from codec profile", index)
			}
		}
	}
	beforeStates := effect.snapshot.States()
	expectedStates := effect.rendered.Expected().States()
	if len(beforeStates) != len(effect.projections) || len(expectedStates) != len(effect.projections) {
		return fmt.Errorf("aggregate effect transition does not cover every projection")
	}
	for index, projection := range effect.projections {
		if err := projection.validate(beforeStates[index], expectedStates[index]); err != nil {
			return fmt.Errorf("projection[%d]: %w", index, err)
		}
	}
	if err := validateAggregateDocumentEffectTransition(
		effect.kind,
		effect.before,
		effect.rendered.Document(),
	); err != nil {
		return err
	}
	return nil
}

func (projection AggregateProjectionEffect) validate(
	before aggregate.ProjectionState,
	expected aggregate.ProjectionState,
) error {
	if err := projection.contract.Validate(); err != nil {
		return err
	}
	if !projection.before.Contract().Equal(projection.contract) ||
		!aggregateProjectionStatesEqual(projection.before, before) ||
		!projection.expected.Contract().Equal(projection.contract) ||
		!aggregateProjectionStatesEqual(projection.expected, expected) {
		return fmt.Errorf("aggregate projection effect differs from document snapshot")
	}
	if projection.expected.Present() != (projection.desired != nil) {
		return fmt.Errorf("aggregate projection desired presence differs from expected state")
	}
	if _, native := aggregate.HostConfigSyntaxForCodec(projection.contract.CodecContractID()); native {
		for _, subject := range projection.subjects {
			if err := aggregate.ValidateSubjectContract(subject.subject, projection.contract); err != nil {
				return err
			}
			selected := false
			for _, state := range projection.previous {
				selected = selected || state.Subject() == subject.subject
			}
			if projection.desired != nil {
				for _, item := range projection.desired.Contributions() {
					selected = selected || item.SubjectID() == subject.subject
				}
			}
			if !selected {
				return fmt.Errorf("native effect subject has no desired or previous contribution")
			}
		}
	}
	if _, native := aggregate.HostConfigSyntaxForCodec(projection.contract.CodecContractID()); native &&
		projection.kind == AggregateEffectRecord && !before.Present() && !expected.Present() {
		if len(projection.previous) == 0 || len(projection.subjects) != len(projection.previous) {
			return fmt.Errorf("native absent retirement requires exact prior subject coverage")
		}
		remaining := make(map[topology.SubjectID]struct{}, len(projection.previous))
		for _, state := range projection.previous {
			remaining[state.Subject()] = struct{}{}
		}
		for _, subject := range projection.subjects {
			if _, present := remaining[subject.subject]; !present || subject.kind != AggregateEffectRemove {
				return fmt.Errorf("native absent retirement requires one removal per prior subject")
			}
			delete(remaining, subject.subject)
		}
		if len(remaining) != 0 {
			return fmt.Errorf("native absent retirement omits a prior subject")
		}
		return nil
	}
	if projection.kind != AggregateEffectRecord && len(projection.subjects) == 0 {
		return fmt.Errorf("mutating aggregate projection requires a subject transition")
	}
	return validateAggregateEffectTransition(
		projection.kind,
		projection.before.Present(),
		projection.expected.Present(),
	)
}

func validateAggregateEffectTransition(kind AggregateEffectKind, beforePresent bool, expectedPresent bool) error {
	valid := false
	switch kind {
	case AggregateEffectCreate:
		valid = !beforePresent && expectedPresent
	case AggregateEffectReplace, AggregateEffectRecord:
		valid = beforePresent && expectedPresent
	case AggregateEffectRemove:
		valid = beforePresent && !expectedPresent
	default:
		return fmt.Errorf("aggregate effect kind %q is unsupported", kind)
	}
	if !valid {
		return fmt.Errorf(
			"aggregate %s effect has invalid projection transition %t -> %t",
			kind,
			beforePresent,
			expectedPresent,
		)
	}
	return nil
}

func validateAggregateDocumentEffectTransition(
	kind AggregateEffectKind,
	before aggregate.Document,
	expected aggregate.Document,
) error {
	valid := false
	switch kind {
	case AggregateEffectCreate:
		valid = !before.Exists() && expected.Exists()
	case AggregateEffectReplace:
		valid = before.Exists() && expected.Exists()
	case AggregateEffectRemove:
		valid = before.Exists() && !expected.Exists()
	case AggregateEffectRecord:
		valid = before.Equal(expected)
	default:
		return fmt.Errorf("aggregate document effect kind %q is unsupported", kind)
	}
	if !valid {
		return fmt.Errorf(
			"aggregate document %s effect has invalid transition %t -> %t",
			kind,
			before.Exists(),
			expected.Exists(),
		)
	}
	return nil
}

func aggregateProjectionStatesEqual(left aggregate.ProjectionState, right aggregate.ProjectionState) bool {
	return left.Equal(right)
}

func (effect AggregateEffect) Kind() AggregateEffectKind { return effect.kind }
func (effect AggregateEffect) SubjectEffects() []AggregateSubjectEffect {
	result := make([]AggregateSubjectEffect, 0)
	for _, projection := range effect.projections {
		result = append(result, projection.subjects...)
	}
	return result
}

func (effect AggregateEffect) ProjectionEffects() []AggregateProjectionEffect {
	return cloneAggregateProjectionEffects(effect.projections)
}

func (effect AggregateEffect) DocumentAddress() aggregate.DocumentAddress {
	return effect.documentAddress
}

func (effect AggregateEffect) CodecContractID() aggregate.CodecContractID {
	return effect.codecContractID
}

func (effect AggregateEffect) Target() target.Target {
	return effect.documentAddress.Target()
}

func (effect AggregateEffect) Scope() target.Scope {
	return effect.documentAddress.Scope()
}

func (effect AggregateEffect) Destination() output.Destination {
	return effect.documentAddress.AggregateRoot()
}
func (effect AggregateEffect) BeforeDocument() aggregate.Document { return effect.before }
func (effect AggregateEffect) BeforeSnapshot() aggregate.Snapshot { return effect.snapshot }
func (effect AggregateEffect) CodecPlan() aggregate.Plan          { return effect.codecPlan }
func (effect AggregateEffect) Rendered() aggregate.RenderedDocument {
	return effect.rendered
}

func (effect AggregateEffect) DesiredContributions() []aggregate.SubjectContribution {
	result := make([]aggregate.SubjectContribution, 0)
	for _, projection := range effect.projections {
		if projection.desired != nil {
			result = append(result, projection.desired.Contributions()...)
		}
	}
	return result
}

func (effect AggregateEffect) PreviousStates() []durable.ManagedAggregateState {
	result := make([]durable.ManagedAggregateState, 0)
	for _, projection := range effect.projections {
		result = append(result, projection.previous...)
	}
	return result
}
func (effect AggregateEffect) Evidence() observe.AggregateEvidence { return effect.evidence }
func (effect AggregateEffect) OperationPreconditions() []aggregate.OperationPrecondition {
	return append([]aggregate.OperationPrecondition(nil), effect.preconditions...)
}

func (effect AggregateEffect) NativeOperationContract() (profile.NativeAggregateOperationContract, bool) {
	return effect.nativeOperation, effect.hasNativeOperation
}

func (effect AggregateEffect) journaledProjectionCount() int {
	count := 0
	for _, projection := range effect.projections {
		if projection.isJournaled() {
			count++
		}
	}
	return count
}

func (subject AggregateSubjectEffect) Kind() AggregateEffectKind { return subject.kind }
func (subject AggregateSubjectEffect) Subject() topology.SubjectID {
	return subject.subject
}

func (subject AggregateSubjectEffect) Contract() aggregate.ProjectionContract {
	return subject.contract.Clone()
}

func (projection AggregateProjectionEffect) Kind() AggregateEffectKind { return projection.kind }
func (projection AggregateProjectionEffect) Contract() aggregate.ProjectionContract {
	return projection.contract.Clone()
}

func (projection AggregateProjectionEffect) Subjects() []AggregateSubjectEffect {
	return append([]AggregateSubjectEffect(nil), projection.subjects...)
}

func (projection AggregateProjectionEffect) MutatesHost() bool {
	return projection.kind == AggregateEffectCreate ||
		projection.kind == AggregateEffectReplace ||
		projection.kind == AggregateEffectRemove
}

func (projection AggregateProjectionEffect) isJournaled() bool {
	return len(projection.subjects) != 0
}

func cloneAggregateProjectionEffects(values []AggregateProjectionEffect) []AggregateProjectionEffect {
	result := make([]AggregateProjectionEffect, len(values))
	for index, value := range values {
		result[index] = value
		result[index].contract = value.contract.Clone()
		result[index].subjects = append([]AggregateSubjectEffect(nil), value.subjects...)
		if value.desired != nil {
			copy, _ := aggregate.NewContributionSet(value.desired.Contributions())
			result[index].desired = &copy
		}
		result[index].previous = append([]durable.ManagedAggregateState(nil), value.previous...)
	}
	return result
}

func aggregateJournalMutations(effects []AggregateEffect) ([]journalrecord.ManagedAggregateMutation, error) {
	mutations := make([]journalrecord.ManagedAggregateMutation, 0, len(effects))
	for index, effect := range effects {
		for projectionIndex, projection := range effect.ProjectionEffects() {
			if !projection.isJournaled() {
				continue
			}
			subjects := projection.Subjects()
			kind, err := aggregateJournalMutationKind(projection.Kind())
			if err != nil {
				return nil, fmt.Errorf(
					"aggregate effect[%d] projection[%d]: %w",
					index,
					projectionIndex,
					err,
				)
			}
			mutation, err := journalrecord.NewManagedAggregateMutation(
				kind,
				subjects[0].Subject(),
				projection.Contract(),
				effect.BeforeDocument(),
				effect.BeforeSnapshot(),
				effect.Rendered(),
				effect.Evidence().FileMode(),
			)
			if err != nil {
				return nil, fmt.Errorf(
					"aggregate effect[%d] projection[%d] journal mutation: %w",
					index,
					projectionIndex,
					err,
				)
			}
			mutations = append(mutations, mutation)
		}
	}
	return mutations, nil
}

func aggregateJournalMutationKind(kind AggregateEffectKind) (journalrecord.AggregateMutationKind, error) {
	switch kind {
	case AggregateEffectCreate:
		return journalrecord.AggregateMutationCreate, nil
	case AggregateEffectReplace:
		return journalrecord.AggregateMutationReplace, nil
	case AggregateEffectRemove:
		return journalrecord.AggregateMutationRemove, nil
	case AggregateEffectRecord:
		return journalrecord.AggregateMutationRecord, nil
	default:
		return "", fmt.Errorf("aggregate projection effect kind %q is not journalable", kind)
	}
}

func aggregateSubjectCount(effects []AggregateEffect) int {
	count := 0
	for _, effect := range effects {
		count += len(effect.SubjectEffects())
	}
	return count
}
