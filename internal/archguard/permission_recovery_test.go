package archguard

import "testing"

func TestProtectionCompositionAndProjectionAreExact(t *testing.T) {
	for _, from := range []string{"internal/workflow/recover", "internal/workflow/apply", "internal/workflow/refresh", "internal/workflow/nativeconfig", "internal/cli/present"} {
		if violations := analyzeImports(from, []string{"example.com/project/internal/workflow/protection"}); len(violations) != 0 {
			t.Fatalf("admitted protection edge %s: %+v", from, violations)
		}
	}
	for _, edge := range []struct{ from, to string }{
		{"internal/workflow/status", "internal/workflow/protection"},
		{"internal/workflow/protection", "internal/workflow/recover"},
		{"internal/workflow/protection", "internal/workflow/apply"},
		{"internal/workflow/protection", "internal/workflow/refresh"},
		{"internal/workflow/recover/other", "internal/workflow/protection"},
		{"internal/workflow/recover", "internal/workflow/protection/private"},
		{"internal/workflow/recover", "internal/workflow/recover/permissions"},
	} {
		assertViolationRule(t, analyzeImports(edge.from, []string{"example.com/project/" + edge.to}), ruleWorkflowNestedImport)
	}
	assertViolationRule(t, analyzeImports("internal/workflow/protection", []string{"example.com/project/internal/cli/present"}), ruleWorkflowPresentImport)
}
