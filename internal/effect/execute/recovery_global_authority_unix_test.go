//go:build darwin || linux

package execute

import (
	"context"
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

				if err := ExecuteRecoveryPlanWithOptions(
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
		fixture.plan.GuardedActions(),
		destinationResolver(fixture.paths),
		testFilesystem(),
		nil,
	)
	if err != nil {
		t.Fatalf("newRecoveryMutationAuthority: %v", err)
	}
	t.Cleanup(func() { _ = authority.close() })
	planned := fixture.plan.Actions()[0]

	rollback, err := stageRecoveryRollback(
		context.Background(),
		authority,
		[]recoveryHostAction{{
			Scope:         target.ScopeGlobal,
			Destination:   destination.String(),
			ExpectedAfter: planned.ExpectedAfter.Clone(),
		}},
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
	partial := []byte("partial recovery write\n")
	writeRecoveryTestFile(t, fixture.admittedPath, partial)
	rollback.entries[0].attempted = true
	rollback.entries[0].effectKnown = true
	rollback.entries[0].effectState = recoveryWholePathState{
		existed: true, kind: recovery.PathKindFile,
		contentHash: string(artifact.HashFileContentWithExecutable(partial, false)),
		fileMode:    0o600,
	}

	if err := rollback.restore(context.Background(), authority); err != nil {
		t.Fatalf("restore recovery rollback: %v", err)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, fixture.after)
	assertRecoveryTestContent(t, fixture.retargetedPath, retargetCanary)
	if err := rollback.cleanup(); err != nil {
		t.Fatalf("cleanup rollback scratch: %v", err)
	}
	if _, err := os.Lstat(rollbackDir); !os.IsNotExist(err) {
		t.Fatalf("rollback scratch stat after cleanup = %v, want absence", err)
	}
}

func TestRecoveryBackupRejectsReplacementAfterViewSelection(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	fixture := newGlobalFileRecoveryFixture(t, destination, true)
	action := recoveryHostActionFromJournalAction(fixture.plan.Actions()[0])
	backup, err := recoveryBackupForAction(fixture.plan.OperationDir(), action)
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

	if _, err := backup.readFile(t.Context()); err == nil ||
		!strings.Contains(err.Error(), "does not match expected hash") {
		t.Fatalf("recovery backup read error = %v, want exact-byte rejection", err)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, fixture.after)
}

func TestRecoveryBackupAcceptsEquivalentReplacementAfterViewSelection(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	fixture := newGlobalFileRecoveryFixture(t, destination, true)
	action := recoveryHostActionFromJournalAction(fixture.plan.Actions()[0])
	backup, err := recoveryBackupForAction(fixture.plan.OperationDir(), action)
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
	path := filepath.Join(t.TempDir(), "backup")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(journal.MaximumRecoveryBackupFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	backup, err := newRecoveryBackup(
		path,
		"files/000001",
		string(artifact.ArtifactKindFile),
		"sha256:"+strings.Repeat("0", 64),
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := backup.readFile(t.Context()); err == nil ||
		!strings.Contains(err.Error(), "134217728") {
		t.Fatalf("recovery backup read error = %v, want bounded rejection", err)
	}
}

func TestRecoveryRollbackRejectsReplacedStageArtifact(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	fixture := newGlobalFileRecoveryFixture(t, destination, true)
	authority, err := newRecoveryMutationAuthority(
		fixture.paths,
		fixture.plan.GuardedActions(),
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
	t.Cleanup(func() { _ = rollback.cleanup() })

	partial := []byte("partial recovery effect\n")
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
		[]byte("replaced rollback stage\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	err = rollback.restore(t.Context(), authority)
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
	t.Cleanup(func() { _ = rollback.cleanup() })
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
	err := ExecuteRecoveryPlanWithOptions(
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

func TestRecoveryDirectFileCommitUsesStagedEntryIdentity(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	fixture := newGlobalFileRecoveryFixture(t, destination, true)
	authority, err := newRecoveryMutationAuthority(
		fixture.paths,
		fixture.plan.GuardedActions(),
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
	rollback, err := stageRecoveryRollback(
		context.Background(),
		authority,
		[]recoveryHostAction{hostAction},
		testAggregateCodecs(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rollback.cleanup() })

	external := []byte("external after staging\n")
	writeRecoveryTestFile(t, fixture.admittedPath, external)
	err = executeRecoveryHostActions(
		context.Background(),
		fixture.plan.OperationDir(),
		authority,
		[]recoveryHostAction{hostAction},
		rollback.entries,
		nil,
		testAggregateCodecs(),
	)
	if err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("executeRecoveryHostActions error = %v, want staged-identity rejection", err)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, external)
}

func TestRecoveryRollbackRefusesExternalChangeAfterCommittedRecoveryEffect(t *testing.T) {
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	fixture := newGlobalFileRecoveryFixture(t, destination, true)
	authority, err := newRecoveryMutationAuthority(
		fixture.paths,
		fixture.plan.GuardedActions(),
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
		context.Background(),
		authority,
		[]recoveryHostAction{hostAction},
		testAggregateCodecs(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rollback.cleanup() })
	if err := executeRecoveryHostActions(
		context.Background(),
		fixture.plan.OperationDir(),
		authority,
		[]recoveryHostAction{hostAction},
		rollback.entries,
		nil,
		testAggregateCodecs(),
	); err != nil {
		t.Fatal(err)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, fixture.before)

	external := []byte("external after recovery effect\n")
	writeRecoveryTestFile(t, fixture.admittedPath, external)
	err = rollback.restore(context.Background(), authority)
	if err == nil || !strings.Contains(err.Error(), "changed outside the recovery attempt") {
		t.Fatalf("rollback.restore error = %v, want external-change refusal", err)
	}
	assertRecoveryTestContent(t, fixture.admittedPath, external)
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
	owner, err := stateauthority.New(statefileKey, manifestPath)
	if err != nil {
		t.Fatalf("construct global recovery owner: %v", err)
	}
	managedPathKey, err := mutation.CanonicalDirectoryEntryKey(admittedPath)
	if err != nil {
		t.Fatalf("canonicalize global recovery managed path: %v", err)
	}
	address, err := ownership.NewManagedAddress(managedPathKey, "")
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
			OwnershipRegistry: registry.Load,
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
