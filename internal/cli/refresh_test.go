package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
	lockworkflow "github.com/isty2e/daem/internal/workflow/lock"
	refreshworkflow "github.com/isty2e/daem/internal/workflow/refresh"
)

func TestRefreshHelpAndGrammar(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantExit   int
		wantOutput string
	}{
		{
			name:       "group help",
			args:       []string{"help", "refresh"},
			wantExit:   0,
			wantOutput: "daem help refresh extension",
		},
		{
			name:       "leaf help",
			args:       []string{"help", "refresh", "extension"},
			wantExit:   0,
			wantOutput: "daem refresh extension <id>",
		},
		{
			name:       "bare group",
			args:       []string{"refresh"},
			wantExit:   2,
			wantOutput: "daem help refresh extension",
		},
		{
			name:       "unknown subject",
			args:       []string{"refresh", "plugin"},
			wantExit:   2,
			wantOutput: `unknown refresh subject "plugin"`,
		},
		{
			name:       "missing id",
			args:       []string{"refresh", "extension"},
			wantExit:   2,
			wantOutput: "extension id is required",
		},
		{
			name:       "multiple ids",
			args:       []string{"refresh", "extension", "x", "y", "--dry-run"},
			wantExit:   2,
			wantOutput: `unexpected argument "y"`,
		},
		{
			name:       "exclusive execution flags",
			args:       []string{"refresh", "extension", "x", "--dry-run", "--yes"},
			wantExit:   2,
			wantOutput: "--dry-run and --yes are mutually exclusive",
		},
		{
			name:       "json requires authorization mode",
			args:       []string{"refresh", "extension", "x", "--json"},
			wantExit:   2,
			wantOutput: "--json requires --dry-run or --yes",
		},
		{
			name:       "json verbose conflict",
			args:       []string{"refresh", "extension", "x", "--dry-run", "--json", "--verbose"},
			wantExit:   2,
			wantOutput: "--json and --verbose are mutually exclusive",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := runWithoutTerminal(test.args, &stdout, &stderr)
			if exitCode != test.wantExit {
				t.Fatalf(
					"exitCode = %d, stdout=%q stderr=%q",
					exitCode,
					stdout.String(),
					stderr.String(),
				)
			}
			if !strings.Contains(
				stdout.String()+stderr.String(),
				test.wantOutput,
			) {
				t.Fatalf(
					"stdout=%q stderr=%q, want %q",
					stdout.String(),
					stderr.String(),
					test.wantOutput,
				)
			}
		})
	}
}

func TestRefreshUnknownExtensionProducesTypedRefusal(t *testing.T) {
	manifestPath := writeCLIRefreshFixture(t)
	calls := 0
	options := refreshCLIRunOptions(t, &calls)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options.Stdout = &stdout
	options.Stderr = &stderr

	exitCode := RunWithOptions([]string{
		"refresh",
		"extension",
		"missing",
		"--manifest",
		manifestPath,
		"--dry-run",
		"--json",
	}, options)
	if exitCode != 1 || calls != 0 {
		t.Fatalf(
			"exitCode=%d calls=%d stdout=%q stderr=%q",
			exitCode,
			calls,
			stdout.String(),
			stderr.String(),
		)
	}
	var report struct {
		Result struct {
			Class      string `json:"class"`
			ReasonCode string `json:"reason_code"`
			Detail     string `json:"detail"`
		} `json:"result"`
		HasErrors bool `json:"has_errors"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("Unmarshal result: %v\n%s", err, stdout.String())
	}
	if report.Result.Class != "refused" ||
		report.Result.ReasonCode != "invalid_selection" ||
		!strings.Contains(report.Result.Detail, `extension id "missing" is not declared`) ||
		!report.HasErrors {
		t.Fatalf("report = %#v", report)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty after JSON result", stderr.String())
	}
}

func TestRefreshWritePlanJSONRefusalUsesOnlyResultEnvelope(t *testing.T) {
	manifestPath := writeCLIRefreshFixture(t)
	calls := 0
	options := refreshCLIRunOptions(t, &calls)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options.Stdout = &stdout
	options.Stderr = &stderr

	exitCode := RunWithOptions([]string{
		"refresh",
		"extension",
		"missing",
		"--manifest",
		manifestPath,
		"--yes",
		"--json",
	}, options)
	if exitCode != 1 || calls != 0 || stderr.Len() != 0 {
		t.Fatalf(
			"exitCode=%d calls=%d stdout=%q stderr=%q",
			exitCode,
			calls,
			stdout.String(),
			stderr.String(),
		)
	}
	var report struct {
		Result struct {
			Class      string `json:"class"`
			ReasonCode string `json:"reason_code"`
			Detail     string `json:"detail"`
		} `json:"result"`
		HasErrors bool `json:"has_errors"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("result is not one JSON document: %v\n%s", err, stdout.String())
	}
	if report.Result.Class != "refused" ||
		report.Result.ReasonCode != "invalid_selection" ||
		report.Result.Detail == "" ||
		!report.HasErrors {
		t.Fatalf("report = %#v", report)
	}
}

