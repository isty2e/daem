//go:build darwin || linux

package apply

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

func TestCarrierRemovalRejectsStateDirReplacementBeforePostAttemptPersistence(t *testing.T) {
	fixture := newWorkflowFixture(t, target.ScopeProject)
	stateDir := filepath.Dir(fixture.statePath)
	movedStateDir := stateDir + "-moved"
	input := fixture.input(t)
	input.Executor = subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			fixture.executorCalls++
			if err := os.Rename(stateDir, movedStateDir); err != nil {
				return subprocess.CommandResult{Started: true, Err: err}
			}
			if err := os.Mkdir(stateDir, 0o700); err != nil {
				return subprocess.CommandResult{Started: true, Err: err}
			}
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})

	plan := scheduledCarrierRemovalTestPlan(t, input.Actions)
	result, err := runScheduledCarrierRemovals(t.Context(), input, plan, plan)
	if fixture.executorCalls != 1 {
		t.Fatalf("carrier removal runner calls = %d, want 1: %v", fixture.executorCalls, err)
	}
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("runScheduledCarrierRemovals error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	if len(result.Attempts) != 1 {
		t.Fatalf("carrier removal attempts = %#v, want one observed attempt", result.Attempts)
	}
	if _, statErr := os.Stat(fixture.statePath); !os.IsNotExist(statErr) {
		t.Fatalf("replacement statefile %q stat error = %v, want absent", fixture.statePath, statErr)
	}
	movedStatePath := filepath.Join(movedStateDir, "state.json")
	content, readErr := os.ReadFile(movedStatePath)
	if readErr != nil {
		t.Fatalf("read moved statefile: %v", readErr)
	}
	snapshot, decodeErr := (statefile.Codec{}).Decode(content)
	if decodeErr != nil {
		t.Fatalf("decode moved statefile: %v", decodeErr)
	}
	if len(snapshot.HostRouteAttempts()) != 0 {
		t.Fatalf("carrier removal attempt persisted through replaced StateDir: %#v", snapshot.HostRouteAttempts())
	}
}
