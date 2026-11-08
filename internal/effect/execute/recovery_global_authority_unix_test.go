//go:build darwin || linux

package execute

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/execute/applyprogram"
	"github.com/isty2e/daem/internal/effect/execute/hostmutation"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/workmeasure"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestRecoveryRetainsGlobalRootAuthorityAcrossAncestorRetarget(t *testing.T) {
	rootRoles := []struct {
		name        string
		destination output.Destination
		useHome     bool
	}{
		{name: "home", destination: outputtest.Parse(t, "~/.codex/AGENTS.md"), useHome: true},
		{name: "data"},
	}
	retargetTimings := []struct {
		name        string
		afterReload bool
	}{
		{name: "before-final-reload"},
		{name: "after-final-reload", afterReload: true},
	}

	for _, rootRole := range rootRoles {
		for _, timing := range retargetTimings {
			t.Run(rootRole.name+"/"+timing.name, func(t *testing.T) {
				fixture := newGlobalFileRecoveryFixture(t, rootRole.destination, rootRole.useHome)
				options := RecoveryOptions{
					Resolver:                destinationResolver(fixture.paths),
					OwnershipRegistryBinder: testOwnershipRegistryBinder(),
					StateCodec:              testStateCodec(),
					StateReader:             testStateReader(fixture.paths.StatefilePath),
					Filesystem:              testFilesystem(),
				}
				if timing.afterReload {
					options.reloadPlan = func(
						ctx context.Context,
						loadOptions journal.PlanLoadOptions,
					) (recovery.Plan, error) {
						current, err := journal.LoadActivePlanWithOptions(
							ctx,
							fixture.paths.JournalPaths(),
							loadOptions,
						)
						if err != nil {
							return recovery.Plan{}, err
						}
						fixture.retarget(t)
						return current, nil
					}
				} else {
					options.ValidateBeforeEffects = func(context.Context, mutation.PhysicalAuthoritySet) error {
						fixture.retarget(t)
						return nil
					}
				}

				if err := executeRecoveryPlanWithOptionsForTest(
					context.Background(),
					fixture.plan,
					fixture.paths,
					options,
				); err != nil {
					t.Fatalf("ExecuteRecoveryPlanWithOptions: %v", err)
				}
				if fixture.beforeExists {
					assertRecoveryTestContent(t, fixture.admittedPath, fixture.before)
				} else if _, err := os.Stat(fixture.admittedPath); !os.IsNotExist(err) {
					t.Fatalf("recovered created path stat error = %v, want absence", err)
				}
				assertRecoveryTestContent(t, fixture.retargetedPath, fixture.after)
				if _, err := os.Stat(fixture.plan.OperationDir()); !os.IsNotExist(err) {
					t.Fatalf("recovery journal stat error = %v, want removed", err)
				}
			})
		}
	}
}

func TestRecoveryRejectsGlobalRootSelectionDriftBeforeEffects(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	fixture := newGlobalFileRecoveryFixture(t, destination, true)
	t.Setenv("HOME", fixture.retargetedRoot)
	t.Setenv("USERPROFILE", fixture.retargetedRoot)
	hostActions := 0

	err := executeRecoveryPlanWithOptionsForTest(
		context.Background(),
		fixture.plan,
		fixture.paths,
		RecoveryOptions{
			Resolver:                destinationResolver(fixture.paths),
			OwnershipRegistryBinder: testOwnershipRegistryBinder(),
			StateCodec:              testStateCodec(),
			StateReader:             testStateReader(fixture.paths.StatefilePath),
			Filesystem:              testFilesystem(),
			beforeHostAction: func(int) error {
				hostActions++
				return nil
			},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "root selection changed") {
		t.Fatalf("ExecuteRecoveryPlanWithOptions error = %v, want global root-selection drift refusal", err)
	}
	if hostActions != 0 {
		t.Fatalf("recovery host actions = %d, want none after root-selection drift", hostActions)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, fixture.after)
	assertRecoveryTestContent(t, fixture.retargetedPath, fixture.after)
	if _, statErr := os.Stat(fixture.plan.OperationDir()); statErr != nil {
		t.Fatalf("retained recovery journal stat error = %v", statErr)
	}
}

func TestRecoveryRejectsOwnershipRewriteImmediatelyBeforeHostEffect(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	fixture := newGlobalFileRecoveryFixture(t, destination, true)
	registryContent, err := os.ReadFile(fixture.paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatalf("read ownership registry: %v", err)
	}
	hostActions := 0

	err = executeRecoveryPlanWithOptionsForTest(
		t.Context(),
		fixture.plan,
		fixture.paths,
		RecoveryOptions{
			Resolver:                destinationResolver(fixture.paths),
			OwnershipRegistryBinder: testOwnershipRegistryBinder(),
			StateCodec:              testStateCodec(),
			StateReader:             testStateReader(fixture.paths.StatefilePath),
			Filesystem:              testFilesystem(),
			beforeHostAction: func(int) error {
				hostActions++
				return os.WriteFile(
					fixture.paths.OwnershipRegistryPath,
					append(append([]byte(nil), registryContent...), '\n'),
					0o600,
				)
			},
		},
	)
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("ExecuteRecoveryPlan error = %v, want stale semantic witness", err)
	}
	if hostActions != 1 {
		t.Fatalf("before-host callbacks = %d, want 1", hostActions)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, fixture.after)
	assertRecoveryJournalRetained(t, fixture.plan)
}

