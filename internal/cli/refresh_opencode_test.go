package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
	refreshworkflow "github.com/isty2e/daem/internal/workflow/refresh"
)

func TestOpenCodeRefreshCLIUsesRealAdapterAndRemainsRepeatable(t *testing.T) {
	manifestPath := writeCLIRefreshFixture(t)
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

	dryRun := runOpenCodeRefreshCLI(t, manifestPath, true, nil)
	if dryRun.Mode != "dry_run" ||
		dryRun.Result.Class != "planned" ||
		dryRun.Disclosure.Command != "opencode" ||
		!slices.Equal(
			dryRun.Disclosure.Args,
			[]string{"plugin", "@acme/formatter", "--force"},
		) ||
		dryRun.Route.ObservationPosture != "attempt_when_unsupported" {
		t.Fatalf("dry-run report = %#v", dryRun)
	}

	calls := 0
	runner := func(
		_ context.Context,
		request subprocess.CommandRequest,
	) subprocess.CommandResult {
		calls++
		if request.Command != "opencode" ||
			!slices.Equal(
				request.Args,
				[]string{"plugin", "@acme/formatter", "--force"},
			) {
			t.Fatalf("OpenCode refresh request = %#v", request)
		}
		return subprocess.CommandResult{
			Started:     true,
			Stdout:      "token=opencode-refresh-secret " + string(bytes.Repeat([]byte("x"), 256)),
			Stderr:      "{malformed-host-output",
			ExitCode:    0,
			HasExitCode: true,
		}
	}
	first := runOpenCodeRefreshCLI(t, manifestPath, false, runner)
	second := runOpenCodeRefreshCLI(t, manifestPath, false, runner)
	for index, report := range []openCodeRefreshReport{first, second} {
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
		bytes.Contains(got, []byte("opencode-refresh-secret")) {
		t.Fatalf("statefile leaked host output: err=%v", err)
	}
}

func TestOpenCodeRefreshRejectsCredentialBearingForgedLockBeforeDisclosure(t *testing.T) {
	const secret = "do-not-print-private-token"
	tests := []struct {
		name       string
		source     string
		wantReason string
	}{
		{
			name:       "query field",
			source:     "https://example.test/plugin.tgz?private_token=" + secret,
			wantReason: "must not contain URL query fields",
		},
		{
			name:       "encoded fragment assignment",
			source:     "github:acme/plugin#private_token%3D" + secret,
			wantReason: "fragment must not contain assignments",
		},
		{
			name:       "malformed relative source with encoded assignment",
			source:     "./plugins/%zz#private_token%3D" + secret,
			wantReason: "extension source URL is malformed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifestPath := writeCLIRefreshFixture(t)
			paths, err := daempaths.Resolve(manifestPath)
			if err != nil {
				t.Fatalf("Resolve returned error: %v", err)
			}
			content, err := os.ReadFile(paths.LockfilePath)
			if err != nil {
				t.Fatalf("ReadFile lockfile: %v", err)
			}
			const original = `source_namespace = "host-source:@acme/formatter"`
			forged := `source_namespace = "host-source:` + test.source + `"`
			if count := bytes.Count(content, []byte(original)); count != 1 {
				t.Fatalf("source namespace occurrences = %d, want 1", count)
			}
			content = bytes.Replace(content, []byte(original), []byte(forged), 1)
			if err := os.WriteFile(paths.LockfilePath, content, 0o600); err != nil {
				t.Fatalf("WriteFile forged lockfile: %v", err)
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := RunWithOptions(
				[]string{
					"refresh",
					"extension",
					"formatter",
					"--manifest",
					manifestPath,
					"--dry-run",
					"--json",
				},
				RunOptions{Stdout: &stdout, Stderr: &stderr},
			)
			if exitCode != 1 {
				t.Fatalf("exitCode = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			var report hostRouteRefreshReport
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
				t.Fatalf("Unmarshal refused report: %v\n%s", err, stdout.String())
			}
			if report.Result.Class != "refused" ||
				report.Disclosure.Command != "" ||
				len(report.Disclosure.Args) != 0 {
				t.Fatalf("refused report disclosed invocation: %#v", report)
			}
			if !strings.Contains(report.Result.Detail, test.wantReason) {
				t.Fatalf("detail = %q, want %q", report.Result.Detail, test.wantReason)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty after JSON result", stderr.String())
			}
			if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
				t.Fatalf("credential value leaked: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestOpenCodeRefreshCLIReportsHostFailuresWithoutFallback(t *testing.T) {
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
			name: "nonzero host exit",
			result: subprocess.CommandResult{
				Started:     true,
				ExitCode:    19,
				HasExitCode: true,
				Err:         errors.New("exit status 19"),
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
			manifestPath := writeCLIRefreshFixture(t)
			report := runOpenCodeRefreshCLIExpectExit(
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

type openCodeRefreshReport struct {
	Mode  string `json:"mode"`
	Route struct {
		ObservationPosture string `json:"observation_posture"`
	} `json:"route"`
	Disclosure struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	} `json:"disclosure"`
	Result struct {
		Class          string `json:"class"`
		ReasonCode     string `json:"reason_code"`
		Detail         string `json:"detail"`
		Attempted      bool   `json:"attempted"`
		ProcessOutcome *struct {
			Reason    string `json:"reason"`
			Cancelled bool   `json:"cancelled"`
			Redacted  bool   `json:"redacted"`
		} `json:"process_outcome"`
		AttemptHistory struct {
			Persisted bool `json:"persisted"`
		} `json:"attempt_history"`
	} `json:"result"`
}

func runOpenCodeRefreshCLI(
	t *testing.T,
	manifestPath string,
	dryRun bool,
	runner subprocess.CommandRunner,
) openCodeRefreshReport {
	t.Helper()
	return runOpenCodeRefreshCLIExpectExit(
		t,
		manifestPath,
		dryRun,
		runner,
		0,
	)
}

func runOpenCodeRefreshCLIExpectExit(
	t *testing.T,
	manifestPath string,
	dryRun bool,
	runner subprocess.CommandRunner,
	wantExit int,
) openCodeRefreshReport {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options := RunOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}
	args := []string{
		"refresh",
		"extension",
		"formatter",
		"--manifest",
		manifestPath,
		"--json",
	}
	if dryRun {
		args = append(args, "--dry-run")
	} else {
		args = append(args, "--yes")
		options.RefreshExecuteOptions = refreshworkflow.ExecuteOptions{
			CommandOptions: subprocess.CommandOptions{
				OutputLimit: 32,
				Runner:      runner,
			},
		}
	}
	if exitCode := RunWithOptions(args, options); exitCode != wantExit {
		t.Fatalf(
			"RunWithOptions exit=%d want=%d stdout=%q stderr=%q",
			exitCode,
			wantExit,
			stdout.String(),
			stderr.String(),
		)
	}
	if dryRun {
		if stderr.Len() != 0 {
			t.Fatalf("dry-run stderr = %q, want empty", stderr.String())
		}
	} else {
		var authorization map[string]json.RawMessage
		if err := json.Unmarshal(stderr.Bytes(), &authorization); err != nil {
			t.Fatalf(
				"execution stderr is not one authorization JSON document: %v\n%s",
				err,
				stderr.String(),
			)
		}
	}
	var report openCodeRefreshReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("Unmarshal refresh report: %v\n%s", err, stdout.String())
	}
	if wantExit != 0 && report.Result.Detail == "" {
		t.Fatalf("failed refresh omitted result detail: %#v", report.Result)
	}
	return report
}
