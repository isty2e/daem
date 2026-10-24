package projection

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

var ErrNativeNarrowingDrift = errors.New("retained native key differs from baseline and desired value")

// BuildNativeNarrowingPlan compares only retained keys. Its operation adapter
// owns old-byte verification and native syntax/value admission before this call.
func BuildNativeNarrowingPlan(baseline, current aggregate.Snapshot, desired []aggregate.ContributionSet) (aggregate.Plan, error) {
	selection, err := current.Selection()
	if err != nil {
		return aggregate.Plan{}, err
	}
	oldSelection, err := baseline.Selection()
	if err != nil {
		return aggregate.Plan{}, err
	}
	if _, native := aggregate.HostConfigSyntaxForCodec(selection.CodecContractID()); !native ||
		oldSelection.CodecContractID() != selection.CodecContractID() ||
		oldSelection.DocumentAddress() != selection.DocumentAddress() ||
		len(selection.Contracts()) != len(desired) || len(oldSelection.Contracts()) != len(desired) {
		return aggregate.Plan{}, fmt.Errorf("native narrowing requires exact correlated selections")
	}
	sets := make(map[aggregate.ProjectionAddress]aggregate.ContributionSet, len(desired))
	for _, set := range desired {
		canonical, err := aggregate.NewContributionSet(set.Contributions())
		if err != nil {
			return aggregate.Plan{}, err
		}
		address := canonical.Address()
		if _, duplicate := sets[address]; duplicate {
			return aggregate.Plan{}, fmt.Errorf("native narrowing repeats a desired key")
		}
		sets[address] = canonical
	}
	old := make(map[aggregate.ProjectionAddress]aggregate.ProjectionState, len(desired))
	for _, state := range baseline.States() {
		old[state.Contract().Address()] = state
	}
	intents := make([]aggregate.ProjectionIntent, 0, len(desired))
	for _, state := range current.States() {
		address := state.Contract().Address()
		previous, hadBefore := old[address]
		set, hasDesired := sets[address]
		if !hadBefore || !hasDesired || !previous.Contract().Equal(state.Contract()) || !set.Contract().Equal(state.Contract()) {
			return aggregate.Plan{}, fmt.Errorf("native narrowing key lacks exact before/desired correlation")
		}
		if _, blocked := state.BlockedBy(); blocked {
			return aggregate.Plan{}, fmt.Errorf("%w at %q", ErrNativeNarrowingDrift, address.KeyPath().String())
		}
		canonicalDesired := set.Contributions()[0].Contribution().CanonicalContribution()
		matchesDesired := state.Present() && state.CanonicalProjection() == canonicalDesired
		if !state.SelectedValueEqual(previous) && !matchesDesired {
			return aggregate.Plan{}, fmt.Errorf("%w at %q", ErrNativeNarrowingDrift, address.KeyPath().String())
		}
		intent, err := aggregate.NewProjectionIntent(state, &set)
		if err != nil {
			return aggregate.Plan{}, err
		}
		intents = append(intents, intent)
	}
	return aggregate.NewPlan(current, intents)
}
