package archguard

import "testing"

func TestConfigurationInvocationCompositionIsExact(t *testing.T) {
	for _, edge := range []struct{ from, to string }{
		{"internal/workflow/apply", "internal/workflow/nativeconfig/invocation"},
		{"internal/workflow/refresh", "internal/workflow/nativeconfig/invocation"},
		{"internal/workflow/nativeconfig/invocation", "internal/workflow/nativeconfig"},
		{"internal/workflow/nativeconfig/invocation", "internal/workflow/protection/invocation"},
	} {
		if violations := analyzeImports(edge.from, []string{"example.com/project/" + edge.to}); len(violations) != 0 {
			t.Fatalf("admitted configuration invocation edge %+v: %+v", edge, violations)
		}
	}
	for _, edge := range []struct{ from, to string }{
		{"internal/workflow/nativeconfig", "internal/workflow/nativeconfig/invocation"},
		{"internal/workflow/nativeconfig/invocation", "internal/workflow/apply"},
		{"internal/workflow/nativeconfig/invocation", "internal/workflow/refresh"},
		{"internal/workflow/recover", "internal/workflow/nativeconfig/invocation"},
		{"internal/workflow/refresh/other", "internal/workflow/nativeconfig/invocation"},
		{"internal/workflow/apply", "internal/workflow/nativeconfig/invocation/private"},
	} {
		assertViolationRule(t, analyzeImports(edge.from, []string{"example.com/project/" + edge.to}), ruleWorkflowNestedImport)
	}
}
