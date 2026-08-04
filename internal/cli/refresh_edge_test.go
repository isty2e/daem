package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/subprocess"
	refreshworkflow "github.com/isty2e/daem/internal/workflow/refresh"
)

func TestRefreshEdgeRound3CLIIngressAndStreams(t *testing.T) {
	t.Run("distinct target selectors are rejected before planning", func(t *testing.T) {
		assertRefreshCLIMisuseBeforeAttempt(
			t,
			[]string{
				"refresh", "extension", "formatter",
				"--target", "opencode",
				"--target", "claude-code",
				"--dry-run",
			},
			"at most one distinct --target",
		)
	})

	t.Run("distinct scope selectors are rejected before planning", func(t *testing.T) {
		assertRefreshCLIMisuseBeforeAttempt(
			t,
			[]string{
				"refresh", "extension", "formatter",
				"--scope", "project",
				"--scope", "global",
				"--dry-run",
			},
			"--scope accepts at most one distinct scope",
		)
	})

	t.Run("unknown flag is grammar misuse without an envelope", func(t *testing.T) {
		assertRefreshCLIMisuseBeforeAttempt(
			t,
			[]string{
				"refresh", "extension", "formatter",
				"--refresh-everything",
			},
			"flag provided but not defined",
		)
	})

	t.Run("failed JSON authorization disclosure prevents launch", func(t *testing.T) {
		manifestPath := writeCLIRefreshFixture(t)
		calls := 0
		options := refreshCLIRunOptions(t, &calls)
		var stdout bytes.Buffer
		options.Stdout = &stdout
		options.Stderr = errorWriter{err: errors.New("stderr closed")}
		exitCode := RunWithOptions([]string{
			"refresh", "extension", "formatter",
			"--manifest", manifestPath,
			"--yes",
			"--json",
		}, options)
		if exitCode != 1 || calls != 0 || stdout.Len() != 0 {
			t.Fatalf(
				"exitCode=%d calls=%d stdout=%q",
				exitCode,
				calls,
				stdout.String(),
			)
		}
	})

	t.Run("failed JSON result write reports one bounded diagnostic", func(t *testing.T) {
		manifestPath := writeCLIRefreshFixture(t)
		calls := 0
		options := refreshCLIRunOptions(t, &calls)
		var stderr bytes.Buffer
		options.Stdout = errorWriter{err: errors.New("stdout closed")}
		options.Stderr = &stderr
		exitCode := RunWithOptions([]string{
			"refresh", "extension", "formatter",
			"--manifest", manifestPath,
			"--dry-run",
			"--json",
		}, options)
		if exitCode != 1 ||
			calls != 0 ||
			!strings.Contains(stderr.String(), "output failed: stdout closed") ||
			strings.Count(stderr.String(), "\n") != 1 {
			t.Fatalf(
				"exitCode=%d calls=%d stderr=%q",
				exitCode,
				calls,
				stderr.String(),
			)
		}
	})

	t.Run("interactive EOF declines without launching", func(t *testing.T) {
		manifestPath := writeCLIRefreshFixture(t)
		calls := 0
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		options := interactiveRunOptions(
			strings.NewReader(""),
			&stdout,
			&stderr,
		)
		injectCLIRefreshDependencies(t, &options, &calls)
		exitCode := RunWithOptions([]string{
			"refresh", "extension", "formatter",
			"--manifest", manifestPath,
		}, options)
		if exitCode != 1 ||
			calls != 0 ||
			!strings.Contains(stdout.String(), "class=cancelled") {
			t.Fatalf(
				"exitCode=%d calls=%d stdout=%q stderr=%q",
				exitCode,
				calls,
				stdout.String(),
				stderr.String(),
			)
		}
	})

	t.Run("ordinary host exit 130 is not process signal identity", func(t *testing.T) {
		manifestPath := writeCLIRefreshFixture(t)
		calls := 0
		options := refreshCLIRunOptions(t, &calls)
		options.RefreshExecuteOptions.CommandOptions.Runner = func(
			context.Context,
			subprocess.CommandRequest,
		) subprocess.CommandResult {
			calls++
			return subprocess.CommandResult{
				Started:     true,
				ExitCode:    130,
				HasExitCode: true,
			}
		}
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		options.Stdout = &stdout
		options.Stderr = &stderr
		exitCode := RunWithOptions([]string{
			"refresh", "extension", "formatter",
			"--manifest", manifestPath,
			"--yes",
		}, options)
		if exitCode != 1 || calls != 1 {
			t.Fatalf(
				"exitCode=%d calls=%d stdout=%q stderr=%q",
				exitCode,
				calls,
				stdout.String(),
				stderr.String(),
			)
		}
		if strings.Contains(
			stderr.String(),
			"refresh failed: refresh failed:",
		) {
			t.Fatalf("duplicated command diagnostic: %q", stderr.String())
		}
	})

	t.Run("JSON authorization and result omit selected local paths", func(t *testing.T) {
		manifestPath := writeCLIRefreshFixture(t)
		calls := 0
		options := refreshCLIRunOptions(t, &calls)
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		options.Stdout = &stdout
		options.Stderr = &stderr
		exitCode := RunWithOptions([]string{
			"refresh", "extension", "formatter",
			"--manifest", manifestPath,
			"--yes",
			"--json",
		}, options)
		if exitCode != 0 || calls != 1 {
			t.Fatalf(
				"exitCode=%d calls=%d stdout=%q stderr=%q",
				exitCode,
				calls,
				stdout.String(),
				stderr.String(),
			)
		}
		if strings.Contains(stdout.String(), manifestPath) ||
			strings.Contains(stderr.String(), manifestPath) {
			t.Fatalf(
				"machine-local path leaked: stdout=%q stderr=%q",
				stdout.String(),
				stderr.String(),
			)
		}
	})
}

