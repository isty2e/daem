//go:build darwin || linux

package apply

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/reconcile"
	reconcilehostroute "github.com/isty2e/daem/internal/reconcile/build/hostroute"
	"github.com/isty2e/daem/internal/reconcile/delegatepolicy"
	"github.com/isty2e/daem/internal/subprocess"
	targetpkg "github.com/isty2e/daem/internal/target"
)

func TestDelegatePersistenceRejectsExternalStateRootReplacedDuringAttempt(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	manifestRoot := filepath.Join(base, "config")
	stateRoot := filepath.Join(base, "state")
	movedStateRoot := stateRoot + "-moved"
	if err := os.Mkdir(manifestRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := isolatedApplyTestPaths(t, manifestRoot)
	paths.StateDir = stateRoot
	paths.StatefilePath = filepath.Join(stateRoot, "state.json")
	paths.RecoveryDir = filepath.Join(stateRoot, "recovery")
	selection := applyMCPSelection(t)
	command := "must-not-run-daem-test"
	args := []string{"--serve", "context7"}
	resources := applyMCPEnvironment(
		t, "context7", targetpkg.TargetClaudeCode, command, args,
		map[string]string{"API_TOKEN": "DAEM_TEST_TOKEN"},
	)
	locked, _ := applyMCPLockfile(
		t,
		"context7",
		command,
		args,
	)
	assessment := buildAggregateApplyAssessment(t, paths, resources, locked, selection, false)
	delegateActions, err := reconcilehostroute.BuildDelegateActions(reconcilehostroute.DelegateInput{
		Locked:          locked,
		SelectedTargets: applySelectedTargets(t, selection),
		Context:         reconcile.ContextApply,
		Readiness: []reconcilehostroute.DelegateReadinessFact{{
			Subject: locked.Locked.Subjects()[0].SubjectID(),
			Runner:  delegatepolicy.RunnerAvailable,
		}},
	})
	if err != nil {
		t.Fatalf("Build delegate actions: %v", err)
	}
	assessment = assessmentWithDelegates(t, assessment, reconcile.ContextApply, delegateActions)
	runnerCalled := false

	result, err := runWithOptions(
		context.Background(),
		paths,
		resources,
		locked,
		selection,
		assessment,
		applyDelegateRunOptions(t, paths, runOptions{
			DelegateExecutor: delegate.NewExecutor(delegate.Options{
				LookupEnv: func(string) (string, bool) {
					return "safe", true
				},
				Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
					runnerCalled = true
					if renameErr := os.Rename(stateRoot, movedStateRoot); renameErr != nil {
						return subprocess.CommandResult{Started: true, Err: renameErr}
					}
					if mkdirErr := os.Mkdir(stateRoot, 0o700); mkdirErr != nil {
						return subprocess.CommandResult{Started: true, Err: mkdirErr}
					}
					return subprocess.CommandResult{Started: true, HasExitCode: true}
				},
			}),
		}),
	)
	if !runnerCalled {
		t.Fatal("delegate runner was not called")
	}
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("runWithOptions error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	if len(result.DelegateAttempts) != 1 {
		t.Fatalf("delegate attempts = %#v, want one observed attempt", result.DelegateAttempts)
	}
	if _, statErr := os.Stat(paths.StatefilePath); !os.IsNotExist(statErr) {
		t.Fatalf("replacement statefile %q stat error = %v, want absent", paths.StatefilePath, statErr)
	}
	movedStatePath := filepath.Join(movedStateRoot, "state.json")
	snapshot, loadErr := statefile.Load(context.Background(), movedStatePath)
	if loadErr != nil {
		t.Fatalf("load pre-attempt statefile %q: %v", movedStatePath, loadErr)
	}
	if len(snapshot.DelegateAttempts()) != 0 {
		t.Fatalf("delegate attempts persisted through replaced state root: %#v", snapshot.DelegateAttempts())
	}
}
