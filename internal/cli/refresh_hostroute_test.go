package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/isty2e/daem/internal/subprocess"
	refreshworkflow "github.com/isty2e/daem/internal/workflow/refresh"
)

type hostRouteRefreshReport struct {
	Mode      string `json:"mode"`
	Selection struct {
		ID    string `json:"id"`
		Scope string `json:"scope"`
	} `json:"selection"`
	Route struct {
		RouteID                string `json:"route_id"`
		AdapterContractVersion string `json:"adapter_contract_version"`
		ExecutionSubject       string `json:"execution_subject"`
		ObservationPosture     string `json:"observation_posture"`
	} `json:"route"`
	Disclosure struct {
		Command               string   `json:"command"`
		Args                  []string `json:"args"`
		TimeoutSeconds        int      `json:"timeout_seconds"`
		EffectClasses         []string `json:"effect_classes"`
		RetainedEffectClasses []string `json:"retained_effect_classes"`
		NonClaims             []string `json:"non_claims"`
	} `json:"disclosure"`
	Result struct {
		Class          string `json:"class"`
		ReasonCode     string `json:"reason_code"`
		Detail         string `json:"detail"`
		Attempted      bool   `json:"attempted"`
		ProcessOutcome *struct {
			Reason   string `json:"reason"`
			Redacted bool   `json:"redacted"`
		} `json:"process_outcome"`
		AttemptHistory struct {
			Persisted bool `json:"persisted"`
		} `json:"attempt_history"`
	} `json:"result"`
}

func runHostRouteRefreshCLI(
	t *testing.T,
	manifestPath string,
	extensionID string,
	dryRun bool,
	runner subprocess.CommandRunner,
	wantExit int,
) hostRouteRefreshReport {
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
		extensionID,
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
	var report hostRouteRefreshReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("Unmarshal refresh report: %v\n%s", err, stdout.String())
	}
	if wantExit != 0 && report.Result.Detail == "" {
		t.Fatalf("failed refresh omitted result detail: %#v", report.Result)
	}
	return report
}
