package ownership

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/executionidentity"
)

type protectionTransitionKind uint8

const (
	protectionArm protectionTransitionKind = iota + 1
	protectionBind
	protectionClear
)

// ProtectionTransition is the closed arm/bind/clear algebra. It cannot replace
// an armed scope, change a ceiling or transfer a pending obligation's owner.
type ProtectionTransition struct {
	kind     protectionTransitionKind
	expected PendingProtection
	target   PendingProtection
}

func NewArmProtection(protection PendingProtection) (ProtectionTransition, error) {
	if err := protection.Validate(); err != nil {
		return ProtectionTransition{}, err
	}
	if protection.Phase() != ProtectionArmed {
		return ProtectionTransition{}, fmt.Errorf("only an armed protection can be published initially")
	}
	return ProtectionTransition{kind: protectionArm, target: protection}, nil
}

func NewBindProtection(armed PendingProtection, execution executionidentity.Binding) (ProtectionTransition, error) {
	bound, err := armed.Bind(execution)
	if err != nil {
		return ProtectionTransition{}, err
	}
	return ProtectionTransition{kind: protectionBind, expected: armed, target: bound}, nil
}

// NewClearProtection describes exact metadata settlement. The effect/recovery
// owner must separately qualify quiescence and every target before publication.
func NewClearProtection(expected PendingProtection) (ProtectionTransition, error) {
	if err := expected.Validate(); err != nil {
		return ProtectionTransition{}, err
	}
	return ProtectionTransition{kind: protectionClear, expected: expected}, nil
}

func (transition ProtectionTransition) Validate() error {
	var err error
	switch transition.kind {
	case protectionArm:
		_, err = NewArmProtection(transition.target)
	case protectionBind:
		execution, present := transition.target.Execution()
		if !present {
			return fmt.Errorf("protection binding transition requires execution identity")
		}
		reconstructed, bindErr := NewBindProtection(transition.expected, execution)
		if bindErr != nil {
			return bindErr
		}
		if !reconstructed.target.Equal(transition.target) {
			return fmt.Errorf("protection binding changes the armed obligation")
		}
	case protectionClear:
		_, err = NewClearProtection(transition.expected)
	default:
		return fmt.Errorf("protection transition is required")
	}
	return err
}

func (transition ProtectionTransition) record() PendingProtection {
	if transition.kind == protectionArm {
		return transition.target
	}
	return transition.expected
}

// ApplyProtection retains unrelated families and permits exact idempotent
// retries. The owner argument does not substitute for live effect authority.
func (registry Registry) ApplyProtection(owner stateauthority.Authority, transition ProtectionTransition) (Registry, bool, error) {
	if !registry.initialized {
		return Registry{}, false, fmt.Errorf("ownership registry is required")
	}
	if err := owner.Validate(); err != nil {
		return Registry{}, false, err
	}
	if err := transition.Validate(); err != nil {
		return Registry{}, false, err
	}
	record := transition.record()
	if !record.owner.Equal(owner) {
		return Registry{}, false, fmt.Errorf("protection transition belongs to another state authority")
	}
	actual, present := registry.Protection(record.invocation)
	if present && !actual.owner.Equal(owner) {
		return Registry{}, false, fmt.Errorf("protection invocation belongs to another state authority")
	}
	if transition.kind == protectionClear && !present {
		return registry, false, nil
	}
	if transition.kind != protectionClear && present && actual.Equal(transition.target) {
		return registry, false, nil
	}
	if transition.kind == protectionArm {
		if present {
			return Registry{}, false, fmt.Errorf("protection invocation is already armed with different facts")
		}
		for _, document := range record.documents {
			if err := registry.RequireWritable(document.PathAuthority()); err != nil {
				return Registry{}, false, err
			}
			var current []DocumentBinding
			for _, binding := range registry.bindings {
				if binding.PathAuthority().Key() == document.PathAuthority().Key() {
					current = append(current, binding)
				}
			}
			if !slices.EqualFunc(current, document.bindings, DocumentBinding.Equal) {
				return Registry{}, false, fmt.Errorf("protection binding cohort changed before arming")
			}
		}
	} else if !present || !actual.Equal(transition.expected) {
		return Registry{}, false, fmt.Errorf("pending protection changed since observation")
	}

	protections := make([]PendingProtection, 0, len(registry.protections)+1)
	for _, protection := range registry.protections {
		if protection.invocation != record.invocation {
			protections = append(protections, protection)
		}
	}
	if transition.kind != protectionClear {
		protections = append(protections, transition.target)
	}
	next, err := NewRegistryWithProtections(registry.claims, registry.bindings, protections)
	if err != nil {
		return Registry{}, false, err
	}
	return next, true, nil
}
