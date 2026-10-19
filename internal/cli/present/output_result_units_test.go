package clipresent

import (
	"bytes"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/target"
)

func TestSharedManagedOutputUsesOnePlanAndResultEntry(t *testing.T) {
	consumers := []target.Target{target.TargetCodex, target.TargetAntigravityCLI}
	fixture := newManagedPathPlanFixture(t, "shared", "shared", target.ScopeProject, consumers, "content")
	plan := fixture.buildPlan(t, managedPathPlanInput{
		includeDesired: true, selectedTargets: consumers,
		evidence: []observe.ManagedPathEvidence{fixture.evidence(t, false, "")},
	})
	for _, options := range []HumanOptions{{}, {Verbose: true}} {
		var preview, result bytes.Buffer
		PrintDryRunPlanWithOptions(&preview, HumanPlanInput{Reconciliation: plan}, options)
		PrintPlanResultWithOptions(&result, plan, options)
		if !strings.HasPrefix(preview.String(), "dry-run: 1 output entry (current=0 pending=1 blocked=0)\n") ||
			!strings.HasPrefix(result.String(), "output results: 1 output entry\n") {
			t.Fatalf("shared output counted its consumers: preview=%s result=%s", preview.String(), result.String())
		}
		if !strings.Contains(result.String(), "targets=") || !strings.Contains(result.String(), "codex") || !strings.Contains(result.String(), "antigravity-cli") {
			t.Fatalf("shared result lost a consumer: %s", result.String())
		}
	}
}
