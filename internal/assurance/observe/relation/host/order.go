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

// OrderAuthorityPath is one target-visible path whose existence or content can
// affect selection and mutation of an admitted extension-order sequence.
type OrderAuthorityPath struct {
	path   string
	target target.Target
	scope  target.Scope
}

func (authority OrderAuthorityPath) Path() string          { return authority.path }
func (authority OrderAuthorityPath) Target() target.Target { return authority.target }
func (authority OrderAuthorityPath) Scope() target.Scope   { return authority.scope }

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
	pi       *observepipackage.OrderObservation
	openCode *observeopencode.OrderObservation
}

// Physical returns defensive copies in canonical physical-sequence order.
func (observation OrderObservation) Physical() []PhysicalOrderObservation {
	return append([]PhysicalOrderObservation(nil), observation.physical...)
}

// Pi returns the host-native Pi observation when Pi owns this order class.
func (observation OrderObservation) Pi() (observepipackage.OrderObservation, bool) {
	if observation.pi == nil {
		return observepipackage.OrderObservation{}, false
	}
	return *observation.pi, true
}

// OpenCode returns the host-native OpenCode observation when OpenCode owns this
// order class.
func (observation OrderObservation) OpenCode() (observeopencode.OrderObservation, bool) {
	if observation.openCode == nil {
		return observeopencode.OrderObservation{}, false
	}
	return *observation.openCode, true
}

// OrderAuthorityPaths returns the complete static path set whose selection may
// change before post-carrier reobservation.
func OrderAuthorityPaths(input OrderInput) ([]OrderAuthorityPath, error) {
	if err := input.Constraint.Validate(); err != nil {
		return nil, fmt.Errorf("extension order constraint: %w", err)
	}
	selectedTarget, capability, admitted := profile.ExtensionOrderCapabilityForClass(
		input.Constraint.ClassID(),
	)
	if !admitted {
		return nil, fmt.Errorf(
			"locked extension order class %q has no unique profile owner",
			input.Constraint.ClassID(),
		)
	}

	var paths []string
	switch selectedTarget {
	case target.TargetPi:
		path, err := observepipackage.SettingsPath(observepipackage.SettingsInput{
			WorkDir:     input.Paths.ManifestRoot,
			ProjectRoot: input.Paths.ManifestRoot,
			Scope:       capability.Scope(),
		})
		if err != nil {
			return nil, err
		}
		paths = []string{path}
	case target.TargetOpenCode:
		var err error
		paths, err = observeopencode.OrderAuthorityPaths(observeopencode.InventoryInput{
			ManifestRoot: input.Paths.ManifestRoot,
			Scope:        capability.Scope(),
		})
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf(
			"extension order class %q resolved to unsupported target %q",
			input.Constraint.ClassID(),
			selectedTarget,
		)
	}

	result := make([]OrderAuthorityPath, 0, len(paths))
	for _, path := range paths {
		result = append(result, OrderAuthorityPath{
			path: path, target: selectedTarget, scope: capability.Scope(),
		})
	}
	return result, nil
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
	var piObservation *observepipackage.OrderObservation
	var openCodeObservation *observeopencode.OrderObservation
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
		piObservation = &observation
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
		openCodeObservation = &observation
	default:
		return OrderObservation{}, fmt.Errorf(
			"extension order class %q resolved to unsupported target %q",
			input.Constraint.ClassID(),
			selectedTarget,
		)
	}
	return newOrderObservation(
		capability,
		physical,
		piObservation,
		openCodeObservation,
	)
}

func newOrderObservation(
	capability profile.ExtensionOrderCapability,
	values []PhysicalOrderObservation,
	piObservation *observepipackage.OrderObservation,
	openCodeObservation *observeopencode.OrderObservation,
) (OrderObservation, error) {
	expectedIDs := capability.PhysicalSequenceIDs()
	switch capability.SequenceMembership() {
	case profile.CompleteClassMembership:
		if len(values) != len(expectedIDs) {
			return OrderObservation{}, fmt.Errorf(
				"extension order class %q observed %d physical sequences, want %d",
				capability.ClassID(),
				len(values),
				len(expectedIDs),
			)
		}
	case profile.LoadedClassSubset:
		if len(values) == 0 || len(values) > len(expectedIDs) {
			return OrderObservation{}, fmt.Errorf(
				"extension order class %q observed %d physical sequences outside candidate bound %d",
				capability.ClassID(),
				len(values),
				len(expectedIDs),
			)
		}
	default:
		return OrderObservation{}, fmt.Errorf(
			"extension order class %q has unsupported sequence membership contract %q",
			capability.ClassID(),
			capability.SequenceMembership(),
		)
	}
	physical := append([]PhysicalOrderObservation(nil), values...)
	slices.SortFunc(physical, func(left, right PhysicalOrderObservation) int {
		return cmp.Compare(left.sequence.SequenceID(), right.sequence.SequenceID())
	})
	allowedIDs := make(map[hostrelation.PhysicalSequenceID]struct{}, len(expectedIDs))
	for _, sequenceID := range expectedIDs {
		allowedIDs[sequenceID] = struct{}{}
	}
	for index, observation := range physical {
		if observation.sequence.ClassID() != capability.ClassID() {
			return OrderObservation{}, fmt.Errorf(
				"extension order sequence %q class %q does not match %q",
				observation.sequence.SequenceID(),
				observation.sequence.ClassID(),
				capability.ClassID(),
			)
		}
		if _, admitted := allowedIDs[observation.sequence.SequenceID()]; !admitted {
			return OrderObservation{}, fmt.Errorf(
				"extension order sequence[%d] %q is not an admitted physical candidate",
				index,
				observation.sequence.SequenceID(),
			)
		}
		if index != 0 &&
			physical[index-1].sequence.SequenceID() == observation.sequence.SequenceID() {
			return OrderObservation{}, fmt.Errorf(
				"extension order sequence %q appears more than once",
				observation.sequence.SequenceID(),
			)
		}
	}
	if capability.SequenceMembership() == profile.CompleteClassMembership {
		for index, observation := range physical {
			if observation.sequence.SequenceID() != expectedIDs[index] {
				return OrderObservation{}, fmt.Errorf(
					"extension order sequence[%d] is %q, want %q",
					index,
					observation.sequence.SequenceID(),
					expectedIDs[index],
				)
			}
		}
	}
	if (piObservation == nil) == (openCodeObservation == nil) {
		return OrderObservation{}, fmt.Errorf(
			"extension order class %q requires exactly one host-native observation",
			capability.ClassID(),
		)
	}
	return OrderObservation{
		physical: physical,
		pi:       piObservation,
		openCode: openCodeObservation,
	}, nil
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
