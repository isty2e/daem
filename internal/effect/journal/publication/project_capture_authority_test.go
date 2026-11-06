package publication

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

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
