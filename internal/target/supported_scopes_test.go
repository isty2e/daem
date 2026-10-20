package target

import "testing"

func TestSupportedScopesReturnsIndependentAcceptedValues(t *testing.T) {
	first := SupportedScopes()
	if len(first) == 0 {
		t.Fatal("scope inventory is empty")
	}
	for _, scope := range first {
		parsed, err := ParseScope(string(scope))
		if err != nil || parsed != scope {
			t.Fatalf("inventory disagrees with scope admission: %q, %v", scope, err)
		}
	}
	original := first[0]
	first[0] = Scope("invalid")
	if second := SupportedScopes(); second[0] != original {
		t.Fatal("caller changed the canonical scope inventory")
	}
	if parsed, err := ParseScope(string(original)); err != nil || parsed != original {
		t.Fatal("inventory mutation changed parser admission")
	}
}
