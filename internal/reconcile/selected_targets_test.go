package reconcile

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestSelectedTargetsAllowsEmptyAndOwnsStorage(t *testing.T) {
	empty, err := NewSelectedTargets(nil)
	if err != nil {
		t.Fatalf("NewSelectedTargets empty returned error: %v", err)
	}
	if empty.Len() != 0 || empty.Contains(target.TargetCodex) || len(empty.Values()) != 0 {
		t.Fatalf("empty selected targets = %#v", empty.Values())
	}

	input := []target.Target{target.TargetPi, target.TargetCodex}
	selected, err := NewSelectedTargets(input)
	if err != nil {
		t.Fatalf("NewSelectedTargets returned error: %v", err)
	}
	input[0] = target.TargetClaudeCode

	values := selected.Values()
	if !slices.Equal(values, []target.Target{target.TargetPi, target.TargetCodex}) {
		t.Fatalf("Values = %#v", values)
	}
	values[0] = target.TargetClaudeCode
	if selected.Values()[0] != target.TargetPi {
		t.Fatalf("mutating returned values changed selected targets")
	}
}

func TestSelectedTargetsRejectsDuplicateAndUnknownTargets(t *testing.T) {
	for _, test := range []struct {
		name   string
		values []target.Target
		want   string
	}{
		{name: "duplicate", values: []target.Target{target.TargetCodex, target.TargetCodex}, want: "duplicate target"},
		{name: "unknown", values: []target.Target{"future"}, want: "unknown target"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSelectedTargets(test.values); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewSelectedTargets error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func planSelectedTargets(t *testing.T, values ...target.Target) SelectedTargets {
	t.Helper()
	selected, err := NewSelectedTargets(values)
	if err != nil {
		t.Fatalf("NewSelectedTargets returned error: %v", err)
	}
	return selected
}
