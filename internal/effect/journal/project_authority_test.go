package journal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func hasRootedPathFailureKind(err error, kind rootedpath.FailureKind) bool {
	var failure *rootedpath.Failure
	return errors.As(err, &failure) && failure.Kind() == kind
}

func TestBuildRecoveryJournalBorrowsProjectRootAndPersistsProvenance(t *testing.T) {
	root := t.TempDir()
	hostPath := filepath.Join(root, "AGENTS.md")
	content := []byte("instructions\n")
	if err := os.WriteFile(hostPath, content, 0o600); err != nil {
		t.Fatalf("write project destination: %v", err)
	}
	contentHash := artifact.HashFileContent(content)
	mutation, evidence, current := projectJournalCaptureFacts(t, contentHash)
	captured := mustJournalProjectRoot(t, root)
	defer captured.Close()

	journal, err := buildRecoveryJournal(
		context.Background(),
		Paths{ManifestRoot: root},
		t.TempDir(),
		"borrowed-project-root",
		time.Date(2026, time.July, 13, 0, 0, 0, 0, time.UTC),
		current,
		current,
		CaptureOptions{
			Filesystem:           journalTestFilesystem(),
			ProjectRoot:          captured,
			ManagedPathMutations: []ManagedPathMutation{mutation},
			ManagedPathEvidence:  []observe.ManagedPathEvidence{evidence},
			StateCodec:           testStateCodec(),
			Resolver: func(output.Destination) (string, error) {
				return hostPath, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("buildRecoveryJournal returned error: %v", err)
	}
	authority, err := captured.Authority()
	if err != nil {
		t.Fatalf("borrowed root was closed by journal capture: %v", err)
	}
	if journal.ProjectRootProvenance == nil {
		t.Fatal("journal project_root_provenance is nil")
	}
	persisted, err := journal.ProjectRootProvenance.canonical()
	if err != nil {
		t.Fatalf("canonical persisted provenance: %v", err)
	}
	if err := persisted.Match(authority); err != nil {
		t.Fatalf("persisted provenance does not match borrowed authority: %v", err)
	}
}

func TestLoadActivePlanRejectsReplacedProjectRootBeforePathClassification(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	stateRoot := filepath.Join(base, "state")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create project root: %v", err)
	}
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatalf("create state root: %v", err)
	}
	hostPath := filepath.Join(root, "AGENTS.md")
	content := []byte("instructions\n")
	if err := os.WriteFile(hostPath, content, 0o600); err != nil {
		t.Fatalf("write project destination: %v", err)
	}
	contentHash := artifact.HashFileContent(content)
	mutation, evidence, current := projectJournalCaptureFacts(t, contentHash)
	paths := Paths{
		RecoveryDir:   filepath.Join(stateRoot, "recovery"),
		StatefilePath: filepath.Join(stateRoot, "state.json"),
		ManifestRoot:  root,
		DataDir:       filepath.Join(stateRoot, "data"),
	}
	captured := mustJournalProjectRoot(t, root)
	_, err := CaptureJournalWithOptions(
		context.Background(),
		paths,
		"replace-root",
		time.Date(2026, time.July, 13, 0, 0, 0, 0, time.UTC),
		current,
		current,
		CaptureOptions{
			Filesystem:           journalTestFilesystem(),
			ProjectRoot:          captured,
			ManagedPathMutations: []ManagedPathMutation{mutation},
			ManagedPathEvidence:  []observe.ManagedPathEvidence{evidence},
			StateCodec:           testStateCodec(),
			Resolver: func(output.Destination) (string, error) {
				return hostPath, nil
			},
		},
	)
	if err != nil {
		_ = captured.Close()
		t.Fatalf("CaptureJournalWithOptions returned error: %v", err)
	}
	if err := captured.Close(); err != nil {
		t.Fatalf("close original project root witness: %v", err)
	}

	moved := filepath.Join(base, "moved-project")
	if err := os.Rename(root, moved); err != nil {
		t.Fatalf("move original project root: %v", err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create replacement project root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), content, 0o600); err != nil {
		t.Fatalf("write matching replacement content: %v", err)
	}

	_, err = LoadActivePlanWithOptions(context.Background(), paths, PlanLoadOptions{
		Filesystem: journalTestFilesystem(),
		Resolver: func(output.Destination) (string, error) {
			return filepath.Join(root, "AGENTS.md"), nil
		},
		StateCodec:  testStateCodec(),
		StateReader: testStateReader(paths.StatefilePath),
	})
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("LoadActivePlan error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
}

func TestValidateProjectRootProvenanceCoverageIsBijective(t *testing.T) {
	journal := defaultRecoveryJournal()
	journal.ProjectRootProvenance = nil
	if err := validateProjectRootProvenanceCoverage(journal); err == nil ||
		!strings.Contains(err.Error(), "require project_root_provenance") {
		t.Fatalf("missing provenance error = %v", err)
	}

	journal = defaultRecoveryJournal()
	journal.Entries[0].Scope = string(target.ScopeGlobal)
	if err := validateProjectRootProvenanceCoverage(journal); err == nil ||
		!strings.Contains(err.Error(), "must not contain project_root_provenance") {
		t.Fatalf("extraneous provenance error = %v", err)
	}

	journal = defaultRecoveryJournal()
	journal.ProjectRootProvenance.ObjectFingerprint = "sha256:short"
	if err := validateProjectRootProvenanceCoverage(journal); err == nil ||
		!strings.Contains(err.Error(), "object fingerprint is invalid") {
		t.Fatalf("invalid provenance error = %v", err)
	}
}

func TestValidateRecoveryJournalRejectsVersionThree(t *testing.T) {
	journal := defaultRecoveryJournal()
	journal.Version = 3
	if err := validateRecoveryJournal(journal, testStateCodec()); err == nil ||
		!strings.Contains(err.Error(), "unsupported recovery journal version 3") {
		t.Fatalf("version 3 error = %v", err)
	}
}

func TestObserveGlobalRecoveryPathRejectsNilPresentCapability(t *testing.T) {
	observation := observeGlobalRecoveryPath(
		context.Background(),
		"~/.codex/config.toml",
		"",
		nil,
		"/expected/config.toml",
		journalTestFilesystem(),
		func(output.Destination) (rootedpath.CommitCapability, bool, error) {
			return nil, true, nil
		},
		journalTestCodecs(),
	)
	if !strings.Contains(observation.Error, "nil retained root authority") {
		t.Fatalf("observation error = %q, want nil capability refusal", observation.Error)
	}
}

func TestObserveGlobalRecoveryPathRejectsResolverCapabilityMismatch(t *testing.T) {
	root, destination, err := rootedpath.CaptureDestination(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatalf("CaptureDestination returned error: %v", err)
	}
	defer root.Close()

	observation := observeGlobalRecoveryPath(
		context.Background(),
		"~/.codex/config.toml",
		"",
		nil,
		filepath.Join(t.TempDir(), "different.toml"),
		journalTestFilesystem(),
		func(output.Destination) (rootedpath.CommitCapability, bool, error) {
			capability, err := root.Acquire(destination)
			return capability, true, err
		},
		journalTestCodecs(),
	)
	if !strings.Contains(observation.Error, "does not match retained authority path") {
		t.Fatalf("observation error = %q, want resolver/capability mismatch refusal", observation.Error)
	}
}

func projectJournalCaptureFacts(
	t *testing.T,
	contentHash artifact.ContentHash,
) (ManagedPathMutation, observe.ManagedPathEvidence, durable.Snapshot) {
	t.Helper()
	previous := testAppliedState(target.TargetCodex, string(contentHash))
	mutation, err := NewManagedPathRecordMutation(
		previous.Subject(),
		previous.ConsumerTargets(),
		previous.Scope(),
		previous.Destination(),
		contentHash,
		contentHash,
		previous.ContentKind(),
		0o600,
		&previous,
	)
	if err != nil {
		t.Fatalf("NewManagedPathRecordMutation returned error: %v", err)
	}
	evidence, err := observe.NewManagedPathEvidence(
		previous.Subject(),
		previous.Destination(),
		true,
		contentHash,
		0o600,
	)
	if err != nil {
		t.Fatalf("NewManagedPathEvidence returned error: %v", err)
	}
	return mutation, evidence, statefileFor(previous)
}

func mustJournalProjectRoot(t *testing.T, root string) *rootedpath.CapturedRoot {
	t.Helper()
	captured, err := rootedpath.CaptureRoot(root)
	if err != nil {
		t.Fatalf("CaptureRoot(%q) returned error: %v", root, err)
	}
	return captured
}
