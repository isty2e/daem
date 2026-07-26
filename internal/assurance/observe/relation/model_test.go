package relation_test

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/realization/relation"
)

func TestCorrelateExactRequiresSubjectAndExpectedKey(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	row := mustManagedRow(t, "context7", "managed/context7")
	inventory := mustInventory(t, relation.InventorySpec{
		Availability: relation.InventorySupported,
		Freshness:    relation.EvidenceFresh,
		Rows:         []relation.Row{row},
	})

	result := relation.Correlate(subject, inventory)
	assertState(t, result, relation.StateExactCorrelation, relation.ReasonNone)
	if len(result.SameSubjectRows()) != 1 || len(result.ManagedKeyRows()) != 1 {
		t.Fatalf(
			"correlated rows = same-subject %d managed-key %d, want 1/1",
			len(result.SameSubjectRows()),
			len(result.ManagedKeyRows()),
		)
	}
}

func TestCorrelateUnkeyedSameSubjectGrantsNoAdoptionGuidance(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	inventory := mustInventory(t, relation.InventorySpec{
		Availability: relation.InventorySupported,
		Freshness:    relation.EvidenceFresh,
		Rows: []relation.Row{
			mustUnmanagedRow(t, "context7"),
		},
	})

	result := relation.Correlate(subject, inventory)
	assertState(t, result, relation.StateUnkeyedSameSubject, relation.ReasonUnkeyedSameSubject)
	assertWatchpoints(t, result, nil)
}

func TestCorrelateDetectsSameSubjectShadowAndSourceCollision(t *testing.T) {
	subject := mustSubject(t, "context7", "marketplace/context7")
	inventory := mustInventory(t, relation.InventorySpec{
		Availability: relation.InventorySupported,
		Freshness:    relation.EvidenceFresh,
		Rows: []relation.Row{
			mustManagedRow(t, "context7", "host-source/context7"),
		},
	})

	result := relation.Correlate(subject, inventory)
	assertState(t, result, relation.StateSameSubjectShadow, relation.ReasonSameSubjectShadow)
	assertWatchpoints(t, result, []relation.Watchpoint{relation.WatchpointReplacementAuthorityRequired})
}

func TestCorrelateDetectsExactManagedRowShadowedBySameSubjectRow(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	inventory := mustInventory(t, relation.InventorySpec{
		Availability: relation.InventorySupported,
		Freshness:    relation.EvidenceFresh,
		Rows: []relation.Row{
			mustManagedRow(t, "context7", "managed/context7"),
			mustUnmanagedRow(t, "context7"),
		},
	})

	result := relation.Correlate(subject, inventory)
	assertState(t, result, relation.StateSameSubjectShadow, relation.ReasonSameSubjectShadow)
	assertRowCounts(t, result, 2, 1)
}

func TestCorrelateDetectsDuplicateExactRowsAsAmbiguous(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	inventory := mustInventory(t, relation.InventorySpec{
		Availability: relation.InventorySupported,
		Freshness:    relation.EvidenceFresh,
		Rows: []relation.Row{
			mustManagedRow(t, "context7", "managed/context7"),
			mustManagedRow(t, "context7", "managed/context7"),
		},
	})

	result := relation.Correlate(subject, inventory)
	assertState(t, result, relation.StateAmbiguous, relation.ReasonAmbiguous)
	assertRowCounts(t, result, 2, 2)
	assertWatchpoints(t, result, []relation.Watchpoint{relation.WatchpointReplacementAuthorityRequired})
}

func TestCorrelateDetectsManagedKeyDrift(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	inventory := mustInventory(t, relation.InventorySpec{
		Availability: relation.InventorySupported,
		Freshness:    relation.EvidenceFresh,
		Rows: []relation.Row{
			mustManagedRow(t, "renamed-context7", "managed/context7"),
		},
	})

	result := relation.Correlate(subject, inventory)
	assertState(t, result, relation.StateManagedKeyDrift, relation.ReasonManagedKeyDrift)
	assertWatchpoints(t, result, []relation.Watchpoint{relation.WatchpointReplacementAuthorityRequired})
}

func TestCorrelateDetectsManagedKeyDriftPlusUnmanagedSameSubjectAsAmbiguous(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	inventory := mustInventory(t, relation.InventorySpec{
		Availability: relation.InventorySupported,
		Freshness:    relation.EvidenceFresh,
		Rows: []relation.Row{
			mustManagedRow(t, "renamed-context7", "managed/context7"),
			mustUnmanagedRow(t, "context7"),
		},
	})

	result := relation.Correlate(subject, inventory)
	assertState(t, result, relation.StateAmbiguous, relation.ReasonAmbiguous)
	assertRowCounts(t, result, 1, 1)
	assertWatchpoints(t, result, []relation.Watchpoint{relation.WatchpointReplacementAuthorityRequired})
}

