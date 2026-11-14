package record

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/realization/aggregate"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// Semantic replay facts are separate from the containing group's physical
// root-role, provenance, alias, selected-subject and operation authority joins.
type recoveryPiOrderEvidence struct {
	Scope           string                          `json:"scope"`
	SequenceID      string                          `json:"sequence_id"`
	Constraint      recoveryRelationOrderConstraint `json:"order_constraint"`
	SettingsContext recoveryLocalSourceContext      `json:"settings_context"`
	Relations       []recoveryPiScopedRelation      `json:"relations"`
}

type recoveryRelationOrderConstraint struct {
	ClassID                string                        `json:"class_id"`
	MemberIdentityContract string                        `json:"member_identity_contract"`
	RuntimeMeaning         string                        `json:"runtime_meaning"`
	Members                []recoveryRelationOrderMember `json:"members"`
}

type recoveryRelationOrderMember struct {
	Subject          persistedSubjectRef `json:"subject"`
	HostLoadIdentity string              `json:"host_load_identity"`
}

type recoveryPiScopedRelation struct {
	Subject            persistedSubjectRef        `json:"subject"`
	SubjectKey         string                     `json:"subject_key"`
	ManagedInstanceKey string                     `json:"managed_instance_key"`
	Scope              string                     `json:"scope"`
	SourceContext      recoveryLocalSourceContext `json:"source_context"`
}

type recoveryLocalSourceContext struct {
	BaseRoot string `json:"base_root"`
	HomeRoot string `json:"home_root"`
}

func (record *recoveryPiOrderEvidence) UnmarshalJSON(content []byte) error {
	if record == nil {
		return fmt.Errorf("Pi order evidence destination is nil")
	}
	if err := requireDocumentEvidenceFields(content, "scope", "sequence_id", "order_constraint", "settings_context", "relations"); err != nil {
		return err
	}
	type wire recoveryPiOrderEvidence
	var decoded wire
	if err := decodeRecoveryJSONStrict(content, &decoded); err != nil {
		return err
	}
	*record = recoveryPiOrderEvidence(decoded)
	return nil
}

func persistedPiOrderEvidence(observation pipackage.OrderObservation) (recoveryPiOrderEvidence, error) {
	input, err := observation.CaptureInput()
	if err != nil {
		return recoveryPiOrderEvidence{}, err
	}
	capability, err := native16PiOrderCapability(input.Scope)
	if err != nil {
		return recoveryPiOrderEvidence{}, err
	}
	if !input.Capability.Equal(capability) {
		return recoveryPiOrderEvidence{}, fmt.Errorf("Pi order capability differs from native16's interpretation")
	}
	if !observation.Changed() {
		return recoveryPiOrderEvidence{}, fmt.Errorf("unchanged Pi order has no selected recovery evidence")
	}
	constraint := input.Constraint
	record := recoveryPiOrderEvidence{
		Scope: string(input.Scope), SequenceID: string(observation.Sequence().SequenceID()),
		SettingsContext: persistedLocalSourceContext(input.SettingsContext),
		Constraint: recoveryRelationOrderConstraint{
			ClassID: string(constraint.ClassID()), MemberIdentityContract: constraint.MemberIdentityContract(),
			RuntimeMeaning: string(constraint.RuntimeMeaning()),
		},
	}
	for _, member := range constraint.Members() {
		subject := member.Subject()
		record.Constraint.Members = append(record.Constraint.Members, recoveryRelationOrderMember{
			Subject:          persistedSubjectRef{Kind: string(subject.Kind()), Namespace: subject.Namespace(), Name: subject.Key()},
			HostLoadIdentity: string(member.HostLoadIdentity()),
		})
	}
	for _, relation := range input.Relations {
		sourceContext, bound := relation.SourceContext()
		if !bound {
			return recoveryPiOrderEvidence{}, fmt.Errorf("Pi order relation has no captured source context")
		}
		key := relation.CorrelationKey()
		subject, expected := key.Subject(), key.ExpectedRelation()
		record.Relations = append(record.Relations, recoveryPiScopedRelation{
			Subject:    persistedSubjectRef{Kind: string(subject.Kind()), Namespace: subject.Namespace(), Name: subject.Key()},
			SubjectKey: string(expected.SubjectKey()), ManagedInstanceKey: string(expected.ManagedInstanceKey()),
			Scope: string(relation.Scope()), SourceContext: persistedLocalSourceContext(sourceContext),
		})
	}
	return record, nil
}

func persistedLocalSourceContext(value extensiontopology.LocalSourceContext) recoveryLocalSourceContext {
	return recoveryLocalSourceContext{BaseRoot: value.BaseRoot(), HomeRoot: value.HomeRoot()}
}

