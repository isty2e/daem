package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/subprocess"
	refreshworkflow "github.com/isty2e/daem/internal/workflow/refresh"
)

func TestRefreshTimeoutCLIRejectsInvalidExplicitDurationsBeforePlanning(t *testing.T) {
	manifestPath := writeCLIRefreshFixture(t)
	tests := []struct {
		name  string
		value string
	}{
		{name: "explicit zero", value: "0"},
		{name: "below minimum", value: "500ms"},
		{name: "fractional second", value: "1500ms"},
		{name: "above maximum", value: "1h1s"},
		{name: "negative", value: "-1s"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
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
				"--timeout=" + test.value,
				"--dry-run",
				"--json",
			}, options)
			if exitCode != 2 ||
				calls != 0 ||
				stdout.Len() != 0 ||
				!strings.Contains(stderr.String(), "--timeout") {
				t.Fatalf(
					"exitCode=%d calls=%d stdout=%q stderr=%q",
					exitCode,
					calls,
					stdout.String(),
					stderr.String(),
				)
			}
		})
	}
}

func TestRefreshTimeoutCLIDisclosesExactDefaultAndBounds(t *testing.T) {
	manifestPath := writeCLIRefreshFixture(t)
	tests := []struct {
		name        string
		timeoutFlag string
		wantSeconds int
	}{
		{
			name:        "default",
			wantSeconds: int(refreshworkflow.DefaultHostCommandTimeout / time.Second),
		},
		{
			name:        "minimum",
			timeoutFlag: "1s",
			wantSeconds: 1,
		},
		{
			name:        "maximum",
			timeoutFlag: "1h",
			wantSeconds: 3600,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			options := refreshCLIRunOptions(t, &calls)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			options.Stdout = &stdout
			options.Stderr = &stderr
			args := []string{
				"refresh",
				"extension",
				"formatter",
				"--manifest",
				manifestPath,
				"--dry-run",
				"--json",
			}
			if test.timeoutFlag != "" {
				args = append(args, "--timeout="+test.timeoutFlag)
			}

			exitCode := RunWithOptions(args, options)
			if exitCode != 0 || calls != 0 {
				t.Fatalf(
					"exitCode=%d calls=%d stdout=%q stderr=%q",
					exitCode,
					calls,
					stdout.String(),
					stderr.String(),
				)
			}
			var report hostRouteRefreshReport
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
				t.Fatalf("Unmarshal refresh report: %v\n%s", err, stdout.String())
			}
			if report.Disclosure.TimeoutSeconds != test.wantSeconds {
				t.Fatalf(
					"timeout_seconds=%d, want %d",
					report.Disclosure.TimeoutSeconds,
					test.wantSeconds,
				)
			}
		})
	}
}

func TestRefreshProgressIsTTYOnlyAndSuppressedFromMachineOutput(t *testing.T) {
	manifestPath := writeCLIRefreshFixture(t)
	tests := []struct {
		name           string
		jsonOutput     bool
		stderrTerminal bool
		wantProgress   bool
	}{
		{name: "human terminal", stderrTerminal: true, wantProgress: true},
		{name: "human redirected"},
		{name: "json terminal", jsonOutput: true, stderrTerminal: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			options := refreshCLIRunOptions(t, &calls)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			options.Stdout = &stdout
			options.Stderr = &stderr
			options.StderrIsTerminal = test.stderrTerminal
			args := []string{
				"refresh",
				"extension",
				"formatter",
				"--manifest",
				manifestPath,
				"--timeout=2m",
				"--yes",
			}
			if test.jsonOutput {
				args = append(args, "--json")
			}

			exitCode := RunWithOptions(args, options)
			if exitCode != 0 || calls != 1 {
				t.Fatalf(
					"exitCode=%d calls=%d stdout=%q stderr=%q",
					exitCode,
					calls,
					stdout.String(),
					stderr.String(),
				)
			}
			hasProgress := strings.Contains(
				stderr.String(),
				"Refreshing extension formatter (timeout 2m0s)",
			)
			if hasProgress != test.wantProgress {
				t.Fatalf(
					"hasProgress=%t, want %t; stderr=%q",
					hasProgress,
					test.wantProgress,
					stderr.String(),
				)
			}
			if test.wantProgress &&
				(!strings.HasSuffix(stderr.String(), "\r\x1b[2K") ||
					strings.Count(stderr.String(), "Refreshing extension") != 1) {
				t.Fatalf("progress was not emitted once and cleared: %q", stderr.String())
			}
		})
	}
}

func TestRefreshProgressClearsBeforePartialOutcomeAndDiagnostic(t *testing.T) {
	manifestPath := writeCLIRefreshFixture(t)
	calls := 0
	options := refreshCLIRunOptions(t, &calls)
	var output bytes.Buffer
	options.Stdout = &output
	options.Stderr = &output
	options.StderrIsTerminal = true
	options.RefreshExecuteOptions.CommandOptions.Runner = func(
		_ context.Context,
		_ subprocess.CommandRequest,
	) subprocess.CommandResult {
		calls++
		return subprocess.CommandResult{
			Started:  true,
			Canceled: true,
			Err:      context.Canceled,
		}
	}

	exitCode := RunWithOptions([]string{
		"refresh",
		"extension",
		"formatter",
		"--manifest",
		manifestPath,
		"--yes",
	}, options)
	rendered := output.String()
	progressIndex := strings.Index(rendered, "Refreshing extension formatter")
	clearIndex := strings.LastIndex(rendered, "\r\x1b[2K")
	outcomeIndex := strings.LastIndex(rendered, "result: class=partial")
	diagnosticIndex := strings.LastIndex(rendered, "refresh failed:")
	if exitCode != 1 ||
		calls != 1 ||
		progressIndex < 0 ||
		clearIndex < progressIndex ||
		outcomeIndex < clearIndex ||
		diagnosticIndex < outcomeIndex {
		t.Fatalf(
			"exitCode=%d calls=%d indexes=(%d,%d,%d,%d) output=%q",
			exitCode,
			calls,
			progressIndex,
			clearIndex,
			outcomeIndex,
			diagnosticIndex,
			rendered,
		)
	}
}
