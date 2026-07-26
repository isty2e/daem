package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
	lockworkflow "github.com/isty2e/daem/internal/workflow/lock"
	refreshworkflow "github.com/isty2e/daem/internal/workflow/refresh"
)

func TestCodexRefreshCLIDisclosesSelectedRelationAndMarketplaceOperation(t *testing.T) {
	manifestPath := writeCodexCLIRefreshFixture(t)
	report := runHostRouteRefreshCLI(
		t,
		manifestPath,
		"documents",
		true,
		nil,
		0,
	)

	if report.Mode != "dry_run" ||
		report.Selection.ID != "documents" ||
		report.Selection.Scope != "global" ||
		report.Route.RouteID != "codex.plugin-marketplace.refresh" ||
		report.Route.ExecutionSubject !=
			"codex-plugin-marketplace:openai-primary-runtime" ||
		report.Route.ObservationPosture != "require_current" ||
		report.Disclosure.Command != "codex" ||
		!slices.Equal(
			report.Disclosure.Args,
			[]string{
				"plugin",
				"marketplace",
				"upgrade",
				"openai-primary-runtime",
				"--json",
			},
		) ||
		!slices.Contains(report.Disclosure.EffectClasses, "shared_marketplace_update") ||
		!slices.Contains(report.Disclosure.EffectClasses, "installed_sibling_cache_refresh") ||
		!slices.Contains(
			report.Disclosure.RetainedEffectClasses,
			"partial_plugin_cache_updates",
		) ||
		!slices.Contains(report.Disclosure.NonClaims, "plugin_only_mutation") ||
		!slices.Contains(report.Disclosure.NonClaims, "plugin_install_fallback") ||
		report.Result.Class != "planned" {
		t.Fatalf("dry-run report = %#v", report)
	}
}

func TestCodexRefreshHumanAuthorizationNamesBroaderMarketplaceEffect(t *testing.T) {
	manifestPath := writeCodexCLIRefreshFixture(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := RunWithOptions(
		[]string{
			"refresh",
			"extension",
			"documents",
			"--manifest",
			manifestPath,
			"--yes",
		},
		RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			RefreshExecuteOptions: refreshworkflow.ExecuteOptions{
				CommandOptions: subprocess.CommandOptions{
					Runner: func(
						_ context.Context,
						_ subprocess.CommandRequest,
					) subprocess.CommandResult {
						return subprocess.CommandResult{
							Started:     true,
							ExitCode:    0,
							HasExitCode: true,
						}
					},
				},
			},
		},
	)
	if exitCode != 0 {
		t.Fatalf(
			"RunWithOptions exit=%d stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	for _, expected := range []string{
		`selection: id="documents" target=codex scope=global carrier=codex-plugin`,
		`subject="codex-plugin-marketplace:openai-primary-runtime"`,
		`args=["plugin","marketplace","upgrade","openai-primary-runtime","--json"]`,
		`installed_sibling_cache_refresh`,
		`result: class=observed_relation`,
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("stdout %q does not contain %q", stdout.String(), expected)
		}
	}
}

