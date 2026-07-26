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