func TestRecoveryRejectsSamePathGlobalRootReplacementBeforeEffects(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	fixture := newGlobalFileRecoveryFixture(t, destination, true)
	movedRoot := filepath.Join(filepath.Dir(fixture.admittedRoot), "moved-admitted")
	if err := os.Rename(fixture.admittedRoot, movedRoot); err != nil {
		t.Fatalf("move captured global root: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(fixture.admittedPath), 0o700); err != nil {
		t.Fatalf("create replacement global root: %v", err)
	}
	writeRecoveryTestFile(t, fixture.admittedPath, fixture.after)
	relativePath, err := filepath.Rel(fixture.admittedRoot, fixture.admittedPath)
	if err != nil {
		t.Fatalf("derive moved destination: %v", err)
	}
	movedPath := filepath.Join(movedRoot, relativePath)
	hostActions := 0

	err = executeRecoveryPlanWithOptionsForTest(
		context.Background(),
		fixture.plan,
		fixture.paths,
		RecoveryOptions{
			Resolver:                destinationResolver(fixture.paths),
			OwnershipRegistryBinder: testOwnershipRegistryBinder(),
			StateCodec:              testStateCodec(),
			StateReader:             testStateReader(fixture.paths.StatefilePath),
			Filesystem:              testFilesystem(),
			beforeHostAction: func(int) error {
				hostActions++
				return nil
			},
		},
	)
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("ExecuteRecoveryPlanWithOptions error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	if hostActions != 0 {
		t.Fatalf("recovery host actions = %d, want none after root replacement", hostActions)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, fixture.after)
	assertRecoveryTestContent(t, movedPath, fixture.after)
	if _, statErr := os.Stat(fixture.plan.OperationDir()); statErr != nil {
		t.Fatalf("retained recovery journal stat error = %v", statErr)
	}
}

func TestRecoveryRequiresEveryGlobalBindingBeforeEffects(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	action := recovery.Action{
		Scope:       target.ScopeGlobal,
		Destination: destination.String(),
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	paths := Paths{}
	authority, err := captureScopedDestinationsForTest(
		paths,
		[]scopedDestinationForTest{{scope: target.ScopeGlobal, destination: outputtest.Parse(t, "~/.codex/OTHER.md")}},
		emptyRemovalDemandsForTest(), nil, destinationResolver(paths), testFilesystem(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authority.close() })

	err = requireRecoveryGlobalBindings(authority, []recovery.Action{action})
	if err == nil || !strings.Contains(err.Error(), destination.String()) {
		t.Fatalf("requireRecoveryGlobalBindings error = %v, want exact unbound destination refusal", err)
	}
	if _, err := authority.destinations.Resolve(target.ScopeGlobal, destination); err == nil ||
		!strings.Contains(err.Error(), "was not bound before effects") {
		t.Fatalf("resolveBoundDestination error = %v, want lexical fallback refusal", err)
	}
}

func TestRecoveryRollbackStageAndRestoreUseSameGlobalRootAuthority(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	fixture := newGlobalFileRecoveryFixture(t, destination, true)
	authority, err := newRecoveryMutationAuthority(
		fixture.paths,
		fixture.plan,
		destinationResolver(fixture.paths),
		testFilesystem(),
		nil,
	)
	if err != nil {
		t.Fatalf("newRecoveryMutationAuthority: %v", err)
	}
	t.Cleanup(func() { _ = authority.close() })
	planned := fixture.plan.Actions()[0]
	hostAction := recoveryHostActionFromJournalAction(planned)
	prepareRecoveryBackupsForTest(t, t.Context(), authority, fixture.plan, []hostmutation.RestorationAction{hostAction})
	rollback, err := authority.hostMutation().StageRestoration(
		context.Background(),
		authority.physicalWorkBudget, authority.control, authority.hostBackups(),
		[]hostmutation.RestorationAction{hostAction},
		testAggregateCodecs(),
	)
	if err != nil {
		t.Fatalf("stageRecoveryRollback: %v", err)
	}
	t.Cleanup(func() { _ = rollback.Cleanup(context.Background()) })

	retargetCanary := []byte("retarget canary\n")
	writeRecoveryTestFile(t, fixture.retargetedPath, retargetCanary)
	fixture.retarget(t)
	beginUnclassifiedRecoveryExecutionForTest(t, authority)
	if _, err := rollback.PrepareExecution(); err != nil {
		t.Fatal(err)
	}
	if err := rollback.Execute(t.Context(), authority.generalExecutionWorkBudget, nil, nil, applyprogram.VisibilityGate{}); err != nil {
		t.Fatalf("execute rooted recovery before compensation: %v", err)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, fixture.before)
	assertRecoveryTestContent(t, fixture.retargetedPath, retargetCanary)

	if err := rollback.Restore(context.Background(), authority.generalExecutionWorkBudget, applyprogram.VisibilityGate{}); err != nil {
		t.Fatalf("restore recovery rollback: %v", err)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, fixture.after)
	assertRecoveryTestContent(t, fixture.retargetedPath, retargetCanary)
	if err := rollback.Cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup rollback scratch: %v", err)
	}
}

func TestRecoveryRejectsUnreservableRollbackWorkBeforeHostEffect(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	fixture := newGlobalFileRecoveryFixture(t, destination, true)
	authority, err := newRecoveryMutationAuthority(
		fixture.paths,
		fixture.plan,
		destinationResolver(fixture.paths),
		testFilesystem(),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authority.close() })

	baselineBytes := int64(len(fixture.after))
	remainingForStage := baselineBytes * 2
	consume, err := workmeasure.NewArtifactWork(
		0,
		authority.physicalWorkBudget.RemainingBytes()-remainingForStage,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.physicalWorkBudget.AdmitTree(consume); err != nil {
		t.Fatal(err)
	}

	_, err = authority.hostMutation().StageRestoration(
		t.Context(),
		authority.physicalWorkBudget, authority.control, authority.hostBackups(),
		[]hostmutation.RestorationAction{recoveryHostActionFromJournalAction(fixture.plan.Actions()[0])},
		testAggregateCodecs(),
	)
	if err == nil || !strings.Contains(err.Error(), "operation limit") {
		t.Fatalf("stageRecoveryRollback error = %v, want pre-effect operation-limit refusal", err)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, fixture.after)
}

func TestRecoveryBackupRejectsReplacementAfterViewSelection(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	fixture := newGlobalFileRecoveryFixture(t, destination, true)
	action := recoveryHostActionFromJournalAction(fixture.plan.Actions()[0])
	authority, err := newRecoveryMutationAuthority(
		fixture.paths,
		fixture.plan,
		destinationResolver(fixture.paths),
		testFilesystem(),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authority.close() })
	prepareRecoveryBackupsForTest(t, t.Context(), authority, fixture.plan, []hostmutation.RestorationAction{action})
	backup, err := authority.hostBackups().For(hostmutation.RestorationBackupRequests([]hostmutation.RestorationAction{action})[0])
	if err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(
		fixture.plan.OperationDir(),
		filepath.FromSlash(action.BackupPath),
	)
	if err := os.WriteFile(backupPath, []byte("replacement after view selection\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := backup.ReadFile(t.Context()); err == nil {
		t.Fatal("recovery backup replacement was accepted")
	}
	assertRecoveryTestContent(t, fixture.admittedPath, fixture.after)
}

func TestRecoveryBackupAcceptsEquivalentReplacementAfterViewSelection(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	fixture := newGlobalFileRecoveryFixture(t, destination, true)
	action := recoveryHostActionFromJournalAction(fixture.plan.Actions()[0])
	authority, err := newRecoveryMutationAuthority(
		fixture.paths,
		fixture.plan,
		destinationResolver(fixture.paths),
		testFilesystem(),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authority.close() })
	prepareRecoveryBackupsForTest(t, t.Context(), authority, fixture.plan, []hostmutation.RestorationAction{action})
	backup, err := authority.hostBackups().For(hostmutation.RestorationBackupRequests([]hostmutation.RestorationAction{action})[0])
	if err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(
		fixture.plan.OperationDir(),
		filepath.FromSlash(action.BackupPath),
	)
	content, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	replacement := backupPath + ".replacement"
	if err := os.WriteFile(replacement, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, backupPath); err != nil {
		t.Fatal(err)
	}

	read, err := backup.ReadFile(t.Context())
	if err != nil {
		t.Fatalf("read equivalent replacement: %v", err)
	}
	if string(read) != string(content) {
		t.Fatalf("recovery backup content = %q, want %q", read, content)
	}
}

