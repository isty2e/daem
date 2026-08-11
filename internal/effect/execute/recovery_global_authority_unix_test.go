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

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
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
							fixture.paths.journalPaths(),
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
	authority := &mutationAuthority{
		globalDestinationBindings: map[output.Destination]globalDestinationBinding{
			outputtest.Parse(t, "~/.codex/OTHER.md"): {},
		},
	}

	err := requireRecoveryGlobalBindings(authority, []recovery.Action{action})
	if err == nil || !strings.Contains(err.Error(), destination.String()) {
		t.Fatalf("requireRecoveryGlobalBindings error = %v, want exact unbound destination refusal", err)
	}
	if _, err := authority.resolveBoundDestination(target.ScopeGlobal, destination); err == nil ||
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

	rollback, err := stageRecoveryRollback(
		context.Background(),
		authority,
		[]recoveryHostAction{hostAction},
		testAggregateCodecs(),
	)
	if err != nil {
		t.Fatalf("stageRecoveryRollback: %v", err)
	}
	rollbackDir := rollback.dir
	t.Cleanup(func() { _ = os.RemoveAll(rollbackDir) })
	if filepath.Clean(filepath.Dir(rollbackDir)) != filepath.Clean(os.TempDir()) {
		t.Fatalf("rollback scratch parent = %q, want system temp %q", filepath.Dir(rollbackDir), os.TempDir())
	}
	relativeToState, err := filepath.Rel(fixture.paths.StateDir, rollbackDir)
	if err != nil {
		t.Fatalf("relativize rollback scratch against state root: %v", err)
	}
	if relativeToState != ".." &&
		!strings.HasPrefix(relativeToState, ".."+string(filepath.Separator)) {
		t.Fatalf("rollback scratch %q is inside selected state root %q", rollbackDir, fixture.paths.StateDir)
	}
	info, err := os.Stat(rollbackDir)
	if err != nil {
		t.Fatalf("stat rollback scratch: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("rollback scratch mode = %04o, want 0700", got)
	}

	retargetCanary := []byte("retarget canary\n")
	writeRecoveryTestFile(t, fixture.retargetedPath, retargetCanary)
	fixture.retarget(t)
	partial := fixture.before
	writeRecoveryTestFile(t, fixture.admittedPath, partial)
	rollback.entries[0].attempted = true
	rollback.entries[0].effectKnown = true
	rollback.entries[0].effectState = recoveryWholePathState{
		existed: true, kind: recovery.PathKindFile,
		contentHash: string(artifact.HashFileContentWithExecutable(partial, false)),
		fileMode:    0o600,
	}
	beginGeneralRecoveryExecutionForTest(t, authority)

	if err := rollback.restore(context.Background(), authority, visibilityEffectGate{}); err != nil {
		t.Fatalf("restore recovery rollback: %v", err)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, fixture.after)
	assertRecoveryTestContent(t, fixture.retargetedPath, retargetCanary)
	if err := rollback.cleanup(context.Background(), authority); err != nil {
		t.Fatalf("cleanup rollback scratch: %v", err)
	}
	if _, err := os.Lstat(rollbackDir); !os.IsNotExist(err) {
		t.Fatalf("rollback scratch stat after cleanup = %v, want absence", err)
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
	consume, err := recovery.NewArtifactWork(
		0,
		authority.physicalWorkBudget.RemainingBytes()-remainingForStage,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.physicalWorkBudget.AdmitTree(consume); err != nil {
		t.Fatal(err)
	}

	_, err = stageRecoveryRollback(
		t.Context(),
		authority,
		[]recoveryHostAction{recoveryHostActionFromJournalAction(fixture.plan.Actions()[0])},
		testAggregateCodecs(),
	)
	if err == nil || !strings.Contains(err.Error(), "operation limit") {
		t.Fatalf("stageRecoveryRollback error = %v, want pre-effect operation-limit refusal", err)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, fixture.after)
}

func TestRecoveryRollbackCleanupRejectsScratchGrowth(t *testing.T) {
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

	rollback, err := stageRecoveryRollback(
		t.Context(),
		authority,
		[]recoveryHostAction{recoveryHostActionFromJournalAction(fixture.plan.Actions()[0])},
		testAggregateCodecs(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(rollback.dir) })
	if err := os.WriteFile(filepath.Join(rollback.dir, "unexpected"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	beginGeneralRecoveryExecutionForTest(t, authority)

	err = rollback.cleanup(t.Context(), authority)
	if err == nil || !strings.Contains(err.Error(), "changed before cleanup") {
		t.Fatalf("rollback cleanup error = %v, want scratch-identity rejection", err)
	}
	if _, err := os.Stat(rollback.dir); err != nil {
		t.Fatalf("scratch changed after rejected cleanup: %v", err)
	}
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
	prepareRecoveryBackupsForTest(t, t.Context(), authority, fixture.plan, []recoveryHostAction{action})
	backup, err := authority.recoveryBackupForAction(action)
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

	if _, err := backup.readFile(t.Context()); err == nil {
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
	prepareRecoveryBackupsForTest(t, t.Context(), authority, fixture.plan, []recoveryHostAction{action})
	backup, err := authority.recoveryBackupForAction(action)
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

	read, err := backup.readFile(t.Context())
	if err != nil {
		t.Fatalf("read equivalent replacement: %v", err)
	}
	if string(read) != string(content) {
		t.Fatalf("recovery backup content = %q, want %q", read, content)
	}
}

func TestRecoveryBackupRejectsOversizedRegularFile(t *testing.T) {
	budget, err := recovery.NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	work, err := recovery.NewArtifactWork(0, recovery.MaximumRecoveryBackupFileBytes+1)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveBackupFileExecution(work); err == nil ||
		!strings.Contains(err.Error(), "134217728") {
		t.Fatalf("recovery backup reservation error = %v, want bounded rejection", err)
	}
}

func TestRecoveryRollbackRejectsReplacedStageArtifact(t *testing.T) {
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
	rollback, err := stageRecoveryRollback(
		t.Context(),
		authority,
		[]recoveryHostAction{hostAction},
		testAggregateCodecs(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rollback.cleanup(context.Background(), authority) })

	partial := fixture.before
	writeRecoveryTestFile(t, fixture.admittedPath, partial)
	rollback.entries[0].attempted = true
	rollback.entries[0].effectKnown = true
	rollback.entries[0].effectState = recoveryWholePathState{
		existed: true, kind: recovery.PathKindFile,
		contentHash: string(artifact.HashFileContentWithExecutable(partial, false)),
		fileMode:    0o600,
	}
	if err := os.WriteFile(
		rollback.entries[0].backupPath,
		[]byte(strings.Repeat("x", len(fixture.after))),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	beginGeneralRecoveryExecutionForTest(t, authority)

	err = rollback.restore(t.Context(), authority, visibilityEffectGate{})
	if err == nil || !strings.Contains(err.Error(), "does not match expected hash") {
		t.Fatalf("rollback.restore error = %v, want replaced-stage rejection", err)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, partial)
}

func TestRecoveryRollbackStagesOneBaselinePerSharedDocument(t *testing.T) {
	fixture := newMCPProjectionApplyFixture(t)
	const actionCount = 64
	content := []byte(nil)
	actions := make([]recoveryHostAction, 0, actionCount)
	for index := range actionCount {
		serverID := fmt.Sprintf("server-%03d", index)
		canonical := fixture.canonicalEntry(t, serverID, "npx")
		var err error
		content, err = mergeMCPPlacementCanonicalEntry(
			t,
			aggregate.MCPPlacementClaudeProject,
			content,
			serverID,
			canonical,
		)
		if err != nil {
			t.Fatalf("merge shared projection %q: %v", serverID, err)
		}
		placement, ok := aggregate.ImplementedMCPPlacement(
			target.TargetClaudeCode,
			target.ScopeProject,
		)
		if !ok {
			t.Fatal("Claude project MCP placement is missing")
		}
		contract, err := placement.ProjectionContract(serverID)
		if err != nil {
			t.Fatalf("projection contract %q: %v", serverID, err)
		}
		actions = append(actions, recoveryHostAction{
			Scope:             target.ScopeProject,
			Destination:       fixture.destination.String(),
			ContentPath:       fixture.contentPath(serverID),
			AggregateContract: &contract,
			ExpectedAfter: recovery.ExpectedPathState{
				Existed:     true,
				PathExisted: true,
				PathMode:    recovery.NewPermissionMode(0o600),
				Kind:        recovery.PathKindFile,
				ContentHash: string(artifact.HashFileContent(canonical)),
			},
		})
	}
	fixture.writeMCPConfig(t, content)
	authority, err := captureMutationAuthority(
		fixture.paths,
		true,
		nil,
		destinationResolver(fixture.paths),
		testFilesystem(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authority.close() })

	rollback, err := stageRecoveryRollback(
		t.Context(),
		authority,
		actions,
		testAggregateCodecs(),
	)
	if err != nil {
		t.Fatalf("stageRecoveryRollback: %v", err)
	}
	t.Cleanup(func() { _ = rollback.cleanup(context.Background(), authority) })
	if len(rollback.entries) != actionCount {
		t.Fatalf("rollback entries = %d, want %d action-aligned entries", len(rollback.entries), actionCount)
	}
	backupPath := rollback.entries[0].backupPath
	if backupPath == "" {
		t.Fatal("shared document has no staged baseline")
	}
	for index, entry := range rollback.entries {
		if entry.backupPath != backupPath {
			t.Fatalf("rollback entry[%d] backup = %q, want shared %q", index, entry.backupPath, backupPath)
		}
	}
	staged, err := os.ReadDir(rollback.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 1 {
		t.Fatalf("staged rollback artifacts = %d, want one per physical document", len(staged))
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
				current, err := journal.LoadActivePlanWithOptions(ctx, fixture.paths.journalPaths(), options)
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
		[]recoveryHostAction{hostAction},
	)
	rollback, err := stageRecoveryRollback(
		context.Background(),
		authority,
		[]recoveryHostAction{hostAction},
		testAggregateCodecs(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rollback.cleanup(context.Background(), authority) })
	beginGeneralRecoveryExecutionForTest(t, authority)

	external := []byte("external after staging\n")
	writeRecoveryTestFile(t, fixture.admittedPath, external)
	err = executeRecoveryHostActions(
		context.Background(),
		authority,
		[]recoveryHostAction{hostAction},
		rollback.entries,
		nil,
		nil,
		testAggregateCodecs(),
		visibilityEffectGate{},
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
		[]recoveryHostAction{hostAction},
	)
	rollback, err := stageRecoveryRollback(
		context.Background(),
		authority,
		[]recoveryHostAction{hostAction},
		testAggregateCodecs(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rollback.cleanup(context.Background(), authority) })
	beginGeneralRecoveryExecutionForTest(t, authority)
	if err := executeRecoveryHostActions(
		context.Background(),
		authority,
		[]recoveryHostAction{hostAction},
		rollback.entries,
		nil,
		nil,
		testAggregateCodecs(),
		visibilityEffectGate{},
	); err != nil {
		t.Fatal(err)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, fixture.before)

	external := []byte("external after recovery effect\n")
	writeRecoveryTestFile(t, fixture.admittedPath, external)
	err = rollback.restore(context.Background(), authority, visibilityEffectGate{})
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
	hostActions := make([]recoveryHostAction, len(contents))
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
		hostActions[index] = recoveryHostAction{
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
		logical, parseErr := recoveryDestination(action.Scope, action.Destination)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if err := authority.bindPhysicalAuthority(action.Scope, logical, action.ConsumerTargets); err != nil {
			t.Fatal(err)
		}
	}
	for index, action := range hostActions {
		logical, parseErr := output.Parse(action.Destination)
		if parseErr != nil {
			t.Fatalf("parse recovery test destination %d: %v", index, parseErr)
		}
		destination, resolveErr := authority.resolveBoundDestination(action.Scope, logical)
		if resolveErr != nil {
			t.Fatalf("resolve recovery test destination %d: %v", index, resolveErr)
		}
		bindTestFileRemovalIntent(t, authority, destination, contents[index])
	}
	rollback, err := stageRecoveryRollback(t.Context(), authority, hostActions, testAggregateCodecs())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rollback.cleanup(context.Background(), authority) })
	if err := authority.prepareRecoveryForwardRemovals(
		t.Context(),
		hostActions,
		testAggregateCodecs(),
	); err != nil {
		t.Fatalf("prepare bounded test removals: %v", err)
	}
	beginGeneralRecoveryExecutionForTest(t, authority)
	validations := 0
	accepts := 0
	gate := visibilityEffectGate{
		before: func(context.Context) error {
			validations++
			if validations == 2 {
				return errors.New("injected visibility authority loss")
			}
			return nil
		},
		after: func(context.Context) error {
			accepts++
			return nil
		},
	}
	err = executeRecoveryHostActions(
		t.Context(),
		authority,
		hostActions,
		rollback.entries,
		nil,
		nil,
		testAggregateCodecs(),
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

func recoveryHostActionFromJournalAction(action recovery.Action) recoveryHostAction {
	return recoveryHostAction{
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
		mutationRequest journal.ManagedPathMutation
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
		mutationRequest, err = journal.NewManagedPathReplaceMutation(
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
		mutationRequest, err = journal.NewManagedPathCreateMutation(
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
	operationID := journal.OperationID(time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC))
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
	address, err := ownership.NewManagedAddress(mustObservedPathAuthority(t, managedPathKey), "")
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
	if _, err := journal.CaptureJournalWithOptions(
		context.Background(),
		paths.journalPaths(),
		operationID,
		time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC),
		currentState,
		nextState,
		journal.CaptureOptions{
			Filesystem:           testFilesystem(),
			ClaimTransitions:     []ownershipmutation.ClaimTransition{transition},
			ManagedPathMutations: []journal.ManagedPathMutation{mutationRequest},
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
		paths.journalPaths(),
		journal.PlanLoadOptions{
			Filesystem:        testFilesystem(),
			Resolver:          destinationResolver(paths),
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
