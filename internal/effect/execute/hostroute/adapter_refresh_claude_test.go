package hostroute

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildClaudePluginRefreshCommandUsesExactScopeAndRoute(t *testing.T) {
	tests := []struct {
		name          string
		scope         target.Scope
		wantHostScope string
	}{
		{name: "project", scope: target.ScopeProject, wantHostScope: "project"},
		{name: "explicit global", scope: target.ScopeGlobal, wantHostScope: "user"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workDir := filepath.Join(t.TempDir(), "project with spaces")
			record, _ := mustClaudePluginFixture(t, subjectSpec{
				sourceKind: desiredextension.SourceKindMarketplace,
				sourceRef:  "context7@official",
				subjectKey: "context7@official",
				scope:      test.scope,
			})
			command, err := BuildOperationCommand(OperationBuildInput{
				Contract:  record,
				Operation: lock.OperationRefresh,
				WorkDir:   workDir,
			})
			if err != nil {
				t.Fatalf("BuildOperationCommand returned error: %v", err)
			}

			attempt := command.AttemptRequest()
			wantArgs := []string{
				"plugin",
				"update",
				"context7@official",
				"--scope",
				test.wantHostScope,
			}
			if attempt.Command != "claude" ||
				!slices.Equal(attempt.Args, wantArgs) ||
				attempt.WorkDir != workDir {
				t.Fatalf("attempt = %#v, want claude %#v in %q", attempt, wantArgs, workDir)
			}
			if command.RouteRequest().RouteID() != "claude-code.plugin-carrier.refresh" {
				t.Fatalf(
					"route id = %q, want Claude refresh route",
					command.RouteRequest().RouteID(),
				)
			}
			disclosure, ok := command.Disclosure()
			if !ok {
				t.Fatal("Claude refresh command has no disclosure")
			}
			if disclosure.ExecutionSubject() != record.SubjectID().String() ||
				!slices.Contains(disclosure.EffectClasses(), "restart_required") ||
				!slices.Contains(disclosure.RetainedEffectClasses(), "old_plugin_cache") ||
				!slices.Contains(disclosure.NonClaims(), "exact_artifact_convergence") {
				t.Fatalf("disclosure = %#v", disclosure)
			}
		})
	}
}

func TestClaudePluginRefreshNeverFallsBackToInstall(t *testing.T) {
	record, _ := mustClaudePluginFixture(t, subjectSpec{
		sourceKind: desiredextension.SourceKindMarketplace,
		sourceRef:  "context7@official",
		subjectKey: "context7@official",
		scope:      target.ScopeProject,
	})
	command, err := BuildOperationCommand(OperationBuildInput{
		Contract:  record,
		Operation: lock.OperationRefresh,
		WorkDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("BuildOperationCommand returned error: %v", err)
	}
	args := command.AttemptRequest().Args
	if slices.Contains(args, "install") || !slices.Contains(args, "update") {
		t.Fatalf("refresh args = %#v, want update and no install", args)
	}
}

func TestClaudePluginRefreshRejectsHostOptionSelector(t *testing.T) {
	_, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		"--danger@official",
	)
	if err == nil || !strings.Contains(err.Error(), "must not begin with '-'") {
		t.Fatalf("NewSourceRef error = %v, want option-like source rejection", err)
	}
}
