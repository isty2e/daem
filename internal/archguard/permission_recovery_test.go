package archguard

import "testing"

func TestPermissionRecoveryCompositionAndProjectionAreExact(t *testing.T) {
	for _, from := range []string{"internal/workflow/recover", "internal/cli/present"} {
		if violations := analyzeImports(from, []string{"example.com/project/internal/workflow/recover/permissions"}); len(violations) != 0 {
			t.Fatalf("admitted permission recovery edge %s: %+v", from, violations)
		}
	}
	for _, edge := range []struct{ from, to string }{
		{"internal/workflow/apply", "internal/workflow/recover/permissions"},
		{"internal/workflow/recover/permissions", "internal/workflow/recover"},
		{"internal/workflow/recover/other", "internal/workflow/recover/permissions"},
		{"internal/workflow/recover", "internal/workflow/recover/permissions/private"},
	} {
		assertViolationRule(t, analyzeImports(edge.from, []string{"example.com/project/" + edge.to}), ruleWorkflowNestedImport)
	}
	assertViolationRule(t, analyzeImports("internal/workflow/recover/permissions", []string{"example.com/project/internal/cli/present"}), ruleWorkflowPresentImport)
}
