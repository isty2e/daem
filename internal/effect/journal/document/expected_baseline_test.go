package document

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	commandhook "github.com/isty2e/daem/internal/realization/aggregate/hook"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestExpectedBaselineRequiresTheExactOrderedHookSet(t *testing.T) {
	placement, ok := aggregate.HookPlacementFor(target.TargetClaudeCode, target.ScopeProject)
	if !ok {
		t.Fatal("missing Claude hook placement")
	}
	row := func(name, command string) durable.ManagedAggregateState {
		t.Helper()
		canonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{Event: "Stop", Type: "command", Command: command})
		if err != nil {
			t.Fatal(err)
		}
		body, err := placement.Contribution(canonical)
		if err != nil {
			t.Fatal(err)
		}
		subject, err := topology.NewSubjectID(topology.SubjectProjection, string(placement.ID()), "hook:"+name)
		if err != nil {
			t.Fatal(err)
		}
		record, err := durable.NewManagedAggregateState(subject, body)
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	const first = `{"hooks":[{"type":"command","command":"first"}]}`
	const second = `{"hooks":[{"type":"command","command":"second"}]}`
	a, b := row("a", "first"), row("b", "second")
	before, after := selectionState(t, b), selectionState(t, b, a)
	index := selectionIndexes(t, before, after)
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{a.Contribution().Contract()})
	if err != nil {
		t.Fatal(err)
	}
	owner := documentClaimsForTest(t).owner
	for _, tc := range []struct {
		name   string
		groups []string
		valid  bool
	}{
		{"unchanged sibling included", []string{first, second}, true},
		{"sibling missing", []string{first}, false},
		{"order changed", []string{second, first}, false},
		{"extra duplicate", []string{first, second, second}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			comparison := carrierComparison(t, selection, `{"hooks":{"Stop":[`+second+`]}}`, `{"outside":true,"hooks":{"Stop":[`+strings.Join(tc.groups, ",")+`]}}`)
			input := NewStateGroupInput([]aggregate.DocumentAddress{selection.DocumentAddress()}, []topology.SubjectID{a.Subject()}, comparison, nil)
			groups, err := ReconstructStateGroups(t.Context(), owner, before, after, []StateGroupInput{input})
			if err != nil {
				t.Fatal(err)
			}
			if err := index.ValidateExpectedBaselines(t.Context(), groups, aggregatecodec.Catalog(), nil); (err == nil) != tc.valid {
				t.Fatalf("expected image accepted = %t, want %t: %v", err == nil, tc.valid, err)
			}
		})
	}
}