func TestRecoveryBackupRejectsOversizedRegularFile(t *testing.T) {
	budget, err := physicalwork.NewBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	work, err := workmeasure.NewArtifactWork(0, recovery.MaximumRecoveryBackupFileBytes+1)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveBackupFileExecution(work); err == nil ||
		!strings.Contains(err.Error(), "134217728") {
		t.Fatalf("recovery backup reservation error = %v, want bounded rejection", err)
	}
}

func TestRecoveryRejectsDirectFileChangeAfterFinalReloadWithoutOverwritingIt(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	fixture := newGlobalFileRecoveryFixture(t, destination, true)
	external := []byte("external after final reload\n")
	err := executeRecoveryPlanWithOptionsForTest(
		context.Background(),
		fixture.plan,
		fixture.paths,
		RecoveryOptions{
			Resolver:                destinationResolver(fixture.paths),
			OwnershipRegistryBinder: testOwnershipRegistryBinder(),
			StateCodec:              testStateCodec(),
			StateReader:             testStateReader(fixture.paths.StatefilePath),
			Filesystem:              testFilesystem(),
			reloadPlan: func(
				ctx context.Context,
				options journal.PlanLoadOptions,
			) (recovery.Plan, error) {
				current, err := journal.LoadActivePlanWithOptions(ctx, fixture.paths.JournalPaths(), options)
				if err == nil {
					writeRecoveryTestFile(t, fixture.admittedPath, external)
				}
				return current, err
			},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "does not match expected") {
		t.Fatalf("ExecuteRecoveryPlanWithOptions error = %v, want expected-after rejection", err)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, external)
	if _, err := os.Stat(fixture.plan.OperationDir()); err != nil {
		t.Fatalf("recovery journal was removed after rejected staging: %v", err)
	}
}

func TestRecoveryRejectsDirectFileChangeAfterRollbackStaging(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	fixture := newGlobalFileRecoveryFixture(t, destination, true)
	external := []byte("external after rollback staging\n")
	hostActions := 0
	err := executeRecoveryPlanWithOptionsForTest(
		t.Context(),
		fixture.plan,
		fixture.paths,
		RecoveryOptions{
			Resolver:                destinationResolver(fixture.paths),
			OwnershipRegistryBinder: testOwnershipRegistryBinder(),
			StateCodec:              testStateCodec(),
			StateReader:             testStateReader(fixture.paths.StatefilePath),
			Filesystem:              testFilesystem(),
			beforeHostAction: func(int) error {
				hostActions++
				writeRecoveryTestFile(t, fixture.admittedPath, external)
				return nil
			},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "changed after rollback staging") {
		t.Fatalf(
			"ExecuteRecoveryPlanWithOptions error = %v, want staged baseline rejection",
			err,
		)
	}
	if hostActions != 1 {
		t.Fatalf("recovery host actions = %d, want one pre-effect probe", hostActions)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, external)
	if _, statErr := os.Stat(fixture.plan.OperationDir()); statErr != nil {
		t.Fatalf("recovery journal was removed after staged baseline rejection: %v", statErr)
	}
}

func TestRecoveryDirectFileCommitRejectsStagedEntryDrift(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	fixture := newGlobalFileRecoveryFixture(t, destination, true)
	authority, err := newRecoveryMutationAuthority(
		fixture.paths,
		fixture.plan,
		destinationResolver(fixture.paths),
		testFilesystem(),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authority.close() })
	planned := fixture.plan.Actions()[0]
	hostAction := recoveryHostActionFromJournalAction(planned)
	prepareRecoveryBackupsForTest(
		t,
		context.Background(),
		authority,
		fixture.plan,
		[]hostmutation.RestorationAction{hostAction},
	)
	rollback, err := authority.hostMutation().StageRestoration(
		context.Background(),
		authority.physicalWorkBudget, authority.control, authority.hostBackups(),
		[]hostmutation.RestorationAction{hostAction},
		testAggregateCodecs(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rollback.Cleanup(context.Background()) })
	beginUnclassifiedRecoveryExecutionForTest(t, authority)

	external := []byte("external after staging\n")
	writeRecoveryTestFile(t, fixture.admittedPath, external)
	if _, err := rollback.PrepareExecution(); err != nil {
		t.Fatal(err)
	}
	err = rollback.Execute(
		context.Background(),
		authority.generalExecutionWorkBudget,
		nil,
		nil,
		applyprogram.VisibilityGate{},
	)
	if err == nil || !strings.Contains(err.Error(), "changed after rollback staging") {
		t.Fatalf("executeRecoveryHostActions error = %v, want staged baseline rejection", err)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, external)
}

func TestRecoveryRollbackRefusesExternalChangeAfterCommittedRecoveryEffect(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	fixture := newGlobalFileRecoveryFixture(t, destination, true)
	authority, err := newRecoveryMutationAuthority(
		fixture.paths,
		fixture.plan,
		destinationResolver(fixture.paths),
		testFilesystem(),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authority.close() })
	hostAction := recoveryHostActionFromJournalAction(fixture.plan.Actions()[0])
	prepareRecoveryBackupsForTest(
		t,
		context.Background(),
		authority,
		fixture.plan,
		[]hostmutation.RestorationAction{hostAction},
	)
	rollback, err := authority.hostMutation().StageRestoration(
		context.Background(),
		authority.physicalWorkBudget, authority.control, authority.hostBackups(),
		[]hostmutation.RestorationAction{hostAction},
		testAggregateCodecs(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rollback.Cleanup(context.Background()) })
	beginUnclassifiedRecoveryExecutionForTest(t, authority)
	if _, err := rollback.PrepareExecution(); err != nil {
		t.Fatal(err)
	}
	if err := rollback.Execute(
		context.Background(),
		authority.generalExecutionWorkBudget,
		nil,
		nil,
		applyprogram.VisibilityGate{},
	); err != nil {
		t.Fatal(err)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, fixture.before)

	external := []byte("external after recovery effect\n")
	writeRecoveryTestFile(t, fixture.admittedPath, external)
	err = rollback.Restore(context.Background(), authority.generalExecutionWorkBudget, applyprogram.VisibilityGate{})
	if err == nil || !strings.Contains(err.Error(), "changed outside the recovery attempt") {
		t.Fatalf("rollback.restore error = %v, want external-change refusal", err)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, external)
}

