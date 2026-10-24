package projection

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

var ErrNativeNarrowingNeedsCapture = errors.New("capture current retained values into source before narrowing")

// BuildNativeNarrowingPlan compares a bound current document read with recorded
// ownership; it does not accept a caller-asserted unchanged result.
func BuildNativeNarrowingPlan(observation aggregate.DocumentObservation, desired []aggregate.ContributionSet, baseline durable.ManagedPathState) (aggregate.Plan, error) {
	if err := observation.Validate(); err != nil {
		return aggregate.Plan{}, err
	}
	current := observation.Snapshot()
	selection, err := current.Selection()
	if err != nil {
		return aggregate.Plan{}, err
	}
	if _, native := aggregate.HostConfigSyntaxForCodec(selection.CodecContractID()); !native ||
		len(selection.Contracts()) != len(desired) {
		return aggregate.Plan{}, fmt.Errorf("native narrowing requires an existing document and exact correlated selection")
	}
	if err := baseline.ContentHash().Validate(); err != nil {
		return aggregate.Plan{}, err
	}
	document := selection.DocumentAddress()
	targets := baseline.ConsumerTargets()
	owner, entityBacked := topologyprojection.EntityID(baseline.Subject())
	if !entityBacked || baseline.ContentKind() != realization.PathProjectionFile || baseline.FileMode()&0o111 != 0 ||
		len(targets) != 1 || targets[0] != document.Target() || baseline.Scope() != document.Scope() ||
		baseline.Destination() != document.AggregateRoot() {
		return aggregate.Plan{}, fmt.Errorf("native narrowing baseline differs from observed document")
	}
	expectedSubject, err := aggregate.HostConfigDocumentSubject(owner, document.Target(), document.Scope())
	if err != nil || expectedSubject != baseline.Subject() {
		return aggregate.Plan{}, fmt.Errorf("native narrowing baseline has a different placement identity")
	}
	sets := make(map[aggregate.ProjectionAddress]aggregate.ContributionSet, len(desired))
	for _, set := range desired {
		canonical, err := aggregate.NewContributionSet(set.Contributions())
		if err != nil {
			return aggregate.Plan{}, err
		}
		for _, item := range canonical.Contributions() {
			id, _, native := topologyprojection.HostConfigKeyIdentity(item.SubjectID())
			if !native || id != owner {
				return aggregate.Plan{}, fmt.Errorf("native narrowing changes the managed declaration")
			}
		}
		address := canonical.Address()
		if _, duplicate := sets[address]; duplicate {
			return aggregate.Plan{}, fmt.Errorf("native narrowing repeats a desired key")
		}
		sets[address] = canonical
	}
	documentUnchanged := observation.ContentHash() == baseline.ContentHash()
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
