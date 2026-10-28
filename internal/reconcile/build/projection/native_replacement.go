package projection

import (
	"fmt"

	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// BuildNativeReplacementDecisions plans explicit selected-key replacement.
// It does not approve the plan or relax namespace, ownership or claim checks.
func BuildNativeReplacementDecisions(input AggregateInput, selection topologyprojection.HostConfigKeySelection) ([]reconcile.AggregateDecision, error) {
	if err := selection.Validate(); err != nil {
		return nil, err
	}
	if len(input.Expected) == 0 || len(input.Expected) != len(input.Desired) {
		return nil, fmt.Errorf("replacement requires a nonempty, exactly covered desired key set")
	}
	selected := make(map[topology.SubjectID]struct{}, len(input.Expected))
	for _, contract := range input.Expected {
		id, key, native := topologyprojection.HostConfigKeyIdentity(contract.SubjectID())
		if !native || id != selection.Declaration() || !selection.Includes(key) {
			return nil, fmt.Errorf("replacement contains a subject outside its selected native declaration")
		}
		selected[contract.SubjectID()] = struct{}{}
	}
	for _, desired := range input.Desired {
		if _, present := selected[desired.SubjectID()]; !present {
			return nil, fmt.Errorf("replacement desired key is not selected")
		}
	}
	for _, previous := range input.States {
		if _, present := selected[previous.Subject()]; !present {
			return nil, fmt.Errorf("replacement cannot retire unselected ownership")
		}
	}
	var locked []lock.LockedSubjectContract
	for subject := range selected {
		if contract, present := input.Locked.Subject(subject); present {
			locked = append(locked, contract)
		}
	}
	for _, contract := range input.Locked.Subjects() {
		if _, supplied := contract.ExactSupply(); supplied && contract.EntityID() == selection.Declaration() {
			locked = append(locked, contract)
		}
	}
	section, err := lock.NewLockedSection(locked, nil)
	if err != nil {
		return nil, err
	}
	input.Locked = section
	return buildAggregateDecisions(input, &selection)
}
