package realization

import (
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestAggregateRealizationAcceptsCanonicalContribution(t *testing.T) {
	contribution, err := aggregate.NewManagedContributionFromLegacy(aggregate.LegacyManagedContributionInput{
		PlacementID:           "codex.project.hooks",
		Target:                target.TargetCodex,
		Scope:                 target.ScopeProject,
		AggregateRoot:         outputtest.Parse(t, ".codex/config.toml"),
		ContentPath:           "/hooks",
		MergeUnit:             "hook-set",
		Cardinality:           aggregate.ContributionSharedSet,
		SiblingRetention:      aggregate.PreserveUnmanagedSiblings,
		SiblingPreservation:   aggregate.PreserveSiblingsSemantic,
		Equivalence:           aggregate.EquivalenceCanonicalSemantic,
		CanonicalContribution: `{"command":"review"}`,
		CodecContractID:       "codex-project-hooks-v1",
		ComparedFields:        []string{"command"},
	})
	if err != nil {
		t.Fatal(err)
	}

	spec, err := NewManagedAggregateContribution(contribution)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := spec.ManagedAggregateContribution()
	if !ok || spec.Validate() != nil || !got.Equal(contribution) {
		t.Fatal("canonical contribution did not survive realization construction")
	}
	if _, err := NewManagedAggregateContribution(aggregate.ManagedContribution{}); err == nil {
		t.Fatal("zero contribution was admitted into the realization union")
	}
}