func TestOrdinaryCommandsNeverInvokeRefreshBuilder(t *testing.T) {
	tests := []struct {
		name string
		args func(string) []string
	}{
		{
			name: "lock",
			args: func(manifestPath string) []string {
				return []string{"lock", "--manifest", manifestPath}
			},
		},
		{
			name: "outdated",
			args: func(manifestPath string) []string {
				return []string{"outdated", "--manifest", manifestPath}
			},
		},
		{
			name: "status",
			args: func(manifestPath string) []string {
				return []string{"status", "--manifest", manifestPath}
			},
		},
		{
			name: "apply dry-run",
			args: func(manifestPath string) []string {
				return []string{"apply", "--dry-run", "--manifest", manifestPath}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifestPath := writeCLIRefreshFixture(t)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			options := RunOptions{
				Stdout: &stdout,
				Stderr: &stderr,
				RefreshPlanOptions: refreshworkflow.PlanOptions{
					CommandBuilder: func(
						refreshworkflow.CommandBuildInput,
					) (refreshworkflow.CommandSpec, error) {
						t.Fatal("ordinary command invoked refresh builder")
						return refreshworkflow.CommandSpec{}, nil
					},
				},
			}
			exitCode := RunWithOptions(test.args(manifestPath), options)
			if exitCode != 0 {
				t.Fatalf(
					"exitCode=%d stdout=%q stderr=%q",
					exitCode,
					stdout.String(),
					stderr.String(),
				)
			}
		})
	}
}