func TestRecoveryHostActionsStopAtLostVisibilityAuthority(t *testing.T) {
	root := t.TempDir()
	paths := Paths{ManifestRoot: root}
	contents := [][]byte{[]byte("first\n"), []byte("second\n")}
	destinations := make([]output.Destination, len(contents))
	guarded := make([]recovery.Action, len(contents))
	hostActions := make([]hostmutation.RestorationAction, len(contents))
	for index, content := range contents {
		destination, err := output.Parse(fmt.Sprintf("FILE-%d.md", index+1))
		if err != nil {
			t.Fatal(err)
		}
		destinations[index] = destination
		writeRecoveryTestFile(t, filepath.Join(root, destination.String()), content)
		guarded[index] = recovery.Action{
			Scope:           target.ScopeProject,
			Destination:     destination.String(),
			ConsumerTargets: []target.Target{target.TargetCodex},
		}
		hostActions[index] = hostmutation.RestorationAction{
			Kind:        recovery.ActionKindRestoreDelete,
			Scope:       target.ScopeProject,
			Destination: destination.String(),
			ExpectedAfter: recovery.ExpectedPathState{
				Existed:     true,
				PathMode:    recovery.NewPermissionMode(0o600),
				Kind:        recovery.PathKindFile,
				ContentHash: string(artifact.HashFileContent(content)),
			},
		}
	}
	authority, err := captureMutationAuthority(
		paths,
		true,
		nil,
		destinationResolver(paths),
		testFilesystem(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authority.close() })
	for _, action := range guarded {
		logical, parseErr := hostmutation.ParseRestorationDestination(action.Scope, action.Destination)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if err := authority.bindPhysicalAuthority(action.Scope, logical, action.ConsumerTargets); err != nil {
			t.Fatal(err)
		}
	}
	var testIntents []recovery.RemovalIntent
	for index, action := range hostActions {
		logical, parseErr := output.Parse(action.Destination)
		if parseErr != nil {
			t.Fatalf("parse recovery test destination %d: %v", index, parseErr)
		}
		destination, resolveErr := authority.destinations.Resolve(action.Scope, logical)
		if resolveErr != nil {
			t.Fatalf("resolve recovery test destination %d: %v", index, resolveErr)
		}
		testIntents = append(testIntents, testFileRemovalIntent(t, destination, contents[index]))
	}
	installRemovalIntentsForTest(t, authority, testIntents)
	rollback, err := authority.hostMutation().StageRestoration(t.Context(), authority.physicalWorkBudget, authority.control, authority.hostBackups(), hostActions, testAggregateCodecs())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rollback.Cleanup(context.Background()) })
	if err := hostmutation.PrepareRecoveryForwardRemovals(
		t.Context(),
		authority.removals,
		hostActions,
		testAggregateCodecs(),
	); err != nil {
		t.Fatalf("prepare bounded test removals: %v", err)
	}
	beginUnclassifiedRecoveryExecutionForTest(t, authority)
	validations := 0
	accepts := 0
	gate := applyprogram.NewVisibilityGate(
		func(context.Context) error {
			validations++
			if validations == 2 {
				return errors.New("injected visibility authority loss")
			}
			return nil
		},
		func(context.Context) error {
			accepts++
			return nil
		},
	)
	if _, err := rollback.PrepareExecution(); err != nil {
		t.Fatal(err)
	}
	err = rollback.Execute(
		t.Context(),
		authority.generalExecutionWorkBudget,
		nil,
		nil,
		gate,
	)
	if err == nil || !strings.Contains(err.Error(), "injected visibility authority loss") {
		t.Fatalf("executeRecoveryHostActions error = %v, want authority loss", err)
	}
	if validations != 2 || accepts != 1 {
		t.Fatalf("visibility gate calls = validate:%d accept:%d, want 2/1", validations, accepts)
	}
	if _, err := os.Lstat(filepath.Join(root, destinations[0].String())); !os.IsNotExist(err) {
		t.Fatalf("first recovery destination stat = %v, want removed", err)
	}
	assertRecoveryTestContent(t, filepath.Join(root, destinations[1].String()), contents[1])
}

