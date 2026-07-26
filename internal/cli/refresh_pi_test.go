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

func TestPiRefreshCLIDisclosesExactCrossScopeRouteAndSourceLimits(t *testing.T) {
	tests := []struct {
		name         string
		scope        string
		source       string
		lockedSource string
	}{
		{name: "project git", scope: "project", source: "github:acme/pi-tools", lockedSource: "github:acme/pi-tools"},
		{name: "global npm pin", scope: "global", source: "npm:@acme/pi-tools@1.2.3", lockedSource: "npm:@acme/pi-tools@1.2.3"},
		{name: "project local path", scope: "project", source: "./local-tools", lockedSource: "local-tools"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifestPath := writePiCLIRefreshFixture(t, test.scope, test.source)
			report := runPiRefreshCLI(t, manifestPath, true, nil)
			if report.Mode != "dry_run" ||
				report.Selection.Scope != test.scope ||
				report.Route.RouteID != "pi.package-carrier.refresh" ||
				report.Route.ObservationPosture != "attempt_when_unsupported" ||
				report.Disclosure.Command != "pi" ||
				!slices.Equal(
					report.Disclosure.Args,
					[]string{"update", "--extension", test.lockedSource},
				) ||
				!slices.Contains(report.Disclosure.EffectClasses, "cross_scope_identity_update") ||
				!slices.Contains(report.Disclosure.RetainedEffectClasses, "partial_scope_updates") ||
				!slices.Contains(report.Disclosure.NonClaims, "scope_locality") ||
				!slices.Contains(report.Disclosure.NonClaims, "pinned_npm_update") ||
				!slices.Contains(report.Disclosure.NonClaims, "local_path_artifact_update") ||
				report.Result.Class != "planned" {
				t.Fatalf("dry-run report = %#v", report)
			}
		})
	}
}

func TestPiRefreshCLIUsesRealAdapterAndRemainsRepeatable(t *testing.T) {
	manifestPath := writePiCLIRefreshFixture(
		t,
		"project",
		"github:acme/pi-tools",
	)
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
		if request.Command != "pi" ||
			!slices.Equal(
				request.Args,
				[]string{"update", "--extension", "github:acme/pi-tools"},
			) {
			t.Fatalf("Pi refresh request = %#v", request)
		}
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
			if slices.Contains(request.Args, forbidden) {
				t.Fatalf("Pi refresh request contains forbidden %q: %#v", forbidden, request)
			}
		}
		return subprocess.CommandResult{
			Started:     true,
			Stdout:      "token=pi-refresh-secret " + string(bytes.Repeat([]byte("x"), 256)),
			Stderr:      "{malformed-host-output",
			ExitCode:    0,
			HasExitCode: true,
		}
	}
	first := runPiRefreshCLI(t, manifestPath, false, runner)
	second := runPiRefreshCLI(t, manifestPath, false, runner)
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
		bytes.Contains(got, []byte("pi-refresh-secret")) {
		t.Fatalf("statefile leaked host output: err=%v", err)
	}
}

func TestPiRefreshCLIReportsHostFailuresWithoutInstallFallback(t *testing.T) {
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
			name: "project trust refusal",
			result: subprocess.CommandResult{
				Started:     true,
				ExitCode:    17,
				HasExitCode: true,
				Err:         errors.New("project is not trusted"),
			},
			wantClass:     "failed",
			wantReason:    "nonzero_exit",
			wantAttempted: true,
			wantPersisted: true,
		},
		{
			name: "no matching package",
			result: subprocess.CommandResult{
				Started:     true,
				ExitCode:    18,
				HasExitCode: true,
				Err:         errors.New("no matching extension"),
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
			manifestPath := writePiCLIRefreshFixture(
				t,
				"project",
				"github:acme/pi-tools",
			)
			report := runPiRefreshCLIExpectExit(
				t,
				manifestPath,
				false,
				func(
					_ context.Context,
					request subprocess.CommandRequest,
				) subprocess.CommandResult {
					if slices.Contains(request.Args, "install") {
						t.Fatalf("refresh fell back to install: %#v", request.Args)
					}
					return test.result
				},
				1,
			)
			if report.Result.Class != test.wantClass ||
				report.Result.Attempted != test.wantAttempted ||
				report.Result.ProcessOutcome == nil ||
				report.Result.ProcessOutcome.Reason != test.wantReason ||
				report.Result.AttemptHistory.Persisted != test.wantPersisted {
				t.Fatalf("report = %#v", report)
			}
		})
	}
}

func runPiRefreshCLI(
	t *testing.T,
	manifestPath string,
	dryRun bool,
	runner subprocess.CommandRunner,
) hostRouteRefreshReport {
	t.Helper()
	return runPiRefreshCLIExpectExit(t, manifestPath, dryRun, runner, 0)
}

func runPiRefreshCLIExpectExit(
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
		"pi-tools",
		dryRun,
		runner,
		wantExit,
	)
}

func writePiCLIRefreshFixture(
	t *testing.T,
	scope string,
	source string,
) string {
	t.Helper()
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	manifest := fmt.Sprintf(`version = 1
targets = ["pi"]

[[extension]]
id = "pi-tools"
carrier = "pi-package"
scope = %q
source = { host_source = %q }
`, scope, source)
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
