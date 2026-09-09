package authoring

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestMissingResourceSelectionBoundsAndOrdersCandidates(t *testing.T) {
	err := missingResourceSelection("instruction", "Project", []ResourceSelection{
		{Name: "project", Scope: "global", Targets: []string{"z", "a"}},
		{Name: "project", Scope: "project", Targets: []string{"b"}},
		{Name: "project", Scope: "user", Targets: []string{"c"}},
		{Name: "project", Scope: "extra", Targets: []string{"d"}},
		{Name: "other", Scope: "project"},
	})
	var typed *ResourceSelectionNotFoundError
	if !errors.As(err, &typed) || err.Error() != `instruction resource "Project" not found` {
		t.Fatalf("error = %v, typed = %#v", err, typed)
	}
	got := typed.AvailableSelections()
	if len(got) != 3 || got[0].Scope != "extra" || got[1].Scope != "global" || !reflect.DeepEqual(got[1].Targets, []string{"a", "z"}) {
		t.Fatalf("candidates = %#v", got)
	}
}

func TestMissingResourceSelectionUsesConservativeTypoAndNoMatch(t *testing.T) {
	if got := missingResourceSelection("hook", "protct", []ResourceSelection{{Name: "protect"}}).(*ResourceSelectionNotFoundError).AvailableSelections(); len(got) != 1 {
		t.Fatalf("typo candidates = %#v", got)
	}
	if got := missingResourceSelection("hook", "absent", []ResourceSelection{{Name: "distant"}}).(*ResourceSelectionNotFoundError).AvailableSelections(); len(got) != 0 {
		t.Fatalf("unrelated candidates = %#v", got)
	}
	wrapped := fmt.Errorf("outer: %w", missingResourceSelection("hook", "absent", nil))
	var typed *ResourceSelectionNotFoundError
	if !errors.As(wrapped, &typed) {
		t.Fatal("wrapped error did not retain type")
	}
}

func TestMissingResourceSelectionUsesAliasesWithoutLosingDeclarationKeys(t *testing.T) {
	available := []ResourceSelection{
		{Name: "codex-review", Alias: "review", Scope: "project", Targets: []string{"codex"}},
		{Name: "claude-review", Alias: "review", Scope: "project", Targets: []string{"claude-code"}},
		{Name: "Review", Scope: "global", Targets: []string{"codex"}},
	}
	failure := missingResourceSelection("skill", "review", available).(*ResourceSelectionNotFoundError)
	selections := failure.AvailableSelections()
	if len(selections) != 2 || selections[0].Name != "claude-review" || selections[1].Name != "codex-review" {
		t.Fatalf("alias selections lost exact declaration keys: %#v", selections)
	}
	available[0].Targets[0] = "mutated input"
	selections[0].Targets[0] = "mutated output"
	fresh := failure.AvailableSelections()
	if fresh[0].Targets[0] != "claude-code" || fresh[1].Targets[0] != "codex" {
		t.Fatalf("retained selectors were aliased: %#v", fresh)
	}
}

func TestNearbyResourceNamesRequireAtMostOneEdit(t *testing.T) {
	for _, test := range []struct {
		left, right string
		want        bool
	}{
		{"review", "reviw", true},
		{"review", "reviex", true},
		{"review", "reviews", true},
		{"review", "review", true},
		{"review", "rewive", false},
		{"review", "other", false},
		{"", "", true},
		{"", "x", true},
		{"", "xy", false},
		{"검토", "검토기", true},
	} {
		if got := oneEditApart(test.left, test.right); got != test.want || oneEditApart(test.right, test.left) != test.want {
			t.Fatalf("one-edit(%q, %q)=%v, want %v", test.left, test.right, got, test.want)
		}
	}
}
