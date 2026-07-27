package execute

import (
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	commandhook "github.com/isty2e/daem/internal/realization/aggregate/hook"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestAggregateEventFactsPreserveAggregateEffectAxis(t *testing.T) {
	placement, ok := aggregate.HookPlacementFor(target.TargetCodex, target.ScopeProject)
	if !ok {
		t.Fatal("Codex project Hook placement is missing")
	}
	canonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{
		Event: "Stop", Type: "command", Command: "echo guard",
	})
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := placement.Contribution(canonical)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "hook.project.codex", "guard")
	if err != nil {
		t.Fatal(err)
	}

	facts := aggregateEventFacts(
		7,
		AggregateSubjectEffect{
			kind: AggregateEffectReplace, subject: subject, contract: contribution.Contract(),
		},
	)
	if facts.AggregateKind != AggregateEffectReplace || facts.ManagedPathKind != "" {
		t.Fatalf(
			"aggregate event axes = aggregate %q managed-path %q",
			facts.AggregateKind,
			facts.ManagedPathKind,
		)
	}
	if facts.Subject != subject || facts.Destination.String() != ".codex/hooks.json" {
		t.Fatalf("aggregate event facts = %#v", facts)
	}
}
