package readiness

import (
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	hookresource "github.com/isty2e/daem/internal/desired/hook"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestSelectedUnsupportedProjectionsReportsAntigravityCLIHook(t *testing.T) {
	environment := desiredtest.Environment(t, desired.Spec{
		Targets:  []target.Target{target.TargetCodex, target.TargetAntigravityCLI},
		Defaults: desiredtest.Defaults(t, target.ScopeProject, skill.InstallModeCopy),
		Hooks: []hookresource.Hook{
			desiredtest.Hook(t, hookresource.Spec{
				Name:    "protect-env",
				Event:   "PreToolUse",
				Type:    hookresource.TypeCommand,
				Command: "python3 hooks/protect.py",
				Targets: []target.Target{
					target.TargetCodex,
					target.TargetAntigravityCLI,
				},
				Scope: target.ScopeProject,
			}),
		},
	})
	selection, err := targetselection.ForAvailableTargets(
		[]target.Target{target.TargetCodex, target.TargetAntigravityCLI},
		nil,
	)
	if err != nil {
		t.Fatalf("ForAvailableTargets returned error: %v", err)
	}

	projections := SelectedUnsupportedProjections(environment, selection)
	if len(projections) != 1 {
		t.Fatalf("unsupported projections = %#v, want one Antigravity CLI hook", projections)
	}
	got := projections[0]
	if got.EntityID().Kind() != entity.KindHook ||
		got.EntityID().Name() != "protect-env" ||
		!reflect.DeepEqual(got.Targets(), []target.Target{target.TargetAntigravityCLI}) {
		t.Fatalf("unsupported hook = %#v, want protect-env antigravity-cli only", got)
	}
}

func TestUnsupportedProjectionTargetsReturnsDefensiveCopy(t *testing.T) {
	environment := desiredtest.Environment(t, desired.Spec{
		Targets:  []target.Target{target.TargetAntigravityCLI},
		Defaults: desiredtest.Defaults(t, target.ScopeProject, skill.InstallModeCopy),
		Hooks: []hookresource.Hook{
			desiredtest.Hook(t, hookresource.Spec{
				Name:    "protect-env",
				Event:   "PreToolUse",
				Type:    hookresource.TypeCommand,
				Command: "python3 hooks/protect.py",
				Targets: []target.Target{target.TargetAntigravityCLI},
				Scope:   target.ScopeProject,
			}),
		},
	})
	selection, err := targetselection.ForAvailableTargets(
		[]target.Target{target.TargetAntigravityCLI},
		nil,
	)
	if err != nil {
		t.Fatalf("ForAvailableTargets returned error: %v", err)
	}

	projections := SelectedUnsupportedProjections(environment, selection)
	first := projections[0].Targets()
	first[0] = target.TargetCodex
	if got := projections[0].Targets(); !reflect.DeepEqual(got, []target.Target{target.TargetAntigravityCLI}) {
		t.Fatalf("Targets() after caller mutation = %#v", got)
	}
}