func TestCorrelateDetectsAmbiguousManagedKeyRows(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	inventory := mustInventory(t, relation.InventorySpec{
		Availability: relation.InventorySupported,
		Freshness:    relation.EvidenceFresh,
		Rows: []relation.Row{
			mustManagedRow(t, "context7", "managed/context7"),
			mustManagedRow(t, "context7-copy", "managed/context7"),
		},
	})

	result := relation.Correlate(subject, inventory)
	assertState(t, result, relation.StateAmbiguous, relation.ReasonAmbiguous)
	assertRowCounts(t, result, 1, 2)
	assertWatchpoints(t, result, []relation.Watchpoint{relation.WatchpointReplacementAuthorityRequired})
}

func TestCorrelateBlocksStaleAndUnsupportedEvidenceBeforeRows(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	stale := mustInventory(t, relation.InventorySpec{
		Availability: relation.InventorySupported,
		Freshness:    relation.EvidenceStale,
		Rows: []relation.Row{
			mustManagedRow(t, "context7", "managed/context7"),
		},
	})
	unsupported := mustInventory(t, relation.InventorySpec{
		Availability: relation.InventoryUnsupported,
		Freshness:    relation.EvidenceFresh,
	})
	unavailable := mustInventory(t, relation.InventorySpec{
		Availability: relation.InventoryUnavailable,
		Freshness:    relation.EvidenceFresh,
	})
	unsupportedAndStale := mustInventory(t, relation.InventorySpec{
		Availability: relation.InventoryUnsupported,
		Freshness:    relation.EvidenceStale,
	})
	unavailableAndStale := mustInventory(t, relation.InventorySpec{
		Availability: relation.InventoryUnavailable,
		Freshness:    relation.EvidenceStale,
	})

	staleResult := relation.Correlate(subject, stale)
	assertState(t, staleResult, relation.StateStaleEvidence, relation.ReasonStaleEvidence)
	assertRowCounts(t, staleResult, 0, 0)
	assertWatchpoints(t, staleResult, []relation.Watchpoint{relation.WatchpointFreshInventoryRequired})

	unsupportedResult := relation.Correlate(subject, unsupported)
	assertState(t, unsupportedResult, relation.StateUnsupported, relation.ReasonUnsupportedInventory)
	assertWatchpoints(t, unsupportedResult, []relation.Watchpoint{relation.WatchpointPassiveInventoryRequired})

	unavailableResult := relation.Correlate(subject, unavailable)
	assertState(t, unavailableResult, relation.StateUnavailableEvidence, relation.ReasonUnavailableEvidence)
	assertWatchpoints(t, unavailableResult, []relation.Watchpoint{relation.WatchpointRelationEvidenceRequired})

	unsupportedStaleResult := relation.Correlate(subject, unsupportedAndStale)
	assertState(t, unsupportedStaleResult, relation.StateUnsupported, relation.ReasonUnsupportedInventory)
	assertWatchpoints(t, unsupportedStaleResult, []relation.Watchpoint{relation.WatchpointPassiveInventoryRequired})

	unavailableStaleResult := relation.Correlate(subject, unavailableAndStale)
	assertState(t, unavailableStaleResult, relation.StateStaleEvidence, relation.ReasonStaleEvidence)
	assertWatchpoints(t, unavailableStaleResult, []relation.Watchpoint{relation.WatchpointFreshInventoryRequired})
}

func TestCorrelationResultSlicesAreDefensiveCopies(t *testing.T) {
	subject := mustSubject(t, "context7", "managed/context7")
	inventory := mustInventory(t, relation.InventorySpec{
		Availability: relation.InventorySupported,
		Freshness:    relation.EvidenceFresh,
		Rows: []relation.Row{
			mustManagedRow(t, "context7", "managed/context7"),
		},
	})

	result := relation.Correlate(subject, inventory)
	rows := result.Rows()
	rows[0] = mustUnmanagedRow(t, "changed")
	sameSubjectRows := result.SameSubjectRows()
	sameSubjectRows[0] = mustUnmanagedRow(t, "changed-again")
	watchpoints := result.Watchpoints()
	watchpoints = append(watchpoints, relation.WatchpointReplacementAuthorityRequired)

	assertState(t, result, relation.StateExactCorrelation, relation.ReasonNone)
	if got := result.Rows()[0].SubjectKey(); got != hostrelation.SubjectKey("context7") {
		t.Fatalf("Rows was not defensively copied, subject = %q", got)
	}
	if got := result.SameSubjectRows()[0].SubjectKey(); got != hostrelation.SubjectKey("context7") {
		t.Fatalf("SameSubjectRows was not defensively copied, subject = %q", got)
	}
	if len(result.Watchpoints()) != 0 {
		t.Fatalf("Watchpoints was not defensively copied: %v", result.Watchpoints())
	}
}

