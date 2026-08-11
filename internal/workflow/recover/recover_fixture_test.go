package recover

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output/hostpath"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

type recoveryFixture struct {
	input        PlanInput
	hostPath     string
	operationDir string
	oldContent   []byte
	newContent   []byte
}

type cleanupRecoveryFixture struct {
	recoveryFixture
	paths      daempaths.Paths
	controlDir string
	residueDir string
	garbageDir string
	recordPath string
	record     retirement.Record
}

func prepareRecoveryFixture(t *testing.T, applied bool) recoveryFixture {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg-data"))
	input := PlanInput{ManifestPath: filepath.Join(root, "daem.toml")}
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	hostPath := filepath.Join(root, "AGENTS.md")
	oldContent := []byte("old instructions\n")
	newContent := []byte("new instructions\n")
	oldHash := string(artifact.HashFileContent(oldContent))
	newHash := string(artifact.HashFileContent(newContent))
	destination := outputtest.Parse(t, "AGENTS.md")
	placement, err := profile.ManagedFilePlacementFor(
		entity.KindInstructions,
		target.TargetCodex,
		target.ScopeProject,
		destination,
	)
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindInstructions, "project")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(id, placement.ID())
	if err != nil {
		t.Fatal(err)
	}
	writeRecoverTestFile(t, hostPath, oldContent)
	currentPath, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		destination,
		artifact.ContentHash(oldHash),
		realization.PathProjectionFile,
		realization.PathPermissionsExecutableClass,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	nextPath, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		destination,
		artifact.ContentHash(newHash),
		realization.PathProjectionFile,
		realization.PathPermissionsExecutableClass,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	currentState, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths: []durable.ManagedPathState{currentPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	nextState, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths: []durable.ManagedPathState{nextPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	mutationRequest, err := journal.NewManagedPathReplaceMutation(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		destination,
		artifact.ContentHash(newHash),
		artifact.ContentHash(oldHash),
		realization.PathProjectionFile,
		0o600,
		currentPath,
	)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(
		subject,
		destination,
		true,
		artifact.ContentHash(oldHash),
		0o600,
	)
	if err != nil {
		t.Fatal(err)
	}
	captured, err := journal.CaptureJournalWithOptions(
		context.Background(),
		journalPaths(paths),
		journal.OperationID(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)),
		time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		currentState,
		nextState,
		journal.CaptureOptions{
			Filesystem:           storagecommit.Adapter{},
			ManagedPathMutations: []journal.ManagedPathMutation{mutationRequest},
			ManagedPathEvidence:  []observe.ManagedPathEvidence{evidence},
			Resolver:             hostpath.NewResolver(root).Resolve,
			StateCodec:           statefile.Codec{},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	writeRecoverTestStatefile(t, paths.StatefilePath, currentState)
	if applied {
		writeRecoverTestFile(t, hostPath, newContent)
	}
	return recoveryFixture{
		input: input, hostPath: hostPath, operationDir: captured.Directory,
		oldContent: oldContent, newContent: newContent,
	}
}

func prepareRemovalRecoveryFixture(t *testing.T) recoveryFixture {
	t.Helper()
	fixture := prepareRecoveryFixture(t, false)
	paths, err := daempaths.Resolve(fixture.input.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(fixture.operationDir); err != nil {
		t.Fatal(err)
	}
	currentState, err := statefile.Load(t.Context(), paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	managed := currentState.ManagedPaths()
	if len(managed) != 1 {
		t.Fatalf("managed paths = %d, want 1", len(managed))
	}
	previous := managed[0]
	mutationRequest, err := journal.NewManagedPathRemoveMutation(
		previous,
		previous.ContentHash(),
	)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(
		previous.Subject(),
		previous.Destination(),
		true,
		previous.ContentHash(),
		0o600,
	)
	if err != nil {
		t.Fatal(err)
	}
	state, err := recovery.NewBeforeRemovalState(recovery.BeforePathState{
		Existed:     true,
		Kind:        recovery.PathKindFile,
		ContentHash: string(previous.ContentHash()),
		PathMode:    recovery.NewPermissionMode(0o600),
	})
	if err != nil {
		t.Fatal(err)
	}
	demand, err := recovery.NewRemovalDemand(
		previous.Scope(),
		previous.Destination(),
		[]recovery.RemovalState{state},
	)
	if err != nil {
		t.Fatal(err)
	}
	demands, err := recovery.NewRemovalDemandSet([]recovery.RemovalDemand{demand})
	if err != nil {
		t.Fatal(err)
	}
	captured, err := journal.CaptureJournalWithOptions(
		t.Context(),
		journalPaths(paths),
		journal.OperationID(time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)),
		time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC),
		currentState,
		durable.EmptySnapshot(),
		journal.CaptureOptions{
			Filesystem:           storagecommit.Adapter{},
			ManagedPathMutations: []journal.ManagedPathMutation{mutationRequest},
			ManagedPathEvidence:  []observe.ManagedPathEvidence{evidence},
			RemovalDemands:       demands,
			Resolver:             hostpath.NewResolver(filepath.Dir(fixture.input.ManifestPath)).Resolve,
			StateCodec:           statefile.Codec{},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	fixture.operationDir = captured.Directory
	return fixture
}

func prepareCleanupRecoveryFixture(
	t *testing.T,
	phase retirement.Phase,
	residuePresent bool,
) cleanupRecoveryFixture {
	t.Helper()
	fixture := prepareRecoveryFixture(t, false)
	paths, err := daempaths.Resolve(fixture.input.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := Plan(t.Context(), fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	active, ok := journal.ActiveRecoveryPlan(prepared.Disclosure())
	if !ok {
		t.Fatalf(
			"authority kind = %q, want active journal",
			prepared.Disclosure().AuthorityKind(),
		)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := active.JournalAuthorityFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	record, err := retirement.NewRecord(active.OperationID(), fingerprint, phase)
	if err != nil {
		t.Fatal(err)
	}
	identity := record.Identity()
	controlDir := filepath.Join(paths.RecoveryDir, identity.ControlName())
	if err := os.MkdirAll(controlDir, retirement.DirectoryMode); err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(controlDir, retirement.RecordFileName)
	writeRetirementRecord(t, recordPath, record)
	residueDir := filepath.Join(paths.RecoveryDir, identity.ResidueName())
	if residuePresent {
		if err := os.Rename(fixture.operationDir, residueDir); err != nil {
			t.Fatal(err)
		}
	} else if err := os.RemoveAll(fixture.operationDir); err != nil {
		t.Fatal(err)
	}
	return cleanupRecoveryFixture{
		recoveryFixture: fixture,
		paths:           paths,
		controlDir:      controlDir,
		residueDir:      residueDir,
		garbageDir:      filepath.Join(paths.RecoveryDir, identity.GCName()),
		recordPath:      recordPath,
		record:          record,
	}
}

func writeRetirementRecord(t *testing.T, path string, record retirement.Record) {
	t.Helper()
	content, err := retirement.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	writeRecoverTestFile(t, path, content)
	if err := os.Chmod(path, retirement.RecordMode); err != nil {
		t.Fatal(err)
	}
}

func writeRecoverTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readRecoverTestFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func assertRecoverPathPresent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("expected path %q: %v", path, err)
	}
}

func assertRecoverPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("path %q exists or stat failed unexpectedly: %v", path, err)
	}
}

func replaceRecoverTestTreeWithClone(t *testing.T, path string) {
	t.Helper()
	parent := filepath.Dir(path)
	replacement := filepath.Join(parent, "."+filepath.Base(path)+"-replacement")
	held := filepath.Join(t.TempDir(), filepath.Base(path))
	copyRecoverTestTree(t, path, replacement)
	if err := os.Rename(path, held); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
}

func copyRecoverTestTree(t *testing.T, source string, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			if err := os.Mkdir(target, info.Mode().Perm()); err != nil && !os.IsExist(err) {
				return err
			}
			return os.Chmod(target, info.Mode().Perm())
		case entry.Type().IsRegular():
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, content, info.Mode().Perm())
		default:
			return &fs.PathError{
				Op:   "copy recovery test tree",
				Path: path,
				Err:  fs.ErrInvalid,
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

func writeRecoverTestStatefile(t *testing.T, path string, snapshot durable.Snapshot) {
	t.Helper()
	content, err := statefile.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	writeRecoverTestFile(t, path, content)
}
