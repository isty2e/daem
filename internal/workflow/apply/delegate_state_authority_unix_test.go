//go:build darwin || linux

package apply

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/execute/delegate"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	daempaths "github.com/isty2e/daem/internal/paths"
	lockfile "github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/reconcile"
	reconcilehostroute "github.com/isty2e/daem/internal/reconcile/build/hostroute"
	"github.com/isty2e/daem/internal/reconcile/delegatepolicy"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/subprocess"
	targetpkg "github.com/isty2e/daem/internal/target"
)

func TestDelegatePersistenceRejectsExternalStateRootReplacedDuringAttempt(t *testing.T) {
	t.Parallel()
	testDelegatePersistenceRejectsStateRootReplacement(t, false)
}

func TestDelegatePersistenceRejectsProjectStateRootReplacedDuringAttempt(t *testing.T) {
	t.Parallel()
	testDelegatePersistenceRejectsStateRootReplacement(t, true)
}

func TestDelegateValidatesStateDirBeforeEveryInvocation(t *testing.T) {
	fixture := writeProviderOrderAuthorizationFixtureWithTwoDelegates(t)
	planned := fixture.initialPlan(t)
	t.Cleanup(func() { _ = planned.Close() })
	if len(planned.Reconciliation.Delegates()) != 2 {
		t.Fatalf("delegates = %#v, want two", planned.Reconciliation.Delegates())
	}
	paths, err := daempaths.Resolve(fixture.manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	initial := durable.EmptySnapshot()
	encoded, err := (statefile.Codec{}).Encode(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.StatefilePath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	locked, err := lockfile.Load(t.Context(), paths.LockfilePath)
	if err != nil {
		t.Fatal(err)
	}
	projectRoot, err := rootedpath.CaptureRoot(paths.ManifestRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer projectRoot.Close()
	barrier, err := recoverygate.NewEffectAuthority(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	movedStateDir := paths.StateDir + "-moved"
	delegateCalls := 0
	replaced := false
	effects := &standaloneStatefileEffects{barrier: barrier}
	reserve := func(
		statePath string,
		plan statefileEffectPlan,
	) (statefileEffectReservation, error) {
		reservation, reserveErr := effects.Reserve(statePath, plan)
		if reserveErr != nil {
			return nil, reserveErr
		}
		return replaceAfterFirstDelegateReservation{
			inner: reservation,
			afterFirst: func() error {
				if delegateCalls != 1 || replaced {
					return nil
				}
				replaced = true
				if renameErr := os.Rename(paths.StateDir, movedStateDir); renameErr != nil {
					return renameErr
				}
				return os.Mkdir(paths.StateDir, 0o700)
			},
		}, nil
	}
	fingerprint, err := remainingExecutionFingerprint(planned.Reconciliation)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runDelegatesAndPersistAttemptRecords(
		t.Context(),
		paths,
		locked,
		applyMCPSelection(t),
		paths.StatefilePath,
		initial,
		0,
		planned.Reconciliation,
		fingerprint,
		runOptions{
			DelegateExecutor: delegate.NewExecutor(delegate.Options{
				Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
					delegateCalls++
					return subprocess.CommandResult{Started: true, HasExitCode: true}
				},
			}),
			executionGuard:            testApplyExecutionGuard(t, paths),
			validateBeforeEffects:     effects.ValidateBefore,
			validateStateDir:          effects.ValidateStateDir,
			reserveStatefileAuthority: reserve,
			projectRoot:               projectRoot,
		},
	)
	if !replaced {
		t.Fatal("test did not replace StateDir between delegate iterations")
	}
	if delegateCalls != 1 {
		t.Fatalf("delegate calls = %d, want one", delegateCalls)
	}
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("delegate error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	if len(result.DelegateAttempts) != 1 {
		t.Fatalf("delegate attempts = %#v, want one", result.DelegateAttempts)
	}
}

type replaceAfterFirstDelegateReservation struct {
	inner      statefileEffectReservation
	afterFirst func() error
}

func (reservation replaceAfterFirstDelegateReservation) Bind(
	ctx context.Context,
) (boundStatefileEffectAuthority, error) {
	bound, err := reservation.inner.Bind(ctx)
	if err != nil {
		return nil, err
	}
	return &replaceAfterFirstDelegateAuthority{
		inner:      bound,
		afterFirst: reservation.afterFirst,
	}, nil
}

type replaceAfterFirstDelegateAuthority struct {
	inner      boundStatefileEffectAuthority
	afterFirst func() error
}

func (authority *replaceAfterFirstDelegateAuthority) Validate(ctx context.Context) error {
	if err := authority.inner.Validate(ctx); err != nil {
		return err
	}
	return authority.afterFirst()
}

func (authority *replaceAfterFirstDelegateAuthority) Entry() *rootedpath.EntryAuthority {
	return authority.inner.Entry()
}

func (authority *replaceAfterFirstDelegateAuthority) Close() error {
	return authority.inner.Close()
}

func testDelegatePersistenceRejectsStateRootReplacement(t *testing.T, projectStateRoot bool) {
	t.Helper()
	base := t.TempDir()
	manifestRoot := filepath.Join(base, "config")
	stateRoot := filepath.Join(base, "state")
	if projectStateRoot {
		stateRoot = filepath.Join(manifestRoot, ".daem")
	}
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
		t.Fatalf("delegate runner was not called: %v", err)
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
