package adopt

import (
	"testing"

	targetpkg "github.com/isty2e/daem/internal/target"
)

func TestSupportedTargetsIsDefensiveAndComplete(t *testing.T) {
	expected := []targetpkg.Target{
		targetpkg.TargetCodex,
		targetpkg.TargetClaudeCode,
		targetpkg.TargetOpenCode,
		targetpkg.TargetPi,
		targetpkg.TargetAntigravityCLI,
	}
	actual := SupportedTargets()
	if len(actual) != len(expected) {
		t.Fatalf("SupportedTargets() = %v, want %v", actual, expected)
	}
	for index, target := range expected {
		if actual[index] != target || !SupportsTarget(target) {
			t.Fatalf("target[%d] = %q, want supported %q", index, actual[index], target)
		}
	}

	actual[0] = ""
	if SupportedTargets()[0] != expected[0] {
		t.Fatal("SupportedTargets returned shared mutable storage")
	}
}

func TestSupportsTargetRejectsUnknownTarget(t *testing.T) {
	if SupportsTarget(targetpkg.Target("future-agent")) {
		t.Fatal("SupportsTarget admitted an unknown target")
	}
}
