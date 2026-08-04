package build

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hook"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/encoding/hookdocument"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildRejectsHookEventBeyondHostProjectionBudget(t *testing.T) {
	environment := lockEnvironment(t, desired.Spec{Hooks: []hook.Hook{
		desiredtest.Hook(t, hook.Spec{
			Name: "oversized-event", Event: strings.Repeat("e", hookdocument.MaximumEventBytes+1),
			Type: hook.TypeCommand, Command: "true",
			Targets: []target.Target{target.TargetClaudeCode}, Scope: target.ScopeProject,
		}),
	}})

	_, err := buildWithTestOptions(context.Background(), environment, stubResolver{}, Options{})
	if !errors.Is(err, hookdocument.ErrStructuralBudgetExceeded) {
		t.Fatalf("Build error = %v, want Hook structural budget error", err)
	}
}
