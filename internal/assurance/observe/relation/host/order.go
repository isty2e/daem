package relationhost

import (
	"cmp"
	"fmt"
	"slices"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeopencode "github.com/isty2e/daem/internal/assurance/observe/opencodeplugin"
	observepipackage "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

// OrderInput identifies one locked extension-order class and the host roots
// from which its admitted physical sequences must be observed.
type OrderInput struct {
	Paths      daempaths.Paths
	Lockfile   lock.File
	Constraint hostrelation.RelationOrderConstraint
}

// PhysicalOrderObservation owns one canonical current physical sequence.
type PhysicalOrderObservation struct {
	sequence relationobserve.ObservedRelationSequence
}

// Sequence returns the canonical current physical order.
func (observation PhysicalOrderObservation) Sequence() relationobserve.ObservedRelationSequence {
	return observation.sequence
}

// OrderObservation contains every independently mutable physical sequence for
// one admitted logical order class. Host-native document values do not escape
// this specialization owner.
type OrderObservation struct {
	physical []PhysicalOrderObservation
}

// Physical returns defensive copies in canonical physical-sequence order.
func (observation OrderObservation) Physical() []PhysicalOrderObservation {
	return append([]PhysicalOrderObservation(nil), observation.physical...)
}

// ObserveOrder dispatches one locked class to its admitted host observer and
// returns only canonical sequence evidence.
func ObserveOrder(input OrderInput) (OrderObservation, error) {
	if err := input.Constraint.Validate(); err != nil {
		return OrderObservation{}, fmt.Errorf("extension order constraint: %w", err)
	}
	selectedTarget, capability, admitted := profile.ExtensionOrderCapabilityForClass(
		input.Constraint.ClassID(),
	)
	if !admitted {
		return OrderObservation{}, fmt.Errorf(
			"locked extension order class %q has no unique profile owner",
			input.Constraint.ClassID(),
		)
	}

	var physical []PhysicalOrderObservation
	switch selectedTarget {
	case target.TargetPi:
		relations, err := piOrderRelations(
			input.Paths,
			input.Lockfile,
			capability,
			input.Constraint,
		)
		if err != nil {
			return OrderObservation{}, err
		}
		observation, err := observepipackage.ReadOrder(observepipackage.OrderInput{
			Settings: observepipackage.SettingsInput{
				WorkDir:     input.Paths.ManifestRoot,
				ProjectRoot: input.Paths.ManifestRoot,
				Scope:       capability.Scope(),
			},
			Constraint: input.Constraint,
			Relations:  relations,
		})
		if err != nil {
			return OrderObservation{}, err
		}
		physical = append(physical, PhysicalOrderObservation{
			sequence: observation.Sequence(),
		})
	case target.TargetOpenCode:
		relations, err := openCodeOrderRelations(
			input.Lockfile,
			capability,
			input.Constraint,
		)
		if err != nil {
			return OrderObservation{}, err
		}
		observation, err := observeopencode.ReadOrder(observeopencode.OrderInput{
			Inventory: observeopencode.InventoryInput{
				ManifestRoot: input.Paths.ManifestRoot,
				Scope:        capability.Scope(),
			},
			Constraint: input.Constraint,
			Relations:  relations,
		})
		if err != nil {
			return OrderObservation{}, err
		}
		for _, document := range observation.Documents() {
			physical = append(physical, PhysicalOrderObservation{
				sequence: document.Sequence(),
			})
		}
	default:
		return OrderObservation{}, fmt.Errorf(
			"extension order class %q resolved to unsupported target %q",
			input.Constraint.ClassID(),
			selectedTarget,
		)
	}
	return newOrderObservation(capability, physical)
}

func newOrderObservation(
	capability profile.ExtensionOrderCapability,
	values []PhysicalOrderObservation,
) (OrderObservation, error) {
	expectedIDs := capability.PhysicalSequenceIDs()
	if len(values) != len(expectedIDs) {
		return OrderObservation{}, fmt.Errorf(
			"extension order class %q observed %d physical sequences, want %d",
			capability.ClassID(),
			len(values),
			len(expectedIDs),
		)
	}
	physical := append([]PhysicalOrderObservation(nil), values...)
	slices.SortFunc(physical, func(left, right PhysicalOrderObservation) int {
		return cmp.Compare(left.sequence.SequenceID(), right.sequence.SequenceID())
	})
	for index, observation := range physical {
		if observation.sequence.ClassID() != capability.ClassID() {
			return OrderObservation{}, fmt.Errorf(
				"extension order sequence %q class %q does not match %q",
				observation.sequence.SequenceID(),
				observation.sequence.ClassID(),
				capability.ClassID(),
			)
		}
		if observation.sequence.SequenceID() != expectedIDs[index] {
			return OrderObservation{}, fmt.Errorf(
				"extension order sequence[%d] is %q, want %q",
				index,
				observation.sequence.SequenceID(),
				expectedIDs[index],
			)
		}
	}
	return OrderObservation{physical: physical}, nil
}

func piOrderRelations(
	paths daempaths.Paths,
	locked lock.File,
	capability profile.ExtensionOrderCapability,
	constraint hostrelation.RelationOrderConstraint,
) ([]observepipackage.ScopedRelation, error) {
	relations := make([]observepipackage.ScopedRelation, 0, len(constraint.Members()))
	for index, member := range constraint.Members() {
		key, err := lockedOrderCorrelation(locked, capability, member)
		if err != nil {
			return nil, fmt.Errorf("Pi order member[%d]: %w", index, err)
		}
		relation, err := observepipackage.NewScopedRelation(
			key,
			capability.Scope(),
			paths.ManifestRoot,
		)
		if err != nil {
			return nil, fmt.Errorf("Pi order member[%d]: %w", index, err)
		}
		relations = append(relations, relation)
	}
	return relations, nil
}

func openCodeOrderRelations(
	locked lock.File,
	capability profile.ExtensionOrderCapability,
	constraint hostrelation.RelationOrderConstraint,
) ([]observeopencode.ScopedRelation, error) {
	relations := make([]observeopencode.ScopedRelation, 0, len(constraint.Members()))
	for index, member := range constraint.Members() {
		key, err := lockedOrderCorrelation(locked, capability, member)
		if err != nil {
			return nil, fmt.Errorf("OpenCode order member[%d]: %w", index, err)
		}
		relation, err := observeopencode.NewScopedRelation(key, capability.Scope())
		if err != nil {
			return nil, fmt.Errorf("OpenCode order member[%d]: %w", index, err)
		}
		relations = append(relations, relation)
	}
	return relations, nil
}

func lockedOrderCorrelation(
	locked lock.File,
	capability profile.ExtensionOrderCapability,
	member hostrelation.RelationOrderMember,
) (relationobserve.CorrelationKey, error) {
	contract, present := locked.Locked.Subject(member.Subject())
	if !present {
		return relationobserve.CorrelationKey{}, fmt.Errorf(
			"locked order subject %q is missing",
			member.Subject(),
		)
	}
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
	if err != nil {
		return relationobserve.CorrelationKey{}, err
	}
	if !admitted {
		return relationobserve.CorrelationKey{}, fmt.Errorf(
			"locked order subject %q is not a managed carrier relation",
			member.Subject(),
		)
	}
	if identity.Carrier().Family() != capability.Carrier() ||
		identity.Scope() != capability.Scope() {
		return relationobserve.CorrelationKey{}, fmt.Errorf(
			"locked order subject %q does not match capability %q/%q",
			member.Subject(),
			capability.Carrier(),
			capability.Scope(),
		)
	}
	return relationobserve.NewCorrelationKey(member.Subject(), identity.ExpectedRelation())
}
