package apply

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/subprocess"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestPiSourceDiagnosticsSurviveProviderEffectAndReplan(t *testing.T) {
	for _, test := range []struct {
		name, source, code string
	}{
		{"git-plus", "git+https://router.home.arpa/acme/diagnostic.git#v1", "extension.pi_source.git_plus"},
		{"bare-scp", "git@router.home.arpa:acme/diagnostic.git#v1", "extension.pi_source.bare_scp"},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertPiSourceProviderReplan(t, test.source, test.code)
		})
	}
}

func assertPiSourceProviderReplan(t *testing.T, source, code string) {
	t.Helper()
	root, manifestPath := writePiProviderMCPFixture(t)
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	writeApplyFile(t, manifestPath, string(content)+`
[[extension]]
id = "diagnostic-source"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "`+source+`" }
`)
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(root, ".pi", "settings.json")
	writeApplyFile(t, settingsPath, fmt.Sprintf(`{"packages":[%q]}`, source))
	writeApplyFile(t, filepath.Join(root, ".pi", "git", "router.home.arpa", "acme", "diagnostic", "package.json"),
		`{"name":"diagnostic-fixture","version":"1.0.0"}`)

	planned, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath, ManageUnmanagedMatches: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(planned.Diagnostics) != 1 || planned.Diagnostics[0].Code != code {
		t.Fatalf("prepared diagnostics=%#v, want Pi source warning", planned.Diagnostics)
	}
	before := slices.Clone(planned.Diagnostics)
	var requests []subprocess.CommandRequest
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			requests = append(requests, request)
			if request.Command != "pi" || !slices.Equal(request.Args, []string{"install", piProviderSource, "-l"}) {
				t.Fatalf("unexpected host effect: %#v", request)
			}
			writePiProviderInstallation(t, root, "2.15.0")
			writeApplyFile(t, settingsPath, fmt.Sprintf(`{"packages":[%q,%q]}`, piProviderSource, source))
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})
	result, err := ExecuteWithOptions(t.Context(), planned, ExecuteOptions{HostRouteExecutor: executor})
	if err != nil {
		t.Fatalf("provider continuation failed with deterministic source warning: %v", err)
	}
	if len(requests) != 1 || !reflect.DeepEqual(before, result.Diagnostics) {
		t.Fatalf("provider/replan changed warning or host effects: requests=%#v diagnostics=%#v", requests, result.Diagnostics)
	}
}
