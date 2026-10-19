package clipresent

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func TestHumanResourceOrderIsStableCopiedAndCatalogRanked(t *testing.T) {
	type item struct {
		name  string
		order humanResourceOrder
	}
	supported := target.SupportedTargets()
	values := []item{{name: "opaque first"}, {name: "opaque second"}}
	want := []string{"earlier resource"}
	for _, selected := range supported {
		values = append(values, item{string(selected), humanResourceOrderFor("skill/tools", []target.Target{selected}, target.ScopeProject)})
		want = append(want, string(selected))
	}
	slices.Reverse(values[2:])
	values = append(
		values,
		item{"earlier resource", humanResourceOrderFor("skill/alpha", []target.Target{supported[len(supported)-1]}, target.ScopeProject)},
		item{"global", humanResourceOrderFor("skill/tools", []target.Target{supported[0]}, target.ScopeGlobal)},
		item{"stable peer", humanResourceOrderFor("skill/tools", []target.Target{supported[0]}, target.ScopeProject)},
	)
	want = slices.Insert(want, 1, "global")
	want = slices.Insert(want, 3, "stable peer")
	want = append(want, "opaque first", "opaque second")
	before := slices.Clone(values)
	ordered := sortedHumanRows(values, func(value item) humanResourceOrder { return value.order })
	var got []string
	for _, value := range ordered {
		got = append(got, value.name)
	}
	if !slices.Equal(got, want) || !reflect.DeepEqual(values, before) {
		t.Fatalf("ordered = %v, want %v; input preserved = %t", got, want, reflect.DeepEqual(values, before))
	}

	shared := humanResourceOrderFor("skill/tools", []target.Target{supported[len(supported)-1], supported[0]}, target.ScopeProject)
	first := humanResourceOrderFor("skill/tools", []target.Target{supported[0]}, target.ScopeProject)
	if shared.compare(first) != 0 {
		t.Fatalf("shared consumer order = %#v, want minimum catalog rank %#v", shared, first)
	}
}

func TestHumanPlanAndResultsShareResourceOrderWithoutMutatingCanonicalPlan(t *testing.T) {
	var paths []reconcile.ManagedPathDecision
	for _, spec := range []struct {
		name      string
		consumers []target.Target
		operation string
	}{
		{"zeta", []target.Target{target.TargetCodex}, "create"},
		{"alpha", []target.Target{target.TargetClaudeCode}, "update"},
		{"alpha", []target.Target{target.TargetAntigravityCLI, target.TargetCodex}, "remove"},
	} {
		fixture := newManagedPathPlanFixture(t, spec.name, spec.name, target.ScopeProject, spec.consumers, "desired")
		input := managedPathPlanInput{
			includeDesired:  true,
			selectedTargets: spec.consumers,
			evidence:        []observe.ManagedPathEvidence{fixture.evidence(t, false, "")},
		}
		if spec.operation != "create" {
			input.states = []durable.ManagedPathState{fixture.state(t, spec.consumers, "before")}
			input.evidence = []observe.ManagedPathEvidence{fixture.evidence(t, true, "before")}
			input.includeDesired = spec.operation != "remove"
		}
		paths = append(paths, fixture.buildPlan(t, input).ManagedPaths()...)
	}
	plan := mustReconciliationPlan(t, paths, mcpProjectionPlan(t).Aggregates())
	canonical := plan.Decisions()
	encode := func() []byte {
		t.Helper()
		var output bytes.Buffer
		if err := PrintPlanJSON(&output, PlanJSONInput{Command: "apply", Mode: "dry-run", Reconciliation: plan}); err != nil {
			t.Fatal(err)
		}
		return bytes.Clone(output.Bytes())
	}
	jsonBefore := encode()
	var resultBefore bytes.Buffer
	if err := PrintApplyResultJSON(&resultBefore, ApplyResultJSONInput{Reconciliation: plan}); err != nil {
		t.Fatal(err)
	}

	for _, verbose := range []bool{false, true} {
		var planned, applied bytes.Buffer
		PrintActionPlanWithOptions(&planned, "apply", HumanPlanInput{Reconciliation: plan}, HumanOptions{Verbose: verbose})
		PrintPlanResultWithOptions(&applied, plan, HumanOptions{Verbose: verbose})
		for label, output := range map[string]string{"plan": planned.String(), "result": applied.String()} {
			var rows strings.Builder
			for line := range strings.SplitSeq(output, "\n") {
				if label == "result" || strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
					fmt.Fprintln(&rows, line)
				}
			}
			want := []string{`resource="mcp-server/context7"`, `resource="skill/alpha" targets=antigravity-cli,codex`, `resource="skill/alpha" target=claude-code`, `resource="skill/zeta"`}
			previous := -1
			for _, row := range want {
				position := strings.Index(rows.String(), row)
				if position <= previous || strings.Count(rows.String(), row) != 1 {
					t.Fatalf("%s verbose=%t missing, repeated or out-of-order row %s:\n%s", label, verbose, row, output)
				}
				previous = position
			}
		}
		if !strings.Contains(planned.String(), "4 output entries (current=0 pending=4 blocked=0)") ||
			!strings.Contains(applied.String(), "output results: 4 output entries") {
			t.Fatalf("count units changed:\n%s\n%s", planned.String(), applied.String())
		}
	}
	var resultAfter bytes.Buffer
	if err := PrintApplyResultJSON(&resultAfter, ApplyResultJSONInput{Reconciliation: plan}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(jsonBefore, encode()) || !bytes.Equal(resultBefore.Bytes(), resultAfter.Bytes()) || !reflect.DeepEqual(canonical, plan.Decisions()) {
		t.Fatal("human rendering mutated canonical plan or JSON")
	}
}
