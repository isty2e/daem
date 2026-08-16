package credentialtext

import "testing"

func TestCredentialRecognitionUsesIdentifierBoundaries(t *testing.T) {
	tests := []struct {
		value      string
		credential bool
		assignment bool
	}{
		{value: "npm:tool@token:actual-secret", credential: true},
		{value: `plugins\client-secret=actual-secret`, credential: true, assignment: true},
		{value: "private_token=secret", credential: true, assignment: true},
		{value: "authorization_code=secret", credential: true, assignment: true},
		{value: "authCode=secret", credential: true, assignment: true},
		{value: `{"token":"secret"}`, credential: true},
		{value: "token_count=2", assignment: true},
		{value: "mytoken=value", assignment: true},
		{value: "package@1.2.3"},
	}
	for _, test := range tests {
		if got := ContainsCredential(test.value); got != test.credential {
			t.Errorf("ContainsCredential(%q) = %t, want %t", test.value, got, test.credential)
		}
		if got := ContainsAssignment(test.value); got != test.assignment {
			t.Errorf("ContainsAssignment(%q) = %t, want %t", test.value, got, test.assignment)
		}
	}
}

func TestRedactPreservesNonCredentialEvidence(t *testing.T) {
	value := `status=failed npm:tool@token:actual-secret plugins\client-secret="quoted value"`
	got, redacted := Redact(value, "[REDACTED]")
	want := `status=failed npm:tool@token:[REDACTED] plugins\client-secret=[REDACTED]`
	if !redacted || got != want {
		t.Fatalf("Redact() = %q/%t, want %q/true", got, redacted, want)
	}
}
