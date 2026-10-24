package projection

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

var ErrNativeNarrowingNeedsCapture = errors.New("capture current retained values into source before narrowing")

// BuildNativeNarrowingPlan compares only retained keys. Its operation adapter
// owns whole-file hash equality and native syntax/value admission before this call.
func BuildNativeNarrowingPlan(current aggregate.Snapshot, desired []aggregate.ContributionSet, documentUnchanged bool) (aggregate.Plan, error) {
	selection, err := current.Selection()
	if err != nil {
		return aggregate.Plan{}, err
	}
	if _, native := aggregate.HostConfigSyntaxForCodec(selection.CodecContractID()); !native ||
		!current.DocumentExisted() || len(selection.Contracts()) != len(desired) {
		return aggregate.Plan{}, fmt.Errorf("native narrowing requires an existing document and exact correlated selection")
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
	intents := make([]aggregate.ProjectionIntent, 0, len(desired))
	for _, state := range current.States() {
		address := state.Contract().Address()
		set, hasDesired := sets[address]
		if !hasDesired || !set.Contract().Equal(state.Contract()) {
			return aggregate.Plan{}, fmt.Errorf("native narrowing key lacks exact current/desired correlation")
		}
		if _, blocked := state.BlockedBy(); blocked {
			return aggregate.Plan{}, fmt.Errorf("retained native key has blocked traversal at %q", address.KeyPath().String())
		}
		canonicalDesired := set.Contributions()[0].Contribution().CanonicalContribution()
		matchesDesired := state.Present() && state.CanonicalProjection() == canonicalDesired
		if !documentUnchanged && !matchesDesired {
			return aggregate.Plan{}, fmt.Errorf("%w at %q", ErrNativeNarrowingNeedsCapture, address.KeyPath().String())
		}
		intent, err := aggregate.NewProjectionIntent(state, &set)
		if err != nil {
			return aggregate.Plan{}, err
		}
		intents = append(intents, intent)
	}
	return aggregate.NewPlan(current, intents)
}
