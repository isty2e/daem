package journal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func hasRootedPathFailureKind(err error, kind rootedpath.FailureKind) bool {
	var failure *rootedpath.Failure
	return errors.As(err, &failure) && failure.Kind() == kind
}

func TestObserveGlobalRecoveryPathPreservesSymlinkTargetThroughRootedFallback(t *testing.T) {
	root := t.TempDir()
	hostPath := filepath.Join(root, "current")
	if err := os.Symlink("nested/target", hostPath); err != nil {
		t.Fatalf("create global symbolic link: %v", err)
	}
	destination, err := output.Parse("~/.daem-test/current")
	if err != nil {
		t.Fatalf("parse global destination: %v", err)
	}
	observation, err := journalqualification.ObserveGlobalPath(
		t.Context(),
		destination.String(),
		"",
		nil,
		hostPath,
		journalTestFilesystem(),
		nil,
		journalTestCodecs(),
		recoveryBackupBudgetForTest(t),
	)
	if err != nil {
		t.Fatalf("observe global symbolic link: %v", err)
	}
	if observation.Error != "" ||
		observation.Kind != recovery.PathKindSymlink ||
		observation.LinkTarget != "nested/target" {
		t.Fatalf("global symbolic-link observation = %#v, want exact target", observation)
	}
}

