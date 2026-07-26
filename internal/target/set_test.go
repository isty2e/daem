package target

import (
	"slices"
	"strings"
	"testing"
)

func TestSetPreservesOrderAndOwnsStorage(t *testing.T) {
	input := []Target{TargetPi, TargetCodex}
	set, err := NewSet(input)
	if err != nil {
		t.Fatalf("NewSet returned error: %v", err)
	}
	input[0] = TargetClaudeCode

	first := set.Values()
	if !slices.Equal(first, []Target{TargetPi, TargetCodex}) {
		t.Fatalf("Values = %#v", first)
	}
	first[0] = TargetClaudeCode
	if got := set.Values()[0]; got != TargetPi {
		t.Fatalf("mutating returned values changed set: %q", got)
	}
	if !set.Contains(TargetCodex) || set.Contains(TargetClaudeCode) || set.Len() != 2 {
		t.Fatalf("set queries returned inconsistent results")
	}
}

func TestSetRejectsEmptyDuplicateAndUnknownTargets(t *testing.T) {
	for _, test := range []struct {
		name   string
		values []Target
		want   string
	}{
		{name: "empty", want: "at least one target"},
		{name: "duplicate", values: []Target{TargetCodex, TargetCodex}, want: "duplicate target"},
		{name: "unknown", values: []Target{"future"}, want: "unknown target"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSet(test.values); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewSet error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestCanonicalSetAllowsEmptyAndReturnsSortedUniqueTargets(t *testing.T) {
	empty, err := CanonicalSet(nil)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("CanonicalSet(nil) = %#v, %v", empty, err)
	}

	canonical, err := CanonicalSet([]Target{
		TargetPi,
		TargetCodex,
		TargetClaudeCode,
		TargetPi,
	})
	if err != nil {
		t.Fatalf("CanonicalSet returned error: %v", err)
	}
	want := []Target{TargetClaudeCode, TargetCodex, TargetPi}
	if !slices.Equal(canonical, want) {
		t.Fatalf("CanonicalSet = %#v, want %#v", canonical, want)
	}
}

func TestCanonicalSetRejectsInvalidTargetWithoutPartialResult(t *testing.T) {
	canonical, err := CanonicalSet([]Target{TargetCodex, "future", TargetPi})
	if err == nil || !strings.Contains(err.Error(), `target[1]: unknown target "future"`) {
		t.Fatalf("CanonicalSet error = %v", err)
	}
	if canonical != nil {
		t.Fatalf("CanonicalSet returned partial result %#v", canonical)
	}
}
