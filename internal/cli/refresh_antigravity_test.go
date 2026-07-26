package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
	lockworkflow "github.com/isty2e/daem/internal/workflow/lock"
)

func TestAntigravityCLIRefreshDisclosesRepeatInstallForExactGlobalSource(t *testing.T) {
	for _, source := range []string{
		"./local-plugin",
		"modern-web-guidance@google",
	} {
		t.Run(source, func(t *testing.T) {
			manifestPath := writeAntigravityCLIRefreshFixture(t, source)
			report := runAntigravityCLIRefresh(t, manifestPath, true, nil, 0)
			if report.Mode != "dry_run" ||
				report.Selection.ID != "guidance" ||
				report.Selection.Scope != "global" ||
				report.Route.RouteID != "antigravity-cli.plugin-carrier.refresh" ||
				report.Route.AdapterContractVersion != "antigravity-cli-plugin-refresh-v1" ||
				report.Route.ObservationPosture != "attempt_when_unsupported" ||
				report.Disclosure.Command != "agy" ||
				!slices.Equal(
					report.Disclosure.Args,
					[]string{"plugin", "install", source},
				) ||
				!slices.Contains(report.Disclosure.EffectClasses, "plugin_bundle_replacement") ||
				!slices.Contains(report.Disclosure.EffectClasses, "import_registry_maintenance") ||
				!slices.Contains(
					report.Disclosure.RetainedEffectClasses,
					"partial_host_source_state",
				) ||
				!slices.Contains(report.Disclosure.NonClaims, "dedicated_host_update") ||
				!slices.Contains(report.Disclosure.NonClaims, "plugin_activation") ||
				!slices.Contains(report.Disclosure.NonClaims, "plugin_uninstall") ||
				!slices.Contains(report.Disclosure.NonClaims, "antigravity_ide_support") ||
				report.Result.Class != "planned" {
				t.Fatalf("dry-run report = %#v", report)
			}
		})
	}
}

func TestAntigravityCLIRefreshRepeatsAttemptWithoutChangingDesiredOrLockedState(t *testing.T) {
	const source = "modern-web-guidance@google"
	manifestPath := writeAntigravityCLIRefreshFixture(t, source)
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	manifestBefore, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile manifest: %v", err)
	}
	lockBefore, err := os.ReadFile(paths.LockfilePath)
	if err != nil {
		t.Fatalf("ReadFile lockfile: %v", err)
	}

	calls := 0
	runner := func(
		_ context.Context,
		request subprocess.CommandRequest,
	) subprocess.CommandResult {
		calls++
		if request.Command != "agy" ||
			!slices.Equal(
				request.Args,
				[]string{"plugin", "install", source},
			) {
			t.Fatalf("Antigravity CLI refresh request = %#v", request)
		}
		for _, forbidden := range []string{
			"import",
			"link",
			"enable",
			"disable",
			"uninstall",
			"--project",
		} {
			if slices.Contains(request.Args, forbidden) {
				t.Fatalf(
					"Antigravity CLI refresh request contains forbidden %q: %#v",
					forbidden,
					request,
				)
			}
		}
		return subprocess.CommandResult{
			Started:     true,
			Stdout:      "token=antigravity-refresh-secret " + string(bytes.Repeat([]byte("x"), 256)),
			Stderr:      "{malformed-host-output",
			ExitCode:    0,
			HasExitCode: true,
		}
	}
	first := runAntigravityCLIRefresh(t, manifestPath, false, runner, 0)
	second := runAntigravityCLIRefresh(t, manifestPath, false, runner, 0)
	for index, report := range []hostRouteRefreshReport{first, second} {
		if report.Result.Class != "attempted_unverified" ||
			!report.Result.Attempted ||
			report.Result.ProcessOutcome == nil ||
			!report.Result.ProcessOutcome.Redacted ||
			!report.Result.AttemptHistory.Persisted {
			t.Fatalf("execution report[%d] = %#v", index, report)
		}
	}
	if calls != 2 {
		t.Fatalf("runner calls = %d, want 2 explicit attempts", calls)
	}
	if got, err := os.ReadFile(manifestPath); err != nil || !bytes.Equal(got, manifestBefore) {
		t.Fatalf("manifest changed: err=%v", err)
	}
	if got, err := os.ReadFile(paths.LockfilePath); err != nil || !bytes.Equal(got, lockBefore) {
		t.Fatalf("lockfile changed: err=%v", err)
	}
	if got, err := os.ReadFile(paths.StatefilePath); err != nil ||
		bytes.Contains(got, []byte("antigravity-refresh-secret")) {
		t.Fatalf("statefile leaked host output: err=%v", err)
	}
}

