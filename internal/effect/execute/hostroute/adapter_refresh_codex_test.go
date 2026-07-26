package hostroute

import (
	"path/filepath"
	"slices"
	"testing"

	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
)

func TestBuildCodexPluginRefreshSeparatesRelationAndMarketplaceSubjects(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "project with spaces")
	fixture := newCodexHostRouteFixture(t, codexHostRouteFixture{
		sourceRef:  "documents@openai-primary-runtime",
		subjectKey: "documents@openai-primary-runtime",
		scope:      target.ScopeGlobal,
		workDir:    workDir,
	})
	command, err := BuildOperationCommand(OperationBuildInput{
		Contract:  fixture.record,
		Operation: lock.OperationRefresh,
		WorkDir:   workDir,
	})
	if err != nil {
		t.Fatalf("BuildOperationCommand returned error: %v", err)
	}

	attempt := command.AttemptRequest()
	wantArgs := []string{
		"plugin",
		"marketplace",
		"upgrade",
		"openai-primary-runtime",
		"--json",
	}
	if attempt.Command != "codex" ||
		!slices.Equal(attempt.Args, wantArgs) ||
		attempt.WorkDir != workDir {
		t.Fatalf("attempt = %#v, want codex %#v in %q", attempt, wantArgs, workDir)
	}
	if command.RouteRequest().RouteID() != "codex.plugin-marketplace.refresh" {
		t.Fatalf("route id = %q", command.RouteRequest().RouteID())
	}
	disclosure, ok := command.Disclosure()
	if !ok {
		t.Fatal("Codex refresh command has no disclosure")
	}
	if disclosure.ExecutionSubject() !=
		"codex-plugin-marketplace:openai-primary-runtime" ||
		disclosure.ExecutionSubject() == fixture.record.SubjectID().String() ||
		!slices.Contains(disclosure.EffectClasses(), "shared_marketplace_update") ||
		!slices.Contains(disclosure.EffectClasses(), "installed_sibling_cache_refresh") ||
		!slices.Contains(disclosure.RetainedEffectClasses(), "partial_plugin_cache_updates") ||
		!slices.Contains(disclosure.NonClaims(), "plugin_only_mutation") ||
		!slices.Contains(disclosure.NonClaims(), "plugin_install_fallback") {
		t.Fatalf("disclosure = %#v", disclosure)
	}
}

func TestCodexMarketplaceRefreshNeverFallsBackToPluginAdd(t *testing.T) {
	spec := codexHostRouteFixture{
		sourceRef:  "documents@openai-primary-runtime",
		subjectKey: "documents@openai-primary-runtime",
		scope:      target.ScopeGlobal,
		workDir:    t.TempDir(),
	}
	install := mustBuildCodexCommand(t, spec).AttemptRequest()
	fixture := newCodexHostRouteFixture(t, spec)
	refresh, err := BuildOperationCommand(OperationBuildInput{
		Contract:  fixture.record,
		Operation: lock.OperationRefresh,
		WorkDir:   spec.workDir,
	})
	if err != nil {
		t.Fatalf("BuildOperationCommand returned error: %v", err)
	}

	if !slices.Contains(install.Args, "add") ||
		!slices.Contains(install.Args, spec.sourceRef) {
		t.Fatalf("install args = %#v", install.Args)
	}
	refreshArgs := refresh.AttemptRequest().Args
	if slices.Contains(refreshArgs, "add") ||
		slices.Contains(refreshArgs, "documents") ||
		!slices.Contains(refreshArgs, "openai-primary-runtime") {
		t.Fatalf("refresh args = %#v", refreshArgs)
	}
}