func TestInventoryRowsAreDefensiveCopies(t *testing.T) {
	rows := []relation.Row{mustManagedRow(t, "context7", "managed/context7")}
	inventory := mustInventory(t, relation.InventorySpec{
		Availability: relation.InventorySupported,
		Freshness:    relation.EvidenceFresh,
		Rows:         rows,
	})
	rows[0] = mustUnmanagedRow(t, "changed")
	returnedRows := inventory.Rows()
	returnedRows[0] = mustUnmanagedRow(t, "also-changed")

	if got := inventory.Rows()[0].SubjectKey(); got != hostrelation.SubjectKey("context7") {
		t.Fatalf("Inventory rows were not defensively copied, subject = %q", got)
	}
}

func TestInventoryAndRowsRejectInvalidFactShapes(t *testing.T) {
	if _, err := relation.NewRow(relation.RowSpec{
		SubjectKey:         "context7",
		ManagedInstanceKey: "managed",
	}); err == nil {
		t.Fatalf("NewRow accepted managed key without presence flag")
	}
	if _, err := relation.NewRow(relation.RowSpec{
		SubjectKey: "context\n7",
	}); err == nil {
		t.Fatalf("NewRow accepted control character in subject key")
	}
	if _, err := relation.NewInventory(relation.InventorySpec{
		Availability: relation.InventoryAvailability("maybe"),
		Freshness:    relation.EvidenceFresh,
	}); err == nil {
		t.Fatalf("NewInventory accepted unsupported availability")
	}
	if _, err := relation.NewInventory(relation.InventorySpec{
		Availability: relation.InventoryUnsupported,
		Freshness:    relation.EvidenceFresh,
		Rows: []relation.Row{
			mustUnmanagedRow(t, "context7"),
		},
	}); err == nil {
		t.Fatalf("NewInventory accepted rows for unsupported inventory")
	}
	if _, err := relation.NewInventory(relation.InventorySpec{
		Availability: relation.InventoryUnavailable,
		Freshness:    relation.EvidenceFresh,
		Rows: []relation.Row{
			mustUnmanagedRow(t, "context7"),
		},
	}); err == nil {
		t.Fatalf("NewInventory accepted rows for unavailable inventory")
	}
}

func mustSubject(t *testing.T, subjectKey string, managedKey string) hostrelation.ExpectedRelation {
	t.Helper()
	relationSubjectKey, err := hostrelation.NewSubjectKey(subjectKey)
	if err != nil {
		t.Fatalf("NewSubjectKey: %v", err)
	}
	managedInstanceKey, err := hostrelation.NewManagedInstanceKey(managedKey)
	if err != nil {
		t.Fatalf("NewManagedInstanceKey: %v", err)
	}
	subject, err := hostrelation.NewExpectedRelation(relationSubjectKey, managedInstanceKey)
	if err != nil {
		t.Fatalf("NewExpectedRelation: %v", err)
	}
	return subject
}

func mustManagedRow(t *testing.T, subjectKey string, managedKey string) relation.Row {
	t.Helper()
	return mustRow(t, relation.RowSpec{
		SubjectKey:            subjectKey,
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    managedKey,
	})
}

func mustUnmanagedRow(t *testing.T, subjectKey string) relation.Row {
	t.Helper()
	return mustRow(t, relation.RowSpec{SubjectKey: subjectKey})
}

func mustRow(t *testing.T, spec relation.RowSpec) relation.Row {
	t.Helper()
	row, err := relation.NewRow(spec)
	if err != nil {
		t.Fatalf("NewRow: %v", err)
	}
	return row
}

func mustInventory(t *testing.T, spec relation.InventorySpec) relation.Inventory {
	t.Helper()
	inventory, err := relation.NewInventory(spec)
	if err != nil {
		t.Fatalf("NewInventory: %v", err)
	}
	return inventory
}

func assertState(
	t *testing.T,
	result relation.CorrelationResult,
	state relation.CorrelationState,
	reason relation.ReasonCode,
) {
	t.Helper()
	if result.State() != state || result.Reason() != reason {
		t.Fatalf("state = (%q, %q), want (%q, %q)", result.State(), result.Reason(), state, reason)
	}
}

func assertWatchpoints(
	t *testing.T,
	result relation.CorrelationResult,
	want []relation.Watchpoint,
) {
	t.Helper()
	got := result.Watchpoints()
	if len(got) != len(want) {
		t.Fatalf("watchpoints length = %d, want %d: %v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("watchpoints[%d] = %q, want %q: %v", index, got[index], want[index], got)
		}
	}
}

func assertRowCounts(
	t *testing.T,
	result relation.CorrelationResult,
	wantSameSubject int,
	wantManagedKey int,
) {
	t.Helper()
	if len(result.SameSubjectRows()) != wantSameSubject ||
		len(result.ManagedKeyRows()) != wantManagedKey {
		t.Fatalf(
			"correlated rows = same-subject %d managed-key %d, want %d/%d",
			len(result.SameSubjectRows()),
			len(result.ManagedKeyRows()),
			wantSameSubject,
			wantManagedKey,
		)
	}
}
