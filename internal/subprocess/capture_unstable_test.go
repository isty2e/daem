package subprocess

import "testing"

func TestPolicySanitizeWithholdsUnresolvedBoundaryText(t *testing.T) {
	// An incomplete trailing escape leaves the decoded form uninspectable, so
	// the truncation boundary cannot prove a secret prefix is absent and the
	// value fails closed.
	result := NewCapturePolicy([]string{"super-secret-value"}, 1024).Sanitize("runner saw super-secret%2", true)
	if result.Text() != "[REDACTED]" || !result.Redacted() || !result.Truncated() {
		t.Fatalf("result = %q redacted=%t truncated=%t, want full withholding", result.Text(), result.Redacted(), result.Truncated())
	}
}