func TestAntigravityCLIRefreshReportsHostFailuresAndPermitsExplicitRetry(t *testing.T) {
	tests := []struct {
		name          string
		result        subprocess.CommandResult
		wantClass     string
		wantReason    string
		wantAttempted bool
		wantPersisted bool
	}{
		{
			name:       "missing executable",
			result:     subprocess.CommandResult{MissingRunner: true, Err: os.ErrNotExist},
			wantClass:  "failed",
			wantReason: "missing_runner",
		},
		{
			name: "malformed plugin manifest retains prior bundle",
			result: subprocess.CommandResult{
				Started:     true,
				ExitCode:    21,
				HasExitCode: true,
				Err:         errors.New("plugin.json is malformed"),
			},
			wantClass:     "failed",
			wantReason:    "nonzero_exit",
			wantAttempted: true,
			wantPersisted: true,
		},
		{
			name: "host source resolution failure",
			result: subprocess.CommandResult{
				Started:     true,
				ExitCode:    22,
				HasExitCode: true,
				Err:         errors.New("plugin source not found"),
			},
			wantClass:     "failed",
			wantReason:    "nonzero_exit",
			wantAttempted: true,
			wantPersisted: true,
		},
		{
			name: "started cancellation",
			result: subprocess.CommandResult{
				Started:  true,
				Canceled: true,
				Err:      context.Canceled,
			},
			wantClass:     "partial",
			wantReason:    "canceled",
			wantAttempted: true,
			wantPersisted: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifestPath := writeAntigravityCLIRefreshFixture(
				t,
				"modern-web-guidance@google",
			)
			calls := 0
			runner := func(
				_ context.Context,
				request subprocess.CommandRequest,
			) subprocess.CommandResult {
				calls++
				if !slices.Equal(
					request.Args,
					[]string{"plugin", "install", "modern-web-guidance@google"},
				) {
					t.Fatalf("unexpected host route: %#v", request)
				}
				if calls == 1 {
					return test.result
				}
				return subprocess.CommandResult{
					Started:     true,
					ExitCode:    0,
					HasExitCode: true,
				}
			}
			failed := runAntigravityCLIRefresh(t, manifestPath, false, runner, 1)
			if failed.Result.Class != test.wantClass ||
				failed.Result.Attempted != test.wantAttempted ||
				failed.Result.ProcessOutcome == nil ||
				failed.Result.ProcessOutcome.Reason != test.wantReason ||
				failed.Result.AttemptHistory.Persisted != test.wantPersisted {
				t.Fatalf("failed report = %#v", failed)
			}
			retried := runAntigravityCLIRefresh(t, manifestPath, false, runner, 0)
			if retried.Result.Class != "attempted_unverified" ||
				!retried.Result.Attempted ||
				!retried.Result.AttemptHistory.Persisted {
				t.Fatalf("retry report = %#v", retried)
			}
			if calls != 2 {
				t.Fatalf("runner calls = %d, want failure plus explicit retry", calls)
			}
		})
	}
}

func runAntigravityCLIRefresh(
	t *testing.T,
	manifestPath string,
	dryRun bool,
	runner subprocess.CommandRunner,
	wantExit int,
) hostRouteRefreshReport {
	t.Helper()
	return runHostRouteRefreshCLI(
		t,
		manifestPath,
		"guidance",
		dryRun,
		runner,
		wantExit,
	)
}

func writeAntigravityCLIRefreshFixture(t *testing.T, source string) string {
	t.Helper()
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	manifest := fmt.Sprintf(`version = 1
targets = ["antigravity-cli"]

[[extension]]
id = "guidance"
carrier = "antigravity-cli-plugin"
scope = "global"
source = { host_source = %q }
`, source)
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if _, err := lockworkflow.RunLock(
		context.Background(),
		lockworkflow.LockInput{ManifestPath: manifestPath},
	); err != nil {
		t.Fatalf("RunLock returned error: %v", err)
	}
	return manifestPath
}
