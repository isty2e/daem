package adopt

import (
	"slices"
	"strings"
	"testing"

	targetpkg "github.com/isty2e/daem/internal/target"
)

func TestCanonicalSkillSourceRoutesCollapsesExactRawDuplicates(t *testing.T) {
	piRoute := SkillSourceRoute{Target: targetpkg.TargetPi, LivePath: "/pi/alpha", ReadPath: "/pi/alpha"}
	codexRoute := SkillSourceRoute{Target: targetpkg.TargetCodex, LivePath: "/codex/alpha", ReadPath: "/codex/alpha"}
	skill := Skill{
		Targets:      []targetpkg.Target{targetpkg.TargetPi, targetpkg.TargetCodex},
		SourceRoutes: []SkillSourceRoute{piRoute, codexRoute, piRoute},
	}

	routes, err := skill.CanonicalSourceRoutes()
	if err != nil {
		t.Fatal(err)
	}
	want := []SkillSourceRoute{codexRoute, piRoute}
	if !slices.Equal(routes, want) {
		t.Fatalf("canonical routes = %#v, want %#v", routes, want)
	}
}

func TestCanonicalSkillSourceRoutesRejectsDistinctRoutesForOneTarget(t *testing.T) {
	skill := Skill{
		Targets: []targetpkg.Target{targetpkg.TargetCodex},
		SourceRoutes: []SkillSourceRoute{
			{Target: targetpkg.TargetCodex, LivePath: "/codex/alpha", ReadPath: "/codex/alpha"},
			{Target: targetpkg.TargetCodex, LivePath: "/other/alpha", ReadPath: "/other/alpha"},
		},
	}

	_, err := skill.CanonicalSourceRoutes()
	if err == nil || !strings.Contains(err.Error(), "conflicting source routes") {
		t.Fatalf("CanonicalSourceRoutes error = %v, want target route conflict", err)
	}
}
