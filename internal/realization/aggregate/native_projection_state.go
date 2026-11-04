package aggregate

import (
	"fmt"

	"github.com/isty2e/daem/internal/output"
)

// NewBlockedProjectionState records a native traversal obstruction. A Snapshot
// must also contain the present selected ancestor; neither fact grants ownership.
// Parent presence describes the native document root, not intermediate traversal.
func NewBlockedProjectionState(contract ProjectionContract, ancestor output.KeyPath) (ProjectionState, error) {
	state := ProjectionState{contract: cloneProjectionContract(contract), parentPresent: true, blockingAncestor: ancestor}
	if ancestor.IsRoot() {
		return ProjectionState{}, fmt.Errorf("blocked native projection requires an ancestor key")
	}
	if err := state.Validate(); err != nil {
		return ProjectionState{}, err
	}
	return state, nil
}

func (state ProjectionState) BlockedBy() (output.KeyPath, bool) {
	return state.blockingAncestor, !state.blockingAncestor.IsRoot()
}

func (state ProjectionState) Absent() bool {
	return !state.present && state.blockingAncestor.IsRoot()
}

func (state ProjectionState) Equal(other ProjectionState) bool {
	return state.parentPresent == other.parentPresent && state.SelectedValueEqual(other)
}

// SelectedValueEqual compares the selected facts without parent presence.
func (state ProjectionState) SelectedValueEqual(other ProjectionState) bool {
	return state.contract.Equal(other.contract) && state.present == other.present &&
		state.canonicalProjection == other.canonicalProjection && state.blockingAncestor == other.blockingAncestor
}

func (snapshot Snapshot) validateBlockingAncestors() error {
	blocked := false
	for _, state := range snapshot.states {
		if !state.blockingAncestor.IsRoot() {
			blocked = true
			break
		}
	}
	if !blocked {
		return nil
	}

	byKey := make(map[output.KeyPath]ProjectionState, len(snapshot.states))
	for _, state := range snapshot.states {
		byKey[state.contract.address.KeyPath()] = state
	}
	for _, state := range snapshot.states {
		if state.blockingAncestor.IsRoot() {
			continue
		}
		ancestor, selected := byKey[state.blockingAncestor]
		if !selected || !ancestor.present {
			return fmt.Errorf("blocked native projection lacks its present selected ancestor")
		}
	}
	return nil
}
