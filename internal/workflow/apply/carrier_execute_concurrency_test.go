package apply

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

func TestConcurrentExecuteSerializesReobservationAndInvokesHostOnce(t *testing.T) {
	root, manifestPath, lockfilePath, _, locked, _ := writeApplyClaudePluginCarrierCommandFixtureForScope(t, target.ScopeProject)
	configRoot := filepath.Join(root, "claude-config")
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)

	firstPlan, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		LockfilePath: lockfilePath,
		TargetValues: []string{"claude-code"},
	})
	if err != nil {
		t.Fatalf("first PlanWrite returned error: %v", err)
	}
	secondPlan, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath: manifestPath,
		LockfilePath: lockfilePath,
		TargetValues: []string{"claude-code"},
	})
	if err != nil {
		t.Fatalf("second PlanWrite returned error: %v", err)
	}

	var calls atomic.Int32
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Clock: fixedApplyHostRouteClock,
		Runner: func(ctx context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			if calls.Add(1) != 1 {
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 97}
			}
			close(firstStarted)
			select {
			case <-releaseFirst:
			case <-ctx.Done():
				return subprocess.CommandResult{Started: true, Err: ctx.Err()}
			}
			content, marshalErr := json.Marshal(map[string]any{
				"version": 2,
				"plugins": map[string]any{
					"context7@market": []map[string]string{{"scope": "project", "projectPath": root}},
				},
			})
			if marshalErr != nil {
				return subprocess.CommandResult{Started: true, Err: marshalErr}
			}
			inventoryPath := filepath.Join(configRoot, "plugins", "installed_plugins.json")
			if err := os.MkdirAll(filepath.Dir(inventoryPath), 0o700); err != nil {
				return subprocess.CommandResult{Started: true, Err: err}
			}
			if err := os.WriteFile(inventoryPath, content, 0o600); err != nil {
				return subprocess.CommandResult{Started: true, Err: err}
			}
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})

	type outcome struct {
		result CommandResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		result, executeErr := ExecuteWithOptions(ctx, firstPlan, ExecuteOptions{HostRouteExecutor: executor})
		outcomes <- outcome{result: result, err: executeErr}
	}()
	<-firstStarted
	secondLaunched := make(chan struct{})
	go func() {
		close(secondLaunched)
		result, executeErr := ExecuteWithOptions(ctx, secondPlan, ExecuteOptions{HostRouteExecutor: executor})
		outcomes <- outcome{result: result, err: executeErr}
	}()
	<-secondLaunched
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("host route calls while first apply holds mutation lock = %d, want 1", got)
	}
	close(releaseFirst)

	var attemptCounts []int
	staleCount := 0
	for range 2 {
		outcome := <-outcomes
		if outcome.err != nil {
			var stale mutation.StaleSnapshotError
			if !errors.As(outcome.err, &stale) {
				t.Fatalf("concurrent ExecuteWithOptions returned error: %v", outcome.err)
			}
			staleCount++
		}
		attemptCounts = append(attemptCounts, len(outcome.result.HostRouteAttempts))
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("host route calls = %d, want exactly one after serialized reobservation", got)
	}
	if staleCount != 1 {
		t.Fatalf("stale result count = %d, want one", staleCount)
	}
	if !(slices.Equal(attemptCounts, []int{1, 0}) || slices.Equal(attemptCounts, []int{0, 1})) {
		t.Fatalf("host route attempt counts = %#v, want one executing result and one stale result", attemptCounts)
	}
	state := loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json"))
	assertApplyProjectManagedCarrierClaim(t, state, locked.Locked.Subjects()[0])
}
