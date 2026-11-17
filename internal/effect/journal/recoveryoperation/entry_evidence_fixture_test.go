package recoveryoperation

import (
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func backupEntryForTest(t *testing.T, before recovery.BeforePathState) journalrecord.PathEntry {
	t.Helper()
	hash := testContentHash("backup capture basis")
	destination := outputtest.Parse(t, "managed-file")
	mutation := normalizedPathCapture(t, journalrecord.CaptureRecord, target.ScopeProject, destination, hash, realization.PathProjectionFile)
	state, err := durable.NewManagedPathState(testManagedPathSubject(t, "capture"), []target.Target{target.TargetCodex}, target.ScopeProject, destination, hash, realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := rootedpath.NewAuthorityProvenance(t.TempDir(), testContentHashString("backup manifest"), testContentHashString("backup mount"))
	if err != nil {
		t.Fatal(err)
	}
	demands, err := recovery.NewRemovalDemandSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := journalrecord.NewOrdinaryDraft("backup-view", time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), statefileFor(state), statefileFor(state), provenance, []journalrecord.CaptureMutation{mutation}, demands)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.BeginEntry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyBefore(before); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyGlobalBinding(nil); err != nil {
		t.Fatal(err)
	}
	if err := draft.CloseEntries(); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyRemovalIntents(nil); err != nil {
		t.Fatal(err)
	}
	if err := draft.BeginClaimCoverage(nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, present, err := draft.NextClaimAuthority(t.Context()); err != nil || present {
		t.Fatal("unexpected project ownership demand", err)
	}
	evidence, err := draft.Finish()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := evidence.RecordedEntries()
	if err != nil || len(entries) != 1 {
		t.Fatal("completed backup capture lost its entry", err)
	}
	return entries[0]
}
