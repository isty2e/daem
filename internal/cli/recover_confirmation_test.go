package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestRunRecoverInteractiveDeclineRetainsCurrentJournal(t *testing.T) {
	fixture := writeRecoverConfirmationFixture(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := RunWithOptions(
		[]string{"recover", "--manifest", fixture.manifestPath},
		interactiveRunOptions(strings.NewReader("no\n"), &stdout, &stderr),
	)
	if exitCode != 1 || !strings.Contains(stderr.String(), "recover canceled") {
		t.Fatalf("exitCode = %d stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "needs_rollback") || !strings.Contains(stderr.String(), "Proceed with recover? [y/N]:") {
		t.Fatalf("stdout = %q stderr = %q", stdout.String(), stderr.String())
	}
	fixture.assertUnchanged(t)
}

func TestRunRecoverInteractiveAcceptanceExecutesDisclosedPlan(t *testing.T) {
	fixture := writeRecoverConfirmationFixture(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := RunWithOptions(
		[]string{"recover", "--manifest", fixture.manifestPath},
		interactiveRunOptions(strings.NewReader("yes\n"), &stdout, &stderr),
	)
	if exitCode != 0 || !strings.Contains(stdout.String(), "recovery completed") {
		t.Fatalf("exitCode = %d stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	content, err := os.ReadFile(fixture.hostPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(fixture.oldContent) {
		t.Fatalf("recovered content = %q, want %q", content, fixture.oldContent)
	}
	if _, err := os.Stat(fixture.operationDir); !os.IsNotExist(err) {
		t.Fatalf("recovery journal exists or stat failed unexpectedly: %v", err)
	}
}

func TestRunRecoverDisclosureWriteFailureDoesNotPromptOrExecute(t *testing.T) {
	fixture := writeRecoverConfirmationFixture(t)
	input := &countingReader{reader: strings.NewReader("yes\n")}
	stdoutErr := errors.New("stdout closed")
	var stderr bytes.Buffer

	exitCode := RunWithOptions(
		[]string{"recover", "--manifest", fixture.manifestPath},
		interactiveRunOptions(input, errorWriter{err: stdoutErr}, &stderr),
	)
	if exitCode != 1 || !strings.Contains(stderr.String(), "recover failed: disclose plan: stdout closed") {
		t.Fatalf("exitCode = %d stderr = %q", exitCode, stderr.String())
	}
	if input.reads != 0 || strings.Contains(stderr.String(), "Proceed with recover?") {
		t.Fatalf("input reads = %d stderr = %q", input.reads, stderr.String())
	}
	fixture.assertUnchanged(t)
}

func TestRunRecoverContextCancellationOverridesAffirmativeAnswer(t *testing.T) {
	fixture := writeRecoverConfirmationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	input := &cancelAfterRead{reader: strings.NewReader("yes\n"), cancel: cancel}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	options := interactiveRunOptions(input, &stdout, &stderr)
	options.Context = ctx

	exitCode := RunWithOptions([]string{"recover", "--manifest", fixture.manifestPath}, options)
	if exitCode != 1 || !strings.Contains(stderr.String(), "recover canceled: context canceled") {
		t.Fatalf("exitCode = %d stdout = %q stderr = %q", exitCode, stdout.String(), stderr.String())
	}
	fixture.assertUnchanged(t)
}

type recoverConfirmationFixture struct {
	manifestPath string
	hostPath     string
	operationDir string
	oldContent   []byte
	newContent   []byte
}

func writeRecoverConfirmationFixture(t *testing.T) recoverConfirmationFixture {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg-data"))
	manifestPath := filepath.Join(root, "daem.toml")
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	hostPath := filepath.Join(root, "AGENTS.md")
	oldContent := []byte("old hooks\n")
	newContent := []byte("new hooks\n")
	oldHash := string(artifact.HashFileContent(oldContent))
	newHash := string(artifact.HashFileContent(newContent))
	destination := outputtest.Parse(t, "AGENTS.md")
	entityID, err := entity.New(entity.KindInstructions, "recovery-confirmation")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(entityID, "instructions.project.agents")
	if err != nil {
		t.Fatal(err)
	}
	writeApplyConfirmationFile(t, root, "AGENTS.md", string(oldContent))
	previousPath, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		destination,
		artifact.ContentHash(oldHash),
		realization.PathProjectionFile,
		realization.PathPermissionsExact,
		0o600,
	)
	if err != nil {
		t.Fatal(err)
	}
	desiredPath, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		destination,
		artifact.ContentHash(newHash),
		realization.PathProjectionFile,
		realization.PathPermissionsExact,
		0o600,
	)
	if err != nil {
		t.Fatal(err)
	}
	currentState, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths: []durable.ManagedPathState{previousPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	nextState, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedPaths: []durable.ManagedPathState{desiredPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := journal.NewManagedPathReplaceMutation(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		destination,
		artifact.ContentHash(newHash),
		artifact.ContentHash(oldHash),
		realization.PathProjectionFile,
		0o600,
		previousPath,
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
		journal.Paths{
			RecoveryDir: paths.RecoveryDir, StatefilePath: paths.StatefilePath,
			ManifestRoot: paths.ManifestRoot, DataDir: paths.DataDir,
		},
		journal.OperationID(time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)),
		time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC),
		currentState,
		nextState,
		journal.CaptureOptions{
			Filesystem:           storagecommit.Adapter{},
			ManagedPathMutations: []journal.ManagedPathMutation{mutation},
			ManagedPathEvidence:  []observe.ManagedPathEvidence{evidence},
			Resolver:             hostpath.NewResolver(root).Resolve,
			StateCodec:           statefile.Codec{},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	writeApplyConfirmationStatefile(t, paths.StatefilePath, currentState)
	writeApplyConfirmationFile(t, root, "AGENTS.md", string(newContent))
	return recoverConfirmationFixture{
		manifestPath: manifestPath,
		hostPath:     hostPath,
		operationDir: captured.Directory,
		oldContent:   oldContent,
		newContent:   newContent,
	}
}

func (fixture recoverConfirmationFixture) assertUnchanged(t *testing.T) {
	t.Helper()
	content, err := os.ReadFile(fixture.hostPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(fixture.newContent) {
		t.Fatalf("host content = %q, want unchanged %q", content, fixture.newContent)
	}
	if _, err := os.Stat(fixture.operationDir); err != nil {
		t.Fatalf("recovery journal missing after refusal: %v", err)
	}
}
