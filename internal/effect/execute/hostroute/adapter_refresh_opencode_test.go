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

func TestBuildOpenCodePluginRefreshCommandUsesExactScopeAndRoute(t *testing.T) {
	tests := []struct {
		name     string
		scope    target.Scope
		wantArgs []string
	}{
		{
			name:     "project",
			scope:    target.ScopeProject,
			wantArgs: []string{"plugin", "@acme/opencode-formatter", "--force"},
		},
		{
			name:     "explicit global",
			scope:    target.ScopeGlobal,
			wantArgs: []string{"plugin", "@acme/opencode-formatter", "--force", "--global"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workDir := filepath.Join(t.TempDir(), "project with spaces")
			fixture := newOpenCodeHostRouteFixture(t, hostSourceRouteFixture{
				sourceRef:  "@acme/opencode-formatter",
				subjectKey: "@acme/opencode-formatter",
				scope:      test.scope,
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
			if attempt.Command != "opencode" ||
				!slices.Equal(attempt.Args, test.wantArgs) ||
				attempt.WorkDir != workDir {
				t.Fatalf("attempt = %#v, want opencode %#v in %q", attempt, test.wantArgs, workDir)
			}
			if command.RouteRequest().RouteID() != "opencode.plugin-carrier.refresh" {
				t.Fatalf("route id = %q", command.RouteRequest().RouteID())
			}
			disclosure, ok := command.Disclosure()
			if !ok {
				t.Fatal("OpenCode refresh command has no disclosure")
			}
			if disclosure.ExecutionSubject() != fixture.record.SubjectID().String() ||
				!slices.Contains(disclosure.EffectClasses(), "same_family_config_replacement") ||
				!slices.Contains(disclosure.RetainedEffectClasses(), "package_cache") ||
				!slices.Contains(disclosure.NonClaims(), "relation_observation") {
				t.Fatalf("disclosure = %#v", disclosure)
			}
		})
	}
}

func TestOpenCodeRefreshForceNeverLeaksIntoInstall(t *testing.T) {
	spec := hostSourceRouteFixture{
		sourceRef:  "@acme/opencode-formatter",
		subjectKey: "@acme/opencode-formatter",
		scope:      target.ScopeGlobal,
		workDir:    t.TempDir(),
	}
	install := mustBuildOpenCodeCommand(t, spec).AttemptRequest()
	fixture := newOpenCodeHostRouteFixture(t, spec)
	refresh, err := BuildOperationCommand(OperationBuildInput{
		Contract:  fixture.record,
		Operation: lock.OperationRefresh,
		WorkDir:   spec.workDir,
	})
	if err != nil {
		t.Fatalf("BuildOperationCommand returned error: %v", err)
	}

	if slices.Contains(install.Args, "--force") {
		t.Fatalf("install args = %#v, must not contain --force", install.Args)
	}
	if !slices.Contains(refresh.AttemptRequest().Args, "--force") {
		t.Fatalf("refresh args = %#v, want --force", refresh.AttemptRequest().Args)
	}
}

func TestOpenCodePluginRefreshRejectsHostOptionSource(t *testing.T) {
	_, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		"--global",
	)
	if err == nil || !strings.Contains(err.Error(), "must not begin with '-'") {
		t.Fatalf("NewSourceRef error = %v, want option-like source rejection", err)
	}
}
