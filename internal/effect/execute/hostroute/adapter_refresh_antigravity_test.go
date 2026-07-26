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

func TestBuildAntigravityCLIPluginRefreshCommandRepeatsExactGlobalSource(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "trusted project with spaces")
	fixture := newAntigravityCLIHostRouteFixture(t, hostSourceRouteFixture{
		sourceRef:  "modern-web-guidance@google",
		subjectKey: "modern-web-guidance@google",
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
	wantArgs := []string{"plugin", "install", "modern-web-guidance@google"}
	if attempt.Command != "agy" ||
		!slices.Equal(attempt.Args, wantArgs) ||
		attempt.WorkDir != workDir {
		t.Fatalf("attempt = %#v, want agy %#v in %q", attempt, wantArgs, workDir)
	}
	if command.RouteRequest().RouteID() != "antigravity-cli.plugin-carrier.refresh" ||
		command.RouteRequest().ContractVersion() != "antigravity-cli-plugin-refresh-v1" {
		t.Fatalf("refresh route = %#v", command.RouteRequest())
	}
	disclosure, ok := command.Disclosure()
	if !ok {
		t.Fatal("Antigravity CLI refresh command has no disclosure")
	}
	if disclosure.ExecutionSubject() != fixture.record.SubjectID().String() ||
		!slices.Contains(disclosure.EffectClasses(), "plugin_bundle_replacement") ||
		!slices.Contains(disclosure.EffectClasses(), "import_registry_maintenance") ||
		!slices.Contains(disclosure.RetainedEffectClasses(), "partial_host_source_state") ||
		!slices.Contains(disclosure.NonClaims(), "dedicated_host_update") ||
		!slices.Contains(disclosure.NonClaims(), "antigravity_ide_support") {
		t.Fatalf("disclosure = %#v", disclosure)
	}
}

func TestAntigravityCLIPluginRefreshRejectsOptionLikeSourceBeforeExecution(t *testing.T) {
	_, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		"--help",
	)
	if err == nil || !strings.Contains(err.Error(), "must not begin with '-'") {
		t.Fatalf("NewSourceRef error = %v, want option-like source rejection", err)
	}
}

func TestAntigravityCLIInstallAndRefreshShareOnlyNativeArgv(t *testing.T) {
	spec := hostSourceRouteFixture{
		sourceRef:  "./local-plugin",
		subjectKey: "./local-plugin",
		scope:      target.ScopeGlobal,
		workDir:    t.TempDir(),
	}
	install := mustBuildAntigravityCLICommand(t, spec)
	fixture := newAntigravityCLIHostRouteFixture(t, spec)
	refresh, err := BuildOperationCommand(OperationBuildInput{
		Contract:  fixture.record,
		Operation: lock.OperationRefresh,
		WorkDir:   spec.workDir,
	})
	if err != nil {
		t.Fatalf("BuildOperationCommand returned error: %v", err)
	}

	if install.AttemptRequest().Command != refresh.AttemptRequest().Command ||
		!slices.Equal(install.AttemptRequest().Args, refresh.AttemptRequest().Args) {
		t.Fatalf(
			"install/refresh native requests differ: %#v / %#v",
			install.AttemptRequest(),
			refresh.AttemptRequest(),
		)
	}
	installRoute := install.RouteRequest()
	refreshRoute := refresh.RouteRequest()
	if installRoute.RouteID() == refreshRoute.RouteID() ||
		installRoute.ContractVersion() == refreshRoute.ContractVersion() ||
		installRoute.CanonicalRequestHash() == refreshRoute.CanonicalRequestHash() {
		t.Fatalf(
			"install/refresh route identities overlap: %#v / %#v",
			installRoute,
			refreshRoute,
		)
	}
	if installRoute.RouteID() != "antigravity-cli.plugin-carrier.install" ||
		refreshRoute.RouteID() != "antigravity-cli.plugin-carrier.refresh" {
		t.Fatalf(
			"install/refresh route ids = %q / %q",
			installRoute.RouteID(),
			refreshRoute.RouteID(),
		)
	}
}
