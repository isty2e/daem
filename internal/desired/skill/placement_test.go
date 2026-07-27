package skill

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestTargetPlacementValidatesScopeRelativeCanonicalPaths(t *testing.T) {
	for _, test := range []struct {
		name  string
		scope target.Scope
		value string
	}{
		{name: "project", scope: target.ScopeProject, value: ".agents/skills"},
		{name: "global", scope: target.ScopeGlobal, value: "~/.codex/skills"},
	} {
		t.Run(test.name, func(t *testing.T) {
			placement, err := NewTargetPlacement(test.scope, test.value)
			if err != nil || placement.InstallTo() != test.value {
				t.Fatalf("NewTargetPlacement = %#v, %v", placement, err)
			}
		})
	}

	for _, test := range []struct {
		name  string
		scope target.Scope
		value string
	}{
		{name: "empty", scope: target.ScopeProject},
		{name: "trim", scope: target.ScopeProject, value: " .agents/skills"},
		{name: "project absolute", scope: target.ScopeProject, value: "/tmp/skills"},
		{name: "project windows absolute", scope: target.ScopeProject, value: "C:/Users/me/skills"},
		{name: "project home", scope: target.ScopeProject, value: "~/.agents/skills"},
		{name: "project parent", scope: target.ScopeProject, value: "../skills"},
		{name: "project noncanonical", scope: target.ScopeProject, value: ".agents/../skills"},
		{name: "global relative", scope: target.ScopeGlobal, value: ".agents/skills"},
		{name: "global absolute", scope: target.ScopeGlobal, value: "/home/user/skills"},
		{name: "global home root", scope: target.ScopeGlobal, value: "~/"},
		{name: "global parent", scope: target.ScopeGlobal, value: "~/../skills"},
		{name: "backslash", scope: target.ScopeProject, value: `.agents\skills`},
		{name: "control", scope: target.ScopeProject, value: ".agents/\nskills"},
		{name: "bidi", scope: target.ScopeProject, value: ".agents/\u202eskills"},
		{name: "invalid utf8", scope: target.ScopeProject, value: string([]byte{'.', 0xff})},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewTargetPlacement(test.scope, test.value); err == nil {
				t.Fatalf("NewTargetPlacement(%q, %q) returned nil error", test.scope, test.value)
			}
		})
	}
}

func TestSkillTargetPlacementsRequireDeclaredTargetsAndDefensiveStorage(t *testing.T) {
	codexPlacement, err := NewTargetPlacement(target.ScopeProject, ".agents/skills")
	if err != nil {
		t.Fatal(err)
	}
	placements := map[target.Target]TargetPlacement{target.TargetCodex: codexPlacement}
	skill, err := New(Spec{
		Name:        "review",
		Source:      sourcetest.Local(t, "skills/review", source.LocalSourceModeVendor),
		Targets:     []target.Target{target.TargetCodex},
		Placements:  placements,
		Scope:       target.ScopeProject,
		InstallMode: InstallModeCopy,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	delete(placements, target.TargetCodex)
	got := skill.TargetPlacements()
	if got[target.TargetCodex].InstallTo() != ".agents/skills" {
		t.Fatalf("TargetPlacements = %#v", got)
	}
	delete(got, target.TargetCodex)
	if len(skill.TargetPlacements()) != 1 {
		t.Fatal("TargetPlacements returned aliased storage")
	}

	piPlacement, err := NewTargetPlacement(target.ScopeProject, ".pi/skills")
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(Spec{
		Name:        "review",
		Source:      sourcetest.Local(t, "skills/review", source.LocalSourceModeVendor),
		Targets:     []target.Target{target.TargetCodex},
		Placements:  map[target.Target]TargetPlacement{target.TargetPi: piPlacement},
		Scope:       target.ScopeProject,
		InstallMode: InstallModeCopy,
	})
	if err == nil || !strings.Contains(err.Error(), `target "pi" is not declared`) {
		t.Fatalf("undeclared placement error = %v", err)
	}
}
