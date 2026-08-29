package archguard

import "testing"

func assertViolationRule(t *testing.T, violations []GuardrailFinding, rule string) {
	t.Helper()
	if countViolationRule(violations, rule) == 0 {
		t.Fatalf("violations = %+v, want rule %q", violations, rule)
	}
}

func countViolationRule(violations []GuardrailFinding, rule string) int {
	count := 0
	for _, violation := range violations {
		if violation.Rule == rule {
			count++
		}
	}
	return count
}
