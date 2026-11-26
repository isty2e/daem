package execute

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/entity"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	"github.com/isty2e/daem/internal/effect/journal/qualification"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"

	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestRecoveryRestoresProjectDirectoryThroughRootAuthority(t *testing.T) {
	t.Run("labels from verified backup", func(t *testing.T) { restoreLegacyProjectTree(t, false, false) })
	t.Run("chmod after staging with reference for changed labels", func(t *testing.T) { restoreLegacyProjectTree(t, true, false) })
	t.Run("late content edit is retained", func(t *testing.T) { restoreLegacyProjectTree(t, false, true) })
}

type afterPreparedRestore struct {
	mutationfs.Store
	afterPrepare func()
}

func (s afterPreparedRestore) PrepareRootedTree(ctx context.Context, capability rootedpath.CommitCapability, populate func(mutationfs.RootedTreeWriter) error) (mutationfs.PreparedRootedTree, error) {
	prepared, err := s.Store.PrepareRootedTree(ctx, capability, populate)
	if err == nil {
		s.afterPrepare()
	}
	return prepared, err
}

func restoreLegacyProjectTree(t *testing.T, supplyReference, editDuringRestore bool) {
	t.Helper()
	base := t.TempDir()
	projectRoot := filepath.Join(base, "project")
	stateDir := filepath.Join(base, "state")
	destination := ".agents/skills/review"
	hostPath := filepath.Join(projectRoot, filepath.FromSlash(destination))
	if err := os.MkdirAll(filepath.Join(hostPath, "nested"), 0o700); err != nil {
		t.Fatalf("create before project tree: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(filepath.Join(hostPath, "nested"), 0o700)
		_ = os.Chmod(hostPath, 0o700)
	})
	writeRecoveryTestFile(t, filepath.Join(hostPath, "SKILL.md"), []byte("before skill\n"))
	if !supplyReference {
		if err := os.Chmod(filepath.Join(hostPath, "SKILL.md"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeRecoveryTestFile(t, filepath.Join(hostPath, "nested", "before.txt"), []byte("before nested\n"))
	if err := os.Chmod(filepath.Join(hostPath, "nested"), 0o500); err != nil {
		t.Fatalf("make before nested directory read-only: %v", err)
	}
	if err := os.Chmod(hostPath, 0o550); err != nil {
		t.Fatalf("make before project tree read-only: %v", err)
	}
	beforeHash, _, err := access.HashPath(context.Background(), hostPath)
	if err != nil {
		t.Fatalf("hash before project tree: %v", err)
	}

	afterSource := filepath.Join(base, "after")
	if err := os.MkdirAll(filepath.Join(afterSource, "nested"), 0o700); err != nil {
		t.Fatalf("create after project tree: %v", err)
	}
	writeRecoveryTestFile(t, filepath.Join(afterSource, "SKILL.md"), []byte("after skill\n"))
	writeRecoveryTestFile(t, filepath.Join(afterSource, "nested", "after.txt"), []byte("after nested\n"))
	if err := os.Chmod(filepath.Join(afterSource, "SKILL.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	var baseline recovery.TreeBaseline
	if supplyReference {
		hashes, err := qualification.ReadBaselineTree(t.Context(), afterSource, physicalwork.NewCaptureBudget(nil))
		if err != nil {
			t.Fatal(err)
		}
		baseline = recovery.VerifiedTree(string(hashes.Source), string(hashes.Installed))
	}
	afterHash, _, err := access.HashPath(context.Background(), afterSource)
	if err != nil {
		t.Fatalf("hash after project tree: %v", err)
	}

	paths := Paths{
		RecoveryDir:   filepath.Join(stateDir, "recovery"),
		StateDir:      stateDir,
		StatefilePath: filepath.Join(stateDir, "state.json"),
		ManifestRoot:  projectRoot,
		DataDir:       filepath.Join(stateDir, "data"),
	}
	placements, err := profile.ManagedPathPlacementsForSelections(
		entity.KindSkill,
		target.ScopeProject,
		[]target.Target{target.TargetCodex},
		nil,
	)
	if err != nil {
		t.Fatalf("derive Skill placement: %v", err)
	}
	placementID := ""
	for _, placement := range placements {
		if _, err := placement.ChildName(outputtest.Parse(t, destination)); err == nil {
			placementID = placement.ID()
			break
		}
	}
	if placementID == "" {
		t.Fatalf("no Skill placement owns %q", destination)
	}
	entityID, err := entity.New(entity.KindSkill, "review")
	if err != nil {
		t.Fatalf("construct Skill entity: %v", err)
	}
	subject, err := topologyprojection.Subject(entityID, placementID)
	if err != nil {
		t.Fatalf("lower Skill projection subject: %v", err)
	}
	previous, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, destination),
		beforeHash,
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
		0,
	)
	if err != nil {
		t.Fatalf("construct managed path state: %v", err)
	}
	mutation, err := journalrecord.NewManagedPathReplaceMutation(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, destination),
		afterHash,
		beforeHash,
		realization.PathProjectionDirectory,
		0,
		previous,
	)
	if err != nil {
		t.Fatalf("construct managed path mutation: %v", err)
	}
	evidence, err := observe.NewManagedPathEvidence(
		subject,
		outputtest.Parse(t, destination),
		true,
		beforeHash,
		0o550,
	)
	if err != nil {
		t.Fatalf("construct managed path evidence: %v", err)
	}
	next, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, destination),
		afterHash,
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
		0,
	)
	if err != nil {
		t.Fatalf("construct next managed path state: %v", err)
	}
	currentState, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths: []durable.ManagedPathState{previous},
	})
	if err != nil {
		t.Fatalf("construct current snapshot: %v", err)
	}
	nextState, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths: []durable.ManagedPathState{next},
	})
	if err != nil {
		t.Fatalf("construct next snapshot: %v", err)
	}
	createdAt := time.Date(2026, time.July, 13, 13, 0, 0, 0, time.UTC)
	removalDemands := testManagedPathRemovalDemandSet(t, &previous, 0, &next, 0)
	if _, err := journalpublication.CaptureJournalWithOptions(
		context.Background(),
		paths.JournalPaths(),
		journalpublication.OperationID(createdAt),
		createdAt,
		currentState,
		nextState,
		journalpublication.CaptureOptions{
			Filesystem:           testFilesystem(),
			ManagedPathMutations: []journalrecord.ManagedPathMutation{mutation},
			ManagedPathEvidence:  []observe.ManagedPathEvidence{evidence},
			RemovalDemands:       removalDemands,
			Resolver:             journalDestinationResolver(paths),
			StateCodec:           testStateCodec(),
		},
	); err != nil {
		t.Fatalf("CaptureJournal returned error: %v", err)
	}
	if err := os.Chmod(filepath.Join(hostPath, "nested"), 0o700); err != nil {
		t.Fatalf("make before nested directory writable for fixture replacement: %v", err)
	}
	if err := os.Chmod(hostPath, 0o700); err != nil {
		t.Fatalf("make before project tree writable for fixture replacement: %v", err)
	}
	if err := os.RemoveAll(hostPath); err != nil {
		t.Fatalf("remove before project tree: %v", err)
	}
	if err := os.Rename(afterSource, hostPath); err != nil {
		t.Fatalf("publish after project tree: %v", err)
	}
	if err := os.Chmod(filepath.Join(hostPath, "nested"), 0o500); err != nil {
		t.Fatalf("make after nested directory read-only: %v", err)
	}
	if err := os.Chmod(hostPath, 0o500); err != nil {
		t.Fatalf("make after project tree read-only: %v", err)
	}
	writeRecoveryTestStatefile(t, paths.StatefilePath, currentState)
	if err := os.Chmod(filepath.Join(hostPath, "SKILL.md"), 0o640); err != nil {
		t.Fatal(err)
	}
	load := testPlanLoadOptions(paths)
	load.TreeBaseline = baseline

	recoveryPlan, err := journal.LoadActivePlanWithOptions(
		context.Background(),
		paths.JournalPaths(),
		load,
	)
	if err != nil {
		t.Fatalf("LoadActivePlan returned error: %v", err)
	}
	options := testRecoveryOptions(paths)
	options.TreeBaseline = load.TreeBaseline
	file := filepath.Join(hostPath, "SKILL.md")
	wantMode := os.FileMode(0o640)
	if supplyReference || editDuringRestore {
		wantMode = 0o644
		options.Filesystem = afterPreparedRestore{options.Filesystem, func() {
			if err := os.Chmod(file, wantMode); err != nil {
				t.Fatal(err)
			}
			if editDuringRestore {
				if err := os.WriteFile(file, []byte("user edit\n"), wantMode); err != nil {
					t.Fatal(err)
				}
			}
		}}
	}
	err = executeRecoveryPlanWithOptionsForTest(
		context.Background(),
		recoveryPlan,
		paths,
		options,
	)
	assertRecoveryTestMode(t, file, wantMode)
	if editDuringRestore {
		if err == nil {
			t.Fatal("recovery accepted a concurrent edit")
		}
		assertRecoveryTestContent(t, file, []byte("user edit\n"))
		assertRecoveryTestContent(t, filepath.Join(hostPath, "nested", "after.txt"), []byte("after nested\n"))
		if err := os.WriteFile(file, []byte("after skill\n"), wantMode); err != nil {
			t.Fatal(err)
		}
		retry, loadErr := journal.LoadActivePlanWithOptions(t.Context(), paths.JournalPaths(), load)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		err = executeRecoveryPlanWithOptionsForTest(t.Context(), retry, paths, testRecoveryOptions(paths))
	}
	if err != nil {
		t.Fatalf("ExecuteRecoveryPlan returned error: %v", err)
	}
	assertRecoveryTestContent(t, filepath.Join(hostPath, "SKILL.md"), []byte("before skill\n"))
	assertRecoveryTestContent(t, filepath.Join(hostPath, "nested", "before.txt"), []byte("before nested\n"))
	if _, err := os.Stat(filepath.Join(hostPath, "nested", "after.txt")); !os.IsNotExist(err) {
		t.Fatalf("after-only path stat error = %v, want absence", err)
	}
	assertRecoveryTestMode(t, hostPath, 0o500)
	assertRecoveryTestMode(t, filepath.Join(hostPath, "nested"), 0o500)
}
