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

func TestBuildPiPackageRefreshCommandPreservesCrossScopeHostRoute(t *testing.T) {
	for _, scope := range []target.Scope{
		target.ScopeProject,
		target.ScopeGlobal,
	} {
		t.Run(string(scope), func(t *testing.T) {
			workDir := filepath.Join(t.TempDir(), "trusted project with spaces")
			fixture := newPiHostRouteFixture(t, hostSourceRouteFixture{
				sourceRef:  "github:acme/pi-tools",
				subjectKey: "github:acme/pi-tools",
				scope:      scope,
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
				"update",
				"--extension",
				"github:acme/pi-tools",
			}
			if attempt.Command != "pi" ||
				!slices.Equal(attempt.Args, wantArgs) ||
				attempt.WorkDir != workDir {
				t.Fatalf("attempt = %#v, want pi %#v in %q", attempt, wantArgs, workDir)
			}
			if command.RouteRequest().RouteID() != "pi.package-carrier.refresh" {
				t.Fatalf("route id = %q", command.RouteRequest().RouteID())
			}
			disclosure, ok := command.Disclosure()
			if !ok {
				t.Fatal("Pi refresh command has no disclosure")
			}
			if disclosure.ExecutionSubject() != fixture.record.SubjectID().String() ||
				!slices.Contains(disclosure.EffectClasses(), "cross_scope_identity_update") ||
				!slices.Contains(disclosure.RetainedEffectClasses(), "partial_scope_updates") ||
				!slices.Contains(disclosure.NonClaims(), "scope_locality") {
				t.Fatalf("disclosure = %#v", disclosure)
			}
		})
	}
}

func TestPiPackageRefreshCannotConstructInstallSelfModelOrBulkRoutes(t *testing.T) {
	spec := hostSourceRouteFixture{
		sourceRef:  "github:acme/pi-tools",
		subjectKey: "github:acme/pi-tools",
		scope:      target.ScopeProject,
		workDir:    t.TempDir(),
	}
	install := mustBuildPiCommand(t, spec).AttemptRequest()
	fixture := newPiHostRouteFixture(t, spec)
	refresh, err := BuildOperationCommand(OperationBuildInput{
		Contract:  fixture.record,
		Operation: lock.OperationRefresh,
		WorkDir:   spec.workDir,
	})
	if err != nil {
		t.Fatalf("BuildOperationCommand returned error: %v", err)
	}

	if !slices.Contains(install.Args, "install") ||
		!slices.Contains(install.Args, "-l") {
		t.Fatalf("project install args = %#v", install.Args)
	}
	refreshArgs := refresh.AttemptRequest().Args
	for _, forbidden := range []string{
		"install",
		"-l",
		"--self",
		"--models",
		"--extensions",
		"--all",
		"--approve",
		"--no-approve",
	} {
		if slices.Contains(refreshArgs, forbidden) {
			t.Fatalf("refresh args = %#v, contain forbidden %q", refreshArgs, forbidden)
		}
	}
}

func TestPiPackageRefreshRejectsHostOptionSource(t *testing.T) {
	_, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		"--self",
	)
	if err == nil || !strings.Contains(err.Error(), "must not begin with '-'") {
		t.Fatalf("NewSourceRef error = %v, want option-like source rejection", err)
	}
}
