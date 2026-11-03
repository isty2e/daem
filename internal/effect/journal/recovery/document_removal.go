package recovery

import "fmt"

// DocumentRemoval retains all logical removal relations for one physical
// document and one cleanup namespace. It grants no filesystem capability.
type DocumentRemoval struct {
	demands   RemovalDemandSet
	namespace RemovalNamespaceAuthority
}

// NewDocumentRemoval admits one namespace only when every logical relation
// permits the same complete regular-file states.
func NewDocumentRemoval(demands RemovalDemandSet, namespace RemovalNamespaceAuthority) (DocumentRemoval, error) {
	if err := demands.Validate(); err != nil {
		return DocumentRemoval{}, err
	}
	relations := demands.Demands()
	if len(relations) == 0 {
		return DocumentRemoval{}, fmt.Errorf("document removal requires logical demands")
	}
	if err := namespace.validate(); err != nil {
		return DocumentRemoval{}, err
	}
	states := relations[0].States()
	for _, state := range states {
		if before, present := state.Before(); present && (!before.Existed || before.Kind != PathKindFile) {
			return DocumentRemoval{}, fmt.Errorf("document removal requires an existing regular-file state")
		}
		if expected, present := state.Expected(); present && (!expected.Existed || expected.Kind != PathKindFile) {
			return DocumentRemoval{}, fmt.Errorf("document removal requires an existing regular-file state")
		}
	}
	for _, demand := range relations {
		other := demand.States()
		if len(states) != len(other) {
			return DocumentRemoval{}, fmt.Errorf("document removal relations disagree on physical states")
		}
		for index, state := range states {
			if !state.Equal(other[index]) {
				return DocumentRemoval{}, fmt.Errorf("document removal relations disagree on physical states")
			}
		}
		if _, err := NewRemovalIntent(demand, namespace); err != nil {
			return DocumentRemoval{}, err
		}
	}
	return DocumentRemoval{demands: demands, namespace: namespace}, nil
}

func (removal DocumentRemoval) Demands() RemovalDemandSet { return removal.demands }

func (removal DocumentRemoval) Namespace() RemovalNamespaceAuthority { return removal.namespace }

// PhysicalIntent selects one actual relation for the single physical cleanup.
// Coverage must still use Demands, not discard the other logical relations.
func (removal DocumentRemoval) PhysicalIntent() (RemovalIntent, error) {
	demands := removal.demands.Demands()
	if len(demands) == 0 {
		return RemovalIntent{}, fmt.Errorf("document removal requires logical demands")
	}
	return NewRemovalIntent(demands[0], removal.namespace)
}
