package listworkflow

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

func TestLocationInventoryDoesNotConflateUnsupportedReasonBoundaries(t *testing.T) {
	rows := make([]LocationEntry, 0, 2)
	for _, diagnostic := range []struct {
		reason string
		detail string
	}{
		{reason: "ab", detail: "c"},
		{reason: "a", detail: "bc"},
	} {
		entry, err := newLocationEntry(locationEntryInput{
			kind: LocationUnsupported, selectedTarget: target.TargetCodex, scope: target.ScopeProject,
			resourceKind: entity.KindHook, realization: LocationUnavailable,
			role: LocationRoleUnsupported, selectionSource: LocationSelectionNotApplicable,
			source: LocationSourceProfile, reason: diagnostic.reason, detail: diagnostic.detail,
		})
		if err != nil {
			t.Fatalf("newLocationEntry returned error: %v", err)
		}
		rows = append(rows, entry)
	}
	inventory, err := newLocationInventory(rows)
	if err != nil {
		t.Fatalf("newLocationInventory conflated distinct diagnostics: %v", err)
	}
	if len(inventory.Entries()) != 2 {
		t.Fatalf("inventory entries = %d, want 2", len(inventory.Entries()))
	}
}

func TestLocationEntryRejectsContradictoryVariants(t *testing.T) {
	base := locationEntryInput{
		kind: LocationPath, selectedTarget: target.TargetCodex, scope: target.ScopeProject,
		resourceKind: entity.KindSkill, realization: LocationManagedPath,
		role: LocationRoleWrite, path: ".agents/skills",
		selectionSource: LocationSelectionProfileDefault, source: LocationSourceProfile,
	}
	for _, test := range []struct {
		name   string
		mutate func(*locationEntryInput)
	}{
		{name: "path plus route", mutate: func(input *locationEntryInput) { input.route = "forged" }},
		{name: "selected without request", mutate: func(input *locationEntryInput) { input.selected = true }},
		{name: "unsupported with path", mutate: func(input *locationEntryInput) {
			input.kind = LocationUnsupported
			input.role = LocationRoleUnsupported
			input.realization = LocationUnavailable
			input.reason = "not-implemented"
		}},
		{name: "delegated without operation", mutate: func(input *locationEntryInput) {
			input.kind = LocationRoute
			input.role = LocationRoleDelegated
			input.realization = LocationDelegatedRoute
			input.path = ""
			input.route = "codex.plugin.install"
			input.selectionSource = LocationSelectionNotApplicable
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			if _, err := newLocationEntry(input); err == nil {
				t.Fatal("newLocationEntry accepted contradictory row")
			}
		})
	}
}
