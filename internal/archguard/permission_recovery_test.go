package archguard

import "testing"

func TestProtectionCompositionAndProjectionAreExact(t *testing.T) {
	for _, edge := range []struct{ from, to string }{
		{"internal/workflow/recover", "internal/workflow/protection/recovery"},
		{"internal/cli/present", "internal/workflow/protection/recovery"},
		{"internal/workflow/apply", "internal/workflow/protection/invocation"},
		{"internal/workflow/refresh", "internal/workflow/protection/invocation"},
		{"internal/workflow/nativeconfig", "internal/workflow/protection/invocation"},
	} {
		if violations := analyzeImports(edge.from, []string{"example.com/project/" + edge.to}); len(violations) != 0 {
			t.Fatalf("admitted protection edge %+v: %+v", edge, violations)
		}
	}
	for _, edge := range []struct{ from, to string }{
		{"internal/workflow/status", "internal/workflow/protection/recovery"},
		{"internal/workflow/protection/invocation", "internal/workflow/recover"},
		{"internal/workflow/protection/recovery", "internal/workflow/apply"},
		{"internal/workflow/protection/invocation", "internal/workflow/protection/recovery"},
		{"internal/workflow/protection/recovery", "internal/workflow/protection/invocation"},
		{"internal/workflow/recover/other", "internal/workflow/protection/recovery"},
		{"internal/workflow/apply", "internal/workflow/protection/recovery"},
		{"internal/workflow/recover", "internal/workflow/protection/invocation"},
		{"internal/workflow/recover", "internal/workflow/protection/private"},
		{"internal/workflow/recover", "internal/workflow/recover/permissions"},
	} {
		assertViolationRule(t, analyzeImports(edge.from, []string{"example.com/project/" + edge.to}), ruleWorkflowNestedImport)
	}
	assertViolationRule(t, analyzeImports("internal/workflow/protection/invocation", []string{"example.com/project/internal/cli/present"}), ruleWorkflowPresentImport)
}