func TestCodexRefreshCLIReportsNoRevisionAndChangedRevisionAsAttempts(t *testing.T) {
	manifestPath := writeCodexCLIRefreshFixture(t)
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
		if request.Command != "codex" ||
			!slices.Equal(
				request.Args,
				[]string{
					"plugin",
					"marketplace",
					"upgrade",
					"openai-primary-runtime",
					"--json",
				},
			) {
			t.Fatalf("Codex refresh request = %#v", request)
		}
		if slices.Contains(request.Args, "add") ||
			slices.Contains(request.Args, "documents") {
			t.Fatalf("Codex refresh narrowed or fell back to plugin add: %#v", request.Args)
		}
		stdout := `{"upgraded":false}`
		if calls == 2 {
			stdout = `{"upgraded":true,"token":"codex-refresh-secret"}` +
				string(bytes.Repeat([]byte("x"), 256))
		}
		return subprocess.CommandResult{
			Started:     true,
			Stdout:      stdout,
			Stderr:      "{malformed-host-output",
			ExitCode:    0,
			HasExitCode: true,
		}
	}
	first := runHostRouteRefreshCLI(
		t,
		manifestPath,
		"documents",
		false,
		runner,
		0,
	)
	second := runHostRouteRefreshCLI(
		t,
		manifestPath,
		"documents",
		false,
		runner,
		0,
	)
	for index, report := range []hostRouteRefreshReport{first, second} {
		if report.Result.Class != "observed_relation" ||
			!report.Result.Attempted ||
			report.Result.ProcessOutcome == nil ||
			!report.Result.AttemptHistory.Persisted {
			t.Fatalf("execution report[%d] = %#v", index, report)
		}
		if report.Result.ProcessOutcome.Redacted != (index == 1) {
			t.Fatalf(
				"execution report[%d] redacted = %t",
				index,
				report.Result.ProcessOutcome.Redacted,
			)
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
		bytes.Contains(got, []byte("codex-refresh-secret")) {
		t.Fatalf("statefile leaked host output: err=%v", err)
	}
}

func TestCodexRefreshCLIReportsMarketplaceFailuresWithoutPluginAdd(t *testing.T) {
	tests := []struct {
		name          string
		result        subprocess.CommandResult
		wantReason    string
		wantAttempted bool
		wantPersisted bool
	}{
		{
			name:       "missing executable",
			result:     subprocess.CommandResult{MissingRunner: true, Err: os.ErrNotExist},
			wantReason: "missing_runner",
		},
		{
			name: "first host failure",
			result: subprocess.CommandResult{
				Started:     true,
				ExitCode:    11,
				HasExitCode: true,
				Err:         errors.New("marketplace upgrade failed"),
			},
			wantReason:    "nonzero_exit",
			wantAttempted: true,
			wantPersisted: true,
		},
		{
			name: "host reported partial plugin error",
			result: subprocess.CommandResult{
				Started:     true,
				Stdout:      `{"marketplace_upgraded":true,"plugin_errors":["sibling"]}`,
				ExitCode:    12,
				HasExitCode: true,
				Err:         errors.New("one installed plugin cache failed"),
			},
			wantReason:    "nonzero_exit",
			wantAttempted: true,
			wantPersisted: true,
		},
		{
			name: "local marketplace not upgradable",
			result: subprocess.CommandResult{
				Started:     true,
				ExitCode:    13,
				HasExitCode: true,
				Err:         errors.New("marketplace does not support upgrade"),
			},
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
			wantReason:    "canceled",
			wantAttempted: true,
			wantPersisted: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifestPath := writeCodexCLIRefreshFixture(t)
			report := runHostRouteRefreshCLI(
				t,
				manifestPath,
				"documents",
				false,
				func(
					_ context.Context,
					request subprocess.CommandRequest,
				) subprocess.CommandResult {
					if slices.Contains(request.Args, "add") ||
						slices.Contains(request.Args, "documents") {
						t.Fatalf("refresh fell back to plugin add: %#v", request.Args)
					}
					return test.result
				},
				1,
			)
			if report.Result.Class != "failed" ||
				report.Result.Attempted != test.wantAttempted ||
				report.Result.ProcessOutcome == nil ||
				report.Result.ProcessOutcome.Reason != test.wantReason ||
				report.Result.AttemptHistory.Persisted != test.wantPersisted {
				t.Fatalf("report = %#v", report)
			}
		})
	}
}

func writeCodexCLIRefreshFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex-home")
	if err := os.Mkdir(codexHome, 0o700); err != nil {
		t.Fatalf("Mkdir Codex home: %v", err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	if err := os.WriteFile(
		filepath.Join(codexHome, "config.toml"),
		[]byte("[plugins.\"documents@openai-primary-runtime\"]\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile Codex config: %v", err)
	}
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(manifestPath, []byte(`version = 1
targets = ["codex"]

[[extension]]
id = "documents"
carrier = "codex-plugin"
scope = "global"
source = { marketplace = "documents@openai-primary-runtime" }
`), 0o600); err != nil {
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