func recoveryHostActionFromJournalAction(action recovery.Action) hostmutation.RestorationAction {
	return hostmutation.RestorationAction{
		Kind:                action.Kind,
		Scope:               action.Scope,
		Destination:         action.Destination,
		ContentPath:         action.ContentPath,
		BackupPath:          action.BackupPath,
		BackupHash:          action.BackupHash,
		BackupKind:          action.BackupKind,
		BackupWork:          action.BackupWork,
		BeforePathMode:      action.BeforePathMode,
		BeforePathExisted:   action.BeforePathExisted,
		BeforeParentExisted: action.BeforeParentExisted,
		ExpectedAfter:       action.ExpectedAfter.Clone(),
		AggregateContract:   action.AggregateContract,
	}
}

type globalFileRecoveryFixture struct {
	paths          Paths
	plan           recovery.Plan
	aliasRoot      string
	admittedRoot   string
	retargetedRoot string
	admittedPath   string
	retargetedPath string
	before         []byte
	after          []byte
	beforeExists   bool
}

func newGlobalFileRecoveryFixture(
	t *testing.T,
	destination output.Destination,
	useHome bool,
) globalFileRecoveryFixture {
	t.Helper()
	base := t.TempDir()
	projectRoot := filepath.Join(base, "project")
	stateDir := filepath.Join(base, "state")
	admittedRoot := filepath.Join(base, "admitted")
	retargetedRoot := filepath.Join(base, "retargeted")
	for _, directory := range []string{projectRoot, stateDir, admittedRoot, retargetedRoot} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("create recovery fixture directory %q: %v", directory, err)
		}
	}
	aliasRoot := filepath.Join(base, "selected-root")
	if err := os.Symlink(admittedRoot, aliasRoot); err != nil {
		t.Fatalf("create selected-root symlink: %v", err)
	}
	paths := Paths{
		RecoveryDir:           filepath.Join(stateDir, "recovery"),
		StateDir:              stateDir,
		StatefilePath:         filepath.Join(stateDir, "state.json"),
		ManifestRoot:          projectRoot,
		DataDir:               filepath.Join(stateDir, "data"),
		OwnershipRegistryPath: filepath.Join(stateDir, "ownership", "claims.json"),
	}
	if useHome {
		t.Setenv("HOME", aliasRoot)
		t.Setenv("USERPROFILE", aliasRoot)
	} else {
		paths.DataDir = aliasRoot
	}

	before := []byte("before recovery\n")
	after := []byte("expected after\n")
	beforeHash := artifact.HashFileContentWithExecutable(before, false)
	afterHash := artifact.HashFileContentWithExecutable(after, false)

	var (
		subject          topology.SubjectID
		consumers        []target.Target
		contentKind      realization.PathProjectionContentKind
		permissionPolicy realization.PathPermissionPolicy
		stateMode        os.FileMode
		beforeExists     bool
	)
	if useHome {
		entityID, err := entity.New(entity.KindInstructions, "global-root-authority")
		if err != nil {
			t.Fatalf("construct global instruction entity: %v", err)
		}
		subject, err = topologyprojection.Subject(entityID, "instructions.global.codex")
		if err != nil {
			t.Fatalf("lower global instruction subject: %v", err)
		}
		consumers = []target.Target{target.TargetCodex}
		contentKind = realization.PathProjectionFile
		permissionPolicy = realization.PathPermissionsExecutableClass
		beforeExists = true
	} else {
		placement, err := profile.HookAssetPlacementFor(
			target.ScopeGlobal,
			[]target.Target{target.TargetCodex},
		)
		if err != nil {
			t.Fatalf("derive global HookAsset placement: %v", err)
		}
		destinationValue, err := placement.Destination("recovery", afterHash)
		if err != nil {
			t.Fatalf("derive global HookAsset destination: %v", err)
		}
		destination = destinationValue
		entityID, err := entity.New(entity.KindHookAsset, "recovery")
		if err != nil {
			t.Fatalf("construct global HookAsset entity: %v", err)
		}
		subject, err = topologyprojection.Subject(entityID, placement.ID())
		if err != nil {
			t.Fatalf("lower global HookAsset subject: %v", err)
		}
		consumers = placement.ConsumerTargets()
		contentKind = realization.PathProjectionFile
		permissionPolicy = realization.PathPermissionsExact
		stateMode = 0o600
	}

	resolver := hostpath.NewResolverWithManagedDataRoot(paths.ManifestRoot, paths.DataDir)
	admittedPath, err := resolver.Resolve(destination)
	if err != nil {
		t.Fatalf("resolve admitted destination: %v", err)
	}
	admittedPath, err = mutation.CanonicalDirectoryEntryPath(admittedPath)
	if err != nil {
		t.Fatalf("canonicalize admitted destination: %v", err)
	}
	canonicalAdmittedRoot, err := mutation.CanonicalDirectoryEntryPath(admittedRoot)
	if err != nil {
		t.Fatalf("canonicalize admitted root: %v", err)
	}
	relativePath, err := filepath.Rel(canonicalAdmittedRoot, admittedPath)
	if err != nil {
		t.Fatalf("derive destination relative path: %v", err)
	}
	retargetedPath := filepath.Join(retargetedRoot, relativePath)

	if beforeExists {
		writeRecoveryTestFile(t, admittedPath, before)
	}

	nextPath, err := durable.NewManagedPathState(
		subject,
		consumers,
		target.ScopeGlobal,
		destination,
		afterHash,
		contentKind,
		permissionPolicy,
		stateMode,
	)
	if err != nil {
		t.Fatalf("construct next global managed path state: %v", err)
	}
	var (
		mutationRequest journalrecord.ManagedPathMutation
		evidence        observe.ManagedPathEvidence
		currentState    durable.Snapshot
	)
	if beforeExists {
		previous, err := durable.NewManagedPathState(
			subject,
			consumers,
			target.ScopeGlobal,
			destination,
			beforeHash,
			contentKind,
			permissionPolicy,
			stateMode,
		)
		if err != nil {
			t.Fatalf("construct previous global managed path state: %v", err)
		}
		mutationRequest, err = journalrecord.NewManagedPathReplaceMutation(
			subject,
			consumers,
			target.ScopeGlobal,
			destination,
			afterHash,
			beforeHash,
			contentKind,
			0o600,
			previous,
		)
		if err != nil {
			t.Fatalf("construct global replace mutation: %v", err)
		}
		evidence, err = observe.NewManagedPathEvidence(subject, destination, true, beforeHash, 0o600)
		if err != nil {
			t.Fatalf("construct global replace evidence: %v", err)
		}
		currentState, err = durable.NewSnapshot(durable.SnapshotInput{
			ManagedPaths: []durable.ManagedPathState{previous},
		})
		if err != nil {
			t.Fatalf("construct current global snapshot: %v", err)
		}
	} else {
		mutationRequest, err = journalrecord.NewManagedPathCreateMutation(
			subject,
			consumers,
			target.ScopeGlobal,
			destination,
			afterHash,
			contentKind,
			0o600,
			nil,
		)
		if err != nil {
			t.Fatalf("construct global create mutation: %v", err)
		}
		evidence, err = observe.NewManagedPathEvidence(subject, destination, false, "", 0)
		if err != nil {
			t.Fatalf("construct global create evidence: %v", err)
		}
		currentState = durable.EmptySnapshot()
	}
	nextState, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths: []durable.ManagedPathState{nextPath},
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{
			recoveryTestPendingCarrierInstall(
				t,
				paths.StatefilePath,
				filepath.Join(projectRoot, "daem.toml"),
			),
		},
	})
	if err != nil {
		t.Fatalf("construct next global snapshot: %v", err)
	}
	operationID := journalpublication.OperationID(time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC))
	statefileKey, err := mutation.CanonicalDirectoryEntryKey(paths.StatefilePath)
	if err != nil {
		t.Fatalf("canonicalize global recovery statefile: %v", err)
	}
	manifestPath, err := mutation.CanonicalDirectoryEntryKey(filepath.Join(projectRoot, "daem.toml"))
	if err != nil {
		t.Fatalf("canonicalize global recovery manifest: %v", err)
	}
	owner, err := stateauthority.New(mustObservedPathAuthority(t, statefileKey), manifestPath)
	if err != nil {
		t.Fatalf("construct global recovery owner: %v", err)
	}
	managedPathKey, err := mutation.CanonicalDirectoryEntryKey(admittedPath)
	if err != nil {
		t.Fatalf("canonicalize global recovery managed path: %v", err)
	}
	address, err := ownership.NewManagedAddressFromLegacy(mustObservedPathAuthority(t, managedPathKey), "")
	if err != nil {
		t.Fatalf("construct global recovery managed address: %v", err)
	}
	var transition ownershipmutation.ClaimTransition
	if beforeExists {
		active, err := ownership.NewActiveClaim(address, owner)
		if err != nil {
			t.Fatalf("construct active global recovery claim: %v", err)
		}
		transition, err = ownershipmutation.NewRetainTransition(active)
		if err != nil {
			t.Fatalf("construct global recovery retain transition: %v", err)
		}
	} else {
		transition, err = ownershipmutation.NewAcquireTransition(address, owner, operationID)
		if err != nil {
			t.Fatalf("construct global recovery acquire transition: %v", err)
		}
	}
	registry, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatalf("construct global recovery ownership store: %v", err)
	}
	claim := transition.Prepared()
	if _, err := registry.Apply(context.Background(), address, ownership.NoClaim(), claim); err != nil {
		t.Fatalf("seed global recovery ownership claim: %v", err)
	}
	removalDemands := recovery.RemovalDemandSet{}
	if !beforeExists {
		removalDemands = testManagedPathRemovalDemandSet(t, nil, 0, &nextPath, stateMode)
	}
	if _, err := journalpublication.CaptureJournalWithOptions(
		context.Background(),
		paths.JournalPaths(),
		operationID,
		time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC),
		currentState,
		nextState,
		journalpublication.CaptureOptions{
			Filesystem:           testFilesystem(),
			ClaimTransitions:     []ownershipmutation.ClaimTransition{transition},
			ManagedPathMutations: []journalrecord.ManagedPathMutation{mutationRequest},
			ManagedPathEvidence:  []observe.ManagedPathEvidence{evidence},
			RemovalDemands:       removalDemands,
			Resolver:             resolver.Resolve,
			StateCodec:           testStateCodec(),
		},
	); err != nil {
		t.Fatalf("capture recovery journal: %v", err)
	}
	writeRecoveryTestFile(t, admittedPath, after)
	writeRecoveryTestFile(t, retargetedPath, after)
	writeRecoveryTestStatefile(t, paths.StatefilePath, currentState)
	recoveryPlan, err := journal.LoadActivePlanWithOptions(
		context.Background(),
		paths.JournalPaths(),
		journal.PlanLoadOptions{
			Filesystem:        testFilesystem(),
			Resolver:          journalDestinationResolver(paths),
			OwnershipRegistry: registry,
			StateCodec:        testStateCodec(),
			StateReader:       testStateReader(paths.StatefilePath),
		},
	)
	if err != nil {
		t.Fatalf("load initial recovery plan: %v", err)
	}
	if recoveryPlan.Classification() != recovery.ClassificationNeedsRollback {
		t.Fatalf(
			"initial recovery classification = %q, want %q; actions=%#v",
			recoveryPlan.Classification(),
			recovery.ClassificationNeedsRollback,
			recoveryPlan.Actions(),
		)
	}
	return globalFileRecoveryFixture{
		paths:          paths,
		plan:           recoveryPlan,
		aliasRoot:      aliasRoot,
		admittedRoot:   admittedRoot,
		retargetedRoot: retargetedRoot,
		admittedPath:   admittedPath,
		retargetedPath: retargetedPath,
		before:         before,
		after:          after,
		beforeExists:   beforeExists,
	}
}

func (fixture globalFileRecoveryFixture) retarget(t *testing.T) {
	t.Helper()
	if err := os.Remove(fixture.aliasRoot); err != nil {
		t.Fatalf("remove selected-root symlink: %v", err)
	}
	if err := os.Symlink(fixture.retargetedRoot, fixture.aliasRoot); err != nil {
		t.Fatalf("retarget selected-root symlink: %v", err)
	}
}