func TestRefreshDryRunJSONUsesFrozenTopLevelSchema(t *testing.T) {
	manifestPath := writeCLIRefreshFixture(t)
	calls := 0
	options := refreshCLIRunOptions(t, &calls)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options.Stdout = &stdout
	options.Stderr = &stderr

	exitCode := RunWithOptions([]string{
		"refresh",
		"extension",
		"formatter",
		"--manifest",
		manifestPath,
		"--dry-run",
		"--json",
	}, options)
	if exitCode != 0 {
		t.Fatalf(
			"exitCode = %d, stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	if calls != 0 {
		t.Fatalf("runner calls = %d, want 0", calls)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("Unmarshal returned error: %v\n%s", err, stdout.String())
	}
	keys := make([]string, 0, len(document))
	for key := range document {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	wantKeys := []string{
		"command",
		"disclosure",
		"has_errors",
		"mode",
		"result",
		"route",
		"schema_version",
		"selection",
	}
	if !slices.Equal(keys, wantKeys) {
		t.Fatalf("top-level keys = %v, want %v", keys, wantKeys)
	}
	if strings.Contains(stdout.String(), manifestPath) {
		t.Fatal("refresh JSON leaked a machine-local manifest path")
	}
}

func TestRefreshInteractiveDeclineNeverRunsHost(t *testing.T) {
	manifestPath := writeCLIRefreshFixture(t)
	calls := 0
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options := interactiveRunOptions(
		strings.NewReader("no\n"),
		&stdout,
		&stderr,
	)
	injectCLIRefreshDependencies(t, &options, &calls)

	exitCode := RunWithOptions([]string{
		"refresh",
		"extension",
		"formatter",
		"--manifest",
		manifestPath,
	}, options)
	if exitCode != 1 {
		t.Fatalf(
			"exitCode = %d, stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	if calls != 0 {
		t.Fatalf("runner calls = %d, want 0", calls)
	}
	if !strings.Contains(stdout.String(), "result: class=cancelled") ||
		!strings.Contains(stderr.String(), "Proceed with refresh? [y/N]:") ||
		!strings.Contains(stderr.String(), "refresh canceled") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRefreshYesJSONDisclosesBeforeOneExactAttempt(t *testing.T) {
	manifestPath := writeCLIRefreshFixture(t)
	calls := 0
	options := refreshCLIRunOptions(t, &calls)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options.Stdout = &stdout
	options.Stderr = &stderr

	exitCode := RunWithOptions([]string{
		"refresh",
		"extension",
		"formatter",
		"--manifest",
		manifestPath,
		"--yes",
		"--json",
	}, options)
	if exitCode != 0 {
		t.Fatalf(
			"exitCode = %d, stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	if calls != 1 {
		t.Fatalf("runner calls = %d, want 1", calls)
	}
	var authorization map[string]json.RawMessage
	if err := json.Unmarshal(stderr.Bytes(), &authorization); err != nil {
		t.Fatalf("authorization disclosure is not JSON: %v\n%s", err, stderr.String())
	}
	var result struct {
		Result struct {
			Class     string `json:"class"`
			Attempted bool   `json:"attempted"`
		} `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("result is not one JSON document: %v\n%s", err, stdout.String())
	}
	if result.Result.Class != "attempted_unverified" ||
		!result.Result.Attempted {
		t.Fatalf("result = %#v", result)
	}
}

func TestRefreshYesDisclosureFailurePreventsHostAttempt(t *testing.T) {
	manifestPath := writeCLIRefreshFixture(t)
	calls := 0
	var stderr bytes.Buffer
	options := refreshCLIRunOptions(t, &calls)
	options.Stdout = errorWriter{err: errors.New("stdout closed")}
	options.Stderr = &stderr

	exitCode := RunWithOptions([]string{
		"refresh",
		"extension",
		"formatter",
		"--manifest",
		manifestPath,
		"--yes",
	}, options)
	if exitCode != 1 {
		t.Fatalf("exitCode = %d, stderr=%q", exitCode, stderr.String())
	}
	if calls != 0 {
		t.Fatalf("runner calls = %d, want 0", calls)
	}
	if !strings.Contains(stderr.String(), "disclose plan") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRefreshNonInteractiveMutationRequiresYesBeforePlanning(t *testing.T) {
	manifestPath := writeCLIRefreshFixture(t)
	calls := 0
	options := refreshCLIRunOptions(t, &calls)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options.Stdout = &stdout
	options.Stderr = &stderr

	exitCode := RunWithOptions([]string{
		"refresh",
		"extension",
		"formatter",
		"--manifest",
		manifestPath,
	}, options)
	if exitCode != 2 {
		t.Fatalf(
			"exitCode = %d, stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	if calls != 0 || stdout.Len() != 0 {
		t.Fatalf("runner calls=%d stdout=%q", calls, stdout.String())
	}
}

func refreshCLIRunOptions(t *testing.T, calls *int) RunOptions {
	t.Helper()
	options := RunOptions{}
	injectCLIRefreshDependencies(t, &options, calls)
	return options
}

func injectCLIRefreshDependencies(
	t *testing.T,
	options *RunOptions,
	calls *int,
) {
	t.Helper()
	disclosure, err := executehostroute.NewDisclosure(
		executehostroute.DisclosureInput{
			ExecutionSubject:      "synthetic formatter package",
			InvocationKind:        executehostroute.InvocationKindCommand,
			CWDPolicy:             executehostroute.CWDPolicySelectedRoot,
			EffectClasses:         []string{"host_package_refresh"},
			RetainedEffectClasses: []string{"host_cache"},
			NonClaims:             []string{"no_rollback"},
		},
	)
	if err != nil {
		t.Fatalf("NewDisclosure returned error: %v", err)
	}
	options.RefreshPlanOptions = refreshworkflow.PlanOptions{
		CommandBuilder: func(
			input refreshworkflow.CommandBuildInput,
		) (refreshworkflow.CommandSpec, error) {
			if input.Operation != lock.OperationRefresh {
				return refreshworkflow.CommandSpec{}, errors.New(
					"synthetic CLI adapter received non-refresh operation",
				)
			}
			return refreshworkflow.NewCommandSpec(
				subprocess.CommandAttemptRequest{
					Command: "synthetic-host",
					Args:    []string{"refresh", "formatter"},
					WorkDir: input.WorkDir,
				},
				disclosure,
			)
		},
	}
	options.RefreshExecuteOptions = refreshworkflow.ExecuteOptions{
		CommandOptions: subprocess.CommandOptions{
			Runner: func(
				_ context.Context,
				request subprocess.CommandRequest,
			) subprocess.CommandResult {
				*calls++
				if request.Command != "synthetic-host" ||
					!slices.Equal(
						request.Args,
						[]string{"refresh", "formatter"},
					) {
					return subprocess.CommandResult{
						Started: true,
						Err:     errors.New("unexpected synthetic request"),
					}
				}
				return subprocess.CommandResult{
					Started:     true,
					ExitCode:    0,
					HasExitCode: true,
				}
			},
		},
	}
}

func writeCLIRefreshFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(manifestPath, []byte(`version = 1
targets = ["opencode"]

[[extension]]
id = "formatter"
carrier = "opencode-plugin"
scope = "project"
source = { host_source = "@acme/formatter" }
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
