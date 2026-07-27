package recover

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal"
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
			StateEncoder:         statefile.Codec{},
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

func writeRecoverTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
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
