package target

import (
	"slices"
	"strings"
	"testing"
)

func TestParseTargetAcceptsAntigravityCLIAndRejectsAggregateOrIDE(t *testing.T) {
	parsed, err := ParseTarget("antigravity-cli")
	if err != nil {
		t.Fatalf("ParseTarget returned error: %v", err)
	}
	if parsed != TargetAntigravityCLI {
		t.Fatalf("ParseTarget = %q, want %q", parsed, TargetAntigravityCLI)
	}

	for _, value := range []string{"antigravity", "antigravity-ide"} {
		t.Run(value, func(t *testing.T) {
			_, err := ParseTarget(value)
			if err == nil {
				t.Fatal("ParseTarget returned nil error")
			}
			if !strings.Contains(err.Error(), "unknown target") {
				t.Fatalf("error = %q, want unknown target diagnostic", err.Error())
			}
		})
	}
}

func TestSupportedTargetsIncludesAntigravityCLIInStableAppendOnlyOrder(t *testing.T) {
	targets := SupportedTargets()
	want := []Target{TargetCodex, TargetClaudeCode, TargetOpenCode, TargetPi, TargetAntigravityCLI}
	if !slices.Equal(targets, want) {
		t.Fatalf("SupportedTargets = %#v, want %#v", targets, want)
	}

	targets[0] = TargetAntigravityCLI
	if SupportedTargets()[0] != TargetCodex {
		t.Fatal("SupportedTargets did not return a defensive copy")
	}
}

func TestParseScopeReportsCanonicalAcceptedValues(t *testing.T) {
	for _, scope := range []Scope{ScopeGlobal, ScopeProject} {
		parsed, err := ParseScope(string(scope))
		if err != nil {
			t.Fatalf("ParseScope(%q) returned error: %v", scope, err)
		}
		if parsed != scope {
			t.Fatalf("ParseScope(%q) = %q", scope, parsed)
		}
	}

	_, err := ParseScope("workspace")
	if err == nil {
		t.Fatal("ParseScope returned nil error")
	}
	want := `unknown scope "workspace" (accepted scopes: global, project)`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}