func (record recoveryPiOrderEvidence) canonical(ctx context.Context, original aggregate.Document) (pipackage.OrderObservation, error) {
	if ctx == nil {
		return pipackage.OrderObservation{}, fmt.Errorf("Pi order evidence context is required")
	}
	if err := ctx.Err(); err != nil {
		return pipackage.OrderObservation{}, err
	}
	scope, err := target.ParseScope(record.Scope)
	if err != nil {
		return pipackage.OrderObservation{}, err
	}
	capability, err := native16PiOrderCapability(scope)
	if err != nil {
		return pipackage.OrderObservation{}, err
	}
	sequenceID, err := hostrelation.NewPhysicalSequenceID(record.SequenceID)
	if err != nil {
		return pipackage.OrderObservation{}, err
	}
	settings, err := extensiontopology.NewLocalSourceContext(record.SettingsContext.BaseRoot, record.SettingsContext.HomeRoot)
	if err != nil {
		return pipackage.OrderObservation{}, err
	}
	constraint, err := record.Constraint.canonical(ctx)
	if err != nil {
		return pipackage.OrderObservation{}, err
	}
	relations := make([]pipackage.ScopedRelation, 0, len(record.Relations))
	for _, row := range record.Relations {
		if err := ctx.Err(); err != nil {
			return pipackage.OrderObservation{}, err
		}
		relation, err := row.canonical()
		if err != nil {
			return pipackage.OrderObservation{}, err
		}
		relations = append(relations, relation)
	}
	observation, err := pipackage.ReconstructOrder(pipackage.LoadedOrderInput{
		Scope: scope, Capability: capability, SettingsContext: settings, Document: original, Constraint: constraint, Relations: relations,
	})
	if err != nil {
		return pipackage.OrderObservation{}, err
	}
	if !observation.Changed() || observation.Sequence().SequenceID() != sequenceID {
		return pipackage.OrderObservation{}, fmt.Errorf("Pi order evidence has no matching changed physical sequence")
	}
	if err := ctx.Err(); err != nil {
		return pipackage.OrderObservation{}, err
	}
	return observation, nil
}

func (record recoveryRelationOrderConstraint) canonical(ctx context.Context) (hostrelation.RelationOrderConstraint, error) {
	classID, err := hostrelation.NewOrderClassID(record.ClassID)
	if err != nil {
		return hostrelation.RelationOrderConstraint{}, err
	}
	members := make([]hostrelation.RelationOrderMember, 0, len(record.Members))
	for _, row := range record.Members {
		if err := ctx.Err(); err != nil {
			return hostrelation.RelationOrderConstraint{}, err
		}
		subject, err := row.Subject.canonical()
		if err != nil {
			return hostrelation.RelationOrderConstraint{}, err
		}
		identity, err := hostrelation.NewHostLoadIdentity(row.HostLoadIdentity)
		if err != nil {
			return hostrelation.RelationOrderConstraint{}, err
		}
		member, err := hostrelation.NewRelationOrderMember(subject, identity)
		if err != nil {
			return hostrelation.RelationOrderConstraint{}, err
		}
		members = append(members, member)
	}
	return hostrelation.NewRelationOrderConstraint(classID, record.MemberIdentityContract, hostrelation.RuntimeMeaning(record.RuntimeMeaning), members)
}

func (record recoveryPiScopedRelation) canonical() (pipackage.ScopedRelation, error) {
	subject, err := record.Subject.canonical()
	if err != nil {
		return pipackage.ScopedRelation{}, err
	}
	source, err := hostrelation.NewSubjectKey(record.SubjectKey)
	if err != nil {
		return pipackage.ScopedRelation{}, err
	}
	managed, err := hostrelation.NewManagedInstanceKey(record.ManagedInstanceKey)
	if err != nil {
		return pipackage.ScopedRelation{}, err
	}
	expected, err := hostrelation.NewExpectedRelation(source, managed)
	if err != nil {
		return pipackage.ScopedRelation{}, err
	}
	key, err := observerelation.NewCorrelationKey(subject, expected)
	if err != nil {
		return pipackage.ScopedRelation{}, err
	}
	scope, err := target.ParseScope(record.Scope)
	if err != nil {
		return pipackage.ScopedRelation{}, err
	}
	sourceContext, err := extensiontopology.NewLocalSourceContext(record.SourceContext.BaseRoot, record.SourceContext.HomeRoot)
	if err != nil {
		return pipackage.ScopedRelation{}, err
	}
	return pipackage.NewScopedRelationWithContext(key, scope, sourceContext)
}