func TestRefreshExitCodePreservesOnlyObservedSignalIdentity(t *testing.T) {
	exit130 := 130
	exit143 := 143
	tests := []struct {
		name   string
		result refreshworkflow.CommandResult
		want   int
	}{
		{
			name: "success",
			result: refreshworkflow.CommandResult{
				ResultClass: refreshworkflow.ResultAttemptedUnverified,
			},
			want: 0,
		},
		{
			name: "ordinary 130",
			result: refreshworkflow.CommandResult{
				ResultClass: refreshworkflow.ResultFailed,
				ProcessOutcome: &refreshworkflow.ProcessOutcome{
					ExitCode: &exit130,
				},
			},
			want: 1,
		},
		{
			name: "signaled 130",
			result: refreshworkflow.CommandResult{
				ResultClass: refreshworkflow.ResultPartial,
				ProcessOutcome: &refreshworkflow.ProcessOutcome{
					ExitCode: &exit130,
					Signaled: true,
				},
			},
			want: 130,
		},
		{
			name: "cancelled 143",
			result: refreshworkflow.CommandResult{
				ResultClass: refreshworkflow.ResultPartial,
				ProcessOutcome: &refreshworkflow.ProcessOutcome{
					ExitCode:  &exit143,
					Cancelled: true,
				},
			},
			want: 143,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := refreshExitCode(test.result); got != test.want {
				t.Fatalf("refreshExitCode() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestRefreshJSONExecutionFailureKeepsProseOutOfAuthorizationStream(t *testing.T) {
	manifestPath := writeCLIRefreshFixture(t)
	calls := 0
	options := refreshCLIRunOptions(t, &calls)
	const secret = "refresh-execution-secret"
	options.RefreshExecuteOptions.CommandOptions.Runner = func(
		context.Context,
		subprocess.CommandRequest,
	) subprocess.CommandResult {
		calls++
		return subprocess.CommandResult{
			Started:     true,
			ExitCode:    17,
			HasExitCode: true,
			Err:         errors.New("token=" + secret + " host rejected request"),
		}
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options.Stdout = &stdout
	options.Stderr = &stderr

	exitCode := RunWithOptions([]string{
		"refresh", "extension", "formatter",
		"--manifest", manifestPath,
		"--yes",
		"--json",
	}, options)
	if exitCode != 1 || calls != 1 {
		t.Fatalf(
			"exitCode=%d calls=%d stdout=%q stderr=%q",
			exitCode,
			calls,
			stdout.String(),
			stderr.String(),
		)
	}
	var authorization map[string]json.RawMessage
	if err := json.Unmarshal(stderr.Bytes(), &authorization); err != nil {
		t.Fatalf("stderr is not one authorization JSON document: %v\n%s", err, stderr.String())
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
		t.Fatalf("stdout is not one result JSON document: %v\n%s", err, stdout.String())
	}
	if report.Result.Class != "failed" ||
		report.Result.ReasonCode != "command_failed" ||
		report.Result.Detail == "" ||
		!report.HasErrors {
		t.Fatalf("report = %#v", report)
	}
	if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
		t.Fatalf("secret leaked: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "host rejected request") {
		t.Fatalf("raw host error detail leaked into result: %q", stdout.String())
	}
}

func TestRefreshJSONPlanningFailureRedactsMachineLocalDetail(t *testing.T) {
	manifestPath := writeCLIRefreshFixture(t)
	calls := 0
	options := refreshCLIRunOptions(t, &calls)
	const (
		hostPath = "/Users/alice/private,token/adapter.json"
		secret   = "planning-secret"
	)
	options.RefreshPlanOptions.CommandBuilder = func(
		refreshworkflow.CommandBuildInput,
	) (refreshworkflow.CommandSpec, error) {
		return refreshworkflow.CommandSpec{}, errors.New(
			"read " + hostPath + ": private_token=" + secret,
		)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options.Stdout = &stdout
	options.Stderr = &stderr

	exitCode := RunWithOptions([]string{
		"refresh", "extension", "formatter",
		"--manifest", manifestPath,
		"--dry-run",
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
		t.Fatalf("stdout is not one result JSON document: %v\n%s", err, stdout.String())
	}
	if report.Result.Class != "refused" ||
		report.Result.ReasonCode != "refresh_unsupported" ||
		!strings.Contains(report.Result.Detail, "[REDACTED]") ||
		!report.HasErrors {
		t.Fatalf("report = %#v", report)
	}
	if strings.Contains(stdout.String(), hostPath) ||
		strings.Contains(stdout.String(), "private,token") ||
		strings.Contains(stdout.String(), "adapter.json") ||
		strings.Contains(stdout.String(), secret) {
		t.Fatalf("private detail leaked: %q", stdout.String())
	}
}

func assertRefreshCLIMisuseBeforeAttempt(
	t *testing.T,
	args []string,
	wantDiagnostic string,
) {
	t.Helper()
	calls := 0
	options := refreshCLIRunOptions(t, &calls)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options.Stdout = &stdout
	options.Stderr = &stderr
	exitCode := RunWithOptions(args, options)
	if exitCode != 2 ||
		calls != 0 ||
		stdout.Len() != 0 ||
		!strings.Contains(stderr.String(), wantDiagnostic) {
		t.Fatalf(
			"exitCode=%d calls=%d stdout=%q stderr=%q, want %q",
			exitCode,
			calls,
			stdout.String(),
			stderr.String(),
			wantDiagnostic,
		)
	}
}
