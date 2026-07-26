package cli

import (
	"bytes"
	"context"
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
				ResultClass: refreshworkflow.ResultFailed,
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
				ResultClass: refreshworkflow.ResultFailed,
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
