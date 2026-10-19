package clipresent

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func TestPrintPlanResultRetainsEveryChangedResource(t *testing.T) {
	for _, change := range []struct {
		name  string
		label string
	}{
		{name: "create", label: "add managed output"},
		{name: "update", label: "update managed output"},
		{name: "remove", label: "remove managed output"},
	} {
		t.Run(change.name, func(t *testing.T) {
			consumers := []target.Target{target.TargetCodex}
			var decisions []reconcile.ManagedPathDecision
			for index := range 5 {
				name := fmt.Sprintf("entry-%d", index)
				fixture := newManagedPathPlanFixture(t, name, name, target.ScopeProject, consumers, "desired")
				input := managedPathPlanInput{
					includeDesired: true, selectedTargets: consumers,
					evidence: []observe.ManagedPathEvidence{fixture.evidence(t, false, "")},
				}
				if change.name != "create" {
					input.states = []durable.ManagedPathState{fixture.state(t, consumers, "previous")}
					input.evidence = []observe.ManagedPathEvidence{fixture.evidence(t, true, "previous")}
				}
				if change.name == "remove" {
					input.includeDesired = false
				}
				decisions = append(decisions, fixture.buildPlan(t, input).ManagedPaths()...)
			}
			plan := mustReconciliationPlan(t, decisions, nil)

			for _, verbose := range []bool{false, true} {
				var output bytes.Buffer
				PrintPlanResultWithOptions(&output, plan, HumanOptions{Verbose: verbose})
				text := output.String()
				if !strings.HasPrefix(text, "output results: 5 output entries\n") {
					t.Fatalf("verbose=%t missing complete count: %s", verbose, text)
				}
				if !verbose && !strings.Contains(text, change.label+": 5\n") {
					t.Fatalf("missing complete action-kind count: %s", text)
				}
				for index := range 5 {
					identity := fmt.Sprintf("resource=\"skill/entry-%d\"", index)
					if count := strings.Count(text, identity); count != 1 {
						t.Fatalf("verbose=%t identity %s occurs %d times, want once: %s", verbose, identity, count, text)
					}
				}
			}
		})
	}
}
