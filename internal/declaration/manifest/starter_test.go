package manifest

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestStarterContentIsCanonicalAndDefensivelyDisclosed(t *testing.T) {
	first := StarterContent()
	if string(first) != "version = 1\ntargets = [\"codex\"]\n" {
		t.Fatalf("StarterContent = %q", first)
	}
	environment, err := Decode(first)
	if err != nil {
		t.Fatalf("Decode(StarterContent) returned error: %v", err)
	}
	if targets := environment.Targets(); !slices.Equal(targets, []target.Target{target.TargetCodex}) {
		t.Fatalf("starter targets = %#v, want codex", targets)
	}

	first[0] = 'X'
	if got := string(StarterContent()); got != "version = 1\ntargets = [\"codex\"]\n" {
		t.Fatalf("StarterContent aliases caller mutation: %q", got)
	}
}
