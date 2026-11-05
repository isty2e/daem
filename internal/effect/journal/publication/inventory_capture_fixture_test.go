package publication

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"

	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func inventoryJournalSnapshot(t *testing.T, seed string) durable.Snapshot {
	t.Helper()
	id, err := entity.New(entity.KindInstructions, "project")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(id, "instructions.project.agents")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	row, err := durable.NewManagedPathState(subject, []target.Target{target.TargetCodex}, target.ScopeProject, destination, testContentHash(seed), realization.PathProjectionFile, realization.PathPermissionsExecutableClass, 0)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{row}})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func captureInventoryJournal(
	t *testing.T,
	recoveryRoot string,
	operationID string,
) (retirement.Identity, CaptureResult) {
	t.Helper()

	result, err := CaptureJournalWithOptions(
		t.Context(),
		Paths{RecoveryDir: recoveryRoot, ManifestRoot: filepath.Dir(recoveryRoot)},
		operationID,
		time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC),
		inventoryJournalSnapshot(t, "before"),
		inventoryJournalSnapshot(t, "after"),
		CaptureOptions{
			Filesystem: journalTestFilesystem(),
			Resolver: func(output.Destination) (string, error) {
				return "", nil
			},
			StateCodec: testStateCodec(),
		},
	)
	if err != nil {
		t.Fatalf("CaptureJournalWithOptions: %v", err)
	}
	snapshot, err := journalTestFilesystem().ReadRegularFileSnapshotUpTo(t.Context(), result.JournalPath, journalrecord.MaximumBytes)
	if err != nil {
		t.Fatalf("read captured inventory journal: %v", err)
	}
	loaded, err := journalrecord.DecodeOrdinarySnapshot(t.Context(), snapshot, result.JournalPath, testStateCodec())
	if err != nil {
		t.Fatalf("decode captured inventory journal: %v", err)
	}
	fingerprint, err := loaded.AuthorityFingerprint(t.Context(), testStateCodec())
	if err != nil {
		t.Fatalf("captured inventory authority fingerprint: %v", err)
	}
	identity, err := retirement.NewIdentity(operationID, fingerprint)
	if err != nil {
		t.Fatalf("retirement.NewIdentity: %v", err)
	}
	if identity.JournalAuthorityFingerprint() != journalrecord.Fingerprint(snapshot.Content()) {
		t.Fatal("inventory identity did not fingerprint its actual published journal bytes")
	}
	return identity, result
}