func TestOrdinaryCaptureBorrowsProjectRootAndEncodesProvenance(t *testing.T) {
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

	material, err := buildOrdinaryCaptureMaterial(
		context.Background(),
		Paths{ManifestRoot: root},
		t.TempDir(),
		"borrowed-project-root",
		time.Date(2026, time.July, 13, 0, 0, 0, 0, time.UTC),
		current,
		current,
		CaptureOptions{
			Filesystem:           journalTestFilesystem(),
			ManifestRoot:         captured,
			ManagedPathMutations: []journalrecord.ManagedPathMutation{mutation},
			ManagedPathEvidence:  []observe.ManagedPathEvidence{evidence},
			StateCodec:           testStateCodec(),
			Resolver: func(output.Destination) (string, error) {
				return hostPath, nil
			},
		},
		&ordinaryCaptureWork{},
	)
	if err != nil {
		t.Fatalf("ordinary capture returned error: %v", err)
	}
	authority, err := captured.Authority()
	if err != nil {
		t.Fatalf("borrowed root was closed by journal capture: %v", err)
	}
	before, after, err := journalrecord.EncodeSnapshots(current, current, testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := material.EncodeOrdinary(before, after)
	if err != nil {
		t.Fatal(err)
	}
	journalRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(journalRoot, "journal.json")
	if err := os.WriteFile(journalPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := journalTestFilesystem().ReadRegularFileSnapshotUpTo(t.Context(), journalPath, journalrecord.MaximumBytes)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := journalrecord.DecodeOrdinarySnapshot(t.Context(), snapshot, journalPath, testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	journal, present := loaded.Ordinary()
	if !present {
		t.Fatal("ordinary capture decoded as another family")
	}
	basis, err := journal.RecoveryPlanningBasis(journalRoot, journalrecord.Fingerprint(encoded), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := basis.ManifestProvenance().Match(authority); err != nil {
		t.Fatalf("persisted provenance does not match borrowed authority: %v", err)
	}
}

func TestOrdinaryCaptureRejectsBorrowedRootForDifferentManifest(t *testing.T) {
	manifestRoot := t.TempDir()
	borrowed := mustJournalProjectRoot(t, t.TempDir())
	defer borrowed.Close()

	_, err := buildOrdinaryCaptureMaterial(
		context.Background(),
		Paths{ManifestRoot: manifestRoot},
		t.TempDir(),
		"mismatched-manifest-root",
		time.Date(2026, time.August, 6, 0, 0, 0, 0, time.UTC),
		durable.EmptySnapshot(),
		inventoryJournalSnapshot(t, "after"),
		CaptureOptions{
			Filesystem:   journalTestFilesystem(),
			ManifestRoot: borrowed,
			Resolver:     func(output.Destination) (string, error) { return "", nil },
			StateCodec:   testStateCodec(),
		},
		&ordinaryCaptureWork{},
	)
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("ordinary capture error = %v, want %s", err, rootedpath.FailureRootReplaced)
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
			ManifestRoot:         captured,
			ManagedPathMutations: []journalrecord.ManagedPathMutation{mutation},
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

func TestLoadStateOnlyPlanRejectsReplacedManifestRootBeforeClassification(t *testing.T) {
	base := t.TempDir()
	manifestRoot := filepath.Join(base, "manifest")
	stateRoot := filepath.Join(base, "state")
	if err := os.MkdirAll(manifestRoot, 0o700); err != nil {
		t.Fatalf("create manifest root: %v", err)
	}
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatalf("create state root: %v", err)
	}
	paths := Paths{
		RecoveryDir:   filepath.Join(stateRoot, "recovery"),
		StatefilePath: filepath.Join(stateRoot, "state.json"),
		ManifestRoot:  manifestRoot,
		DataDir:       filepath.Join(stateRoot, "data"),
	}
	if _, err := CaptureJournalWithOptions(
		context.Background(),
		paths,
		"replace-state-only-root",
		time.Date(2026, time.August, 6, 0, 0, 0, 0, time.UTC),
		durable.EmptySnapshot(),
		inventoryJournalSnapshot(t, "after"),
		CaptureOptions{
			Filesystem: journalTestFilesystem(),
			Resolver:   func(output.Destination) (string, error) { return "", nil },
			StateCodec: testStateCodec(),
		},
	); err != nil {
		t.Fatalf("CaptureJournalWithOptions returned error: %v", err)
	}

	moved := filepath.Join(base, "original-manifest")
	if err := os.Rename(manifestRoot, moved); err != nil {
		t.Fatalf("move original manifest root: %v", err)
	}
	if err := os.Mkdir(manifestRoot, 0o700); err != nil {
		t.Fatalf("create replacement manifest root: %v", err)
	}

	_, err := LoadActivePlanWithOptions(context.Background(), paths, PlanLoadOptions{
		Filesystem:  journalTestFilesystem(),
		Resolver:    func(output.Destination) (string, error) { return "", nil },
		StateCodec:  testStateCodec(),
		StateReader: testStateReader(paths.StatefilePath),
	})
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("LoadActivePlan error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
}

func TestManifestRecoveryAuthorityAdmitsBudgetBeforeRootObservation(t *testing.T) {
	base := t.TempDir()
	manifestRoot := filepath.Join(base, "manifest")
	if err := os.Mkdir(manifestRoot, 0o700); err != nil {
		t.Fatalf("create manifest root: %v", err)
	}
	captured := mustJournalProjectRoot(t, manifestRoot)
	authority, err := captured.Authority()
	if err != nil {
		t.Fatalf("read captured manifest authority: %v", err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatalf("derive manifest provenance: %v", err)
	}
	if err := captured.Close(); err != nil {
		t.Fatalf("close captured manifest authority: %v", err)
	}
	budget := newRecoveryPathBudget(t)
	for budget.AdmitPathComponents(physicalwork.MaximumPhysicalPathDepth) == nil {
	}
	if err := os.Rename(manifestRoot, manifestRoot+"-moved"); err != nil {
		t.Fatalf("move manifest root: %v", err)
	}
	if err := os.Mkdir(manifestRoot, 0o700); err != nil {
		t.Fatalf("create replacement manifest root: %v", err)
	}

	_, err = journalnamespace.RecaptureManifestAuthority(
		manifestRoot,
		provenance,
		budget,
	)
	if err == nil || !strings.Contains(err.Error(), "path-component work exceeds operation limit") {
		t.Fatalf("manifest recovery authority error = %v, want pre-I/O budget rejection", err)
	}
}

func TestObserveGlobalRecoveryPathRejectsNilPresentCapability(t *testing.T) {
	observation, err := journalqualification.ObserveGlobalPath(
		context.Background(),
		"~/.codex/config.toml",
		"",
		nil,
		"/expected/config.toml",
		journalTestFilesystem(),
		func(output.Destination, rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, bool, error) {
			return nil, true, nil
		},
		journalTestCodecs(),
		recoveryBackupBudgetForTest(t),
	)
	if err != nil {
		t.Fatalf("observe nil global capability: %v", err)
	}
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

	observation, err := journalqualification.ObserveGlobalPath(
		context.Background(),
		"~/.codex/config.toml",
		"",
		nil,
		filepath.Join(t.TempDir(), "different.toml"),
		journalTestFilesystem(),
		func(output.Destination, rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, bool, error) {
			capability, err := root.Acquire(destination)
			return capability, true, err
		},
		journalTestCodecs(),
		recoveryBackupBudgetForTest(t),
	)
	if err != nil {
		t.Fatalf("observe mismatched global capability: %v", err)
	}
	if !strings.Contains(observation.Error, "does not match retained authority path") {
		t.Fatalf("observation error = %q, want resolver/capability mismatch refusal", observation.Error)
	}
}

func projectJournalCaptureFacts(
	t *testing.T,
	contentHash artifact.ContentHash,
) (journalrecord.ManagedPathMutation, observe.ManagedPathEvidence, durable.Snapshot) {
	t.Helper()
	previous := testAppliedState(target.TargetCodex, string(contentHash))
	mutation, err := journalrecord.NewManagedPathRecordMutation(
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
