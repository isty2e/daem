package refresh

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
)

func TestClaudeRefreshUsesRealAdapterForExactExternalRelation(t *testing.T) {
	manifestPath, _ := writeClaudeExternalRefreshFixture(t)
	manifestBefore := readTestFile(t, manifestPath)
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	lockBefore := readTestFile(t, paths.LockfilePath)

	prepared := planClaudeRefresh(t, manifestPath)
	disclosure := prepared.Disclosure()
	if disclosure.Route.ObservationPosture != PostureRequireCurrent ||
		disclosure.Observation == nil ||
		disclosure.Observation.State != observerelation.StateExactCorrelation ||
		disclosure.Disclosure.Invocation.Command != "claude" ||
		!slices.Equal(
			disclosure.Disclosure.Invocation.Args,
			[]string{"plugin", "update", "context7@market", "--scope", "project"},
		) {
		t.Fatalf("disclosure = %#v", disclosure)
	}

	calls := 0
	result, err := Execute(context.Background(), prepared, ExecuteOptions{
		CommandOptions: subprocess.CommandOptions{
			Runner: func(
				_ context.Context,
				request subprocess.CommandRequest,
			) subprocess.CommandResult {
				calls++
				assertClaudeRefreshRequest(t, request)
				return subprocess.CommandResult{
					Started:     true,
					Stdout:      "token=claude-refresh-secret",
					Stderr:      "{malformed-host-output",
					ExitCode:    0,
					HasExitCode: true,
				}
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if calls != 1 ||
		result.ResultClass != ResultObservedRelation ||
		result.Observation == nil ||
		result.Observation.State != observerelation.StateExactCorrelation ||
		result.ProcessOutcome == nil ||
		!result.ProcessOutcome.Redacted ||
		!result.AttemptHistory.Persisted {
		t.Fatalf(
			"calls=%d result=%#v process=%#v observation=%#v",
			calls,
			result,
			result.ProcessOutcome,
			result.Observation,
		)
	}
	if got := readTestFile(t, manifestPath); !slices.Equal(got, manifestBefore) {
		t.Fatal("Claude refresh changed manifest bytes")
	}
	if got := readTestFile(t, paths.LockfilePath); !slices.Equal(got, lockBefore) {
		t.Fatal("Claude refresh changed lockfile bytes")
	}
	retry, err := PlanDryRun(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "context7",
	}, PlanOptions{})
	if err != nil || retry.ResultClass != ResultPlanned {
		t.Fatalf("retry dry-run result=%#v err=%v", retry, err)
	}
	if got := readTestFile(t, paths.StatefilePath); bytes.Contains(
		got,
		[]byte("claude-refresh-secret"),
	) {
		t.Fatal("Claude refresh persisted hostile host output")
	}
}

func TestClaudeRefreshCommandFailureCannotBorrowCurrentRelationSuccess(t *testing.T) {
	manifestPath, _ := writeClaudeExternalRefreshFixture(t)
	prepared := planClaudeRefresh(t, manifestPath)

	result, err := Execute(context.Background(), prepared, ExecuteOptions{
		CommandOptions: subprocess.CommandOptions{
			Runner: func(
				_ context.Context,
				request subprocess.CommandRequest,
			) subprocess.CommandResult {
				assertClaudeRefreshRequest(t, request)
				return subprocess.CommandResult{
					Started:     true,
					Stderr:      "password=claude-failure-secret",
					ExitCode:    23,
					HasExitCode: true,
					Err:         errors.New("exit status 23"),
				}
			},
		},
	})
	if err == nil ||
		result.ResultClass != ResultFailed ||
		result.ReasonCode != ReasonCommandFailed ||
		result.Observation == nil ||
		result.Observation.State != observerelation.StateExactCorrelation ||
		result.ProcessOutcome == nil ||
		result.ProcessOutcome.ExitCode == nil ||
		*result.ProcessOutcome.ExitCode != 23 ||
		result.FailureDetail() != "delegated host command result: nonzero_exit" ||
		!result.ProcessOutcome.Redacted ||
		!result.AttemptHistory.Persisted {
		t.Fatalf("result=%#v err=%v", result, err)
	}

	retry, retryErr := PlanDryRun(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "context7",
	}, PlanOptions{})
	if retryErr != nil || retry.ResultClass != ResultPlanned {
		t.Fatalf("retry dry-run result=%#v err=%v", retry, retryErr)
	}
}

func TestClaudeRefreshPostObservationLossIsPartial(t *testing.T) {
	manifestPath, inventoryPath := writeClaudeExternalRefreshFixture(t)
	prepared := planClaudeRefresh(t, manifestPath)

	result, err := Execute(context.Background(), prepared, ExecuteOptions{
		CommandOptions: subprocess.CommandOptions{
			Runner: func(
				_ context.Context,
				request subprocess.CommandRequest,
			) subprocess.CommandResult {
				assertClaudeRefreshRequest(t, request)
				if writeErr := os.WriteFile(
					inventoryPath,
					[]byte(`{"version":2,"plugins":{}}`),
					0o600,
				); writeErr != nil {
					t.Fatalf("WriteFile empty Claude inventory: %v", writeErr)
				}
				return subprocess.CommandResult{
					Started:     true,
					ExitCode:    0,
					HasExitCode: true,
				}
			},
		},
	})
	if err == nil ||
		result.ResultClass != ResultPartial ||
		result.ReasonCode != ReasonPostObservationFailed ||
		result.Observation == nil ||
		result.Observation.State != observerelation.StateMissing ||
		!result.AttemptHistory.Persisted {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestClaudeRefreshRefusesFreshAbsenceBeforeCommandConstruction(t *testing.T) {
	manifestPath, inventoryPath := writeClaudeExternalRefreshFixture(t)
	if err := os.WriteFile(
		inventoryPath,
		[]byte(`{"version":2,"plugins":{}}`),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile empty Claude inventory: %v", err)
	}

	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "context7",
	}, PlanOptions{})
	if err == nil {
		t.Fatal("PlanWrite returned nil error")
	}
	t.Cleanup(func() { _ = prepared.Close() })
	result := prepared.Disclosure()
	if result.ResultClass != ResultRefused ||
		result.ReasonCode != ReasonRelationMissing ||
		result.Attempted {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func writeClaudeExternalRefreshFixture(t *testing.T) (string, string) {
	t.Helper()
	manifestPath, _ := writeObservedRefreshFixture(t)
	configRoot := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
	inventoryPath := filepath.Join(
		configRoot,
		"plugins",
		"installed_plugins.json",
	)
	if err := os.MkdirAll(filepath.Dir(inventoryPath), 0o700); err != nil {
		t.Fatalf("MkdirAll Claude inventory directory: %v", err)
	}
	inventory := []byte(
		`{"version":2,"plugins":{"context7@market":[{"scope":"project","projectPath":` +
			strconv.Quote(filepath.Dir(manifestPath)) +
			`}]}}`,
	)
	if err := os.WriteFile(inventoryPath, inventory, 0o600); err != nil {
		t.Fatalf("WriteFile Claude inventory: %v", err)
	}
	return manifestPath, inventoryPath
}

func planClaudeRefresh(t *testing.T, manifestPath string) *PreparedCommand {
	t.Helper()
	prepared, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		ExtensionID:  "context7",
	}, PlanOptions{})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Close() })
	return prepared
}

func assertClaudeRefreshRequest(t *testing.T, request subprocess.CommandRequest) {
	t.Helper()
	if request.Command != "claude" ||
		!slices.Equal(
			request.Args,
			[]string{
				"plugin",
				"update",
				"context7@market",
				"--scope",
				"project",
			},
		) {
		t.Fatalf("Claude refresh request = %#v", request)
	}
}
