package relation

import (
	"testing"

	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/topology"
)

func TestProspectiveInventoryRetiresActualOccurrenceNotRequesterKey(t *testing.T) {
	first := mustRelationObservationKey(t, mustRelationObservationSubject(t, topology.SubjectHostRelation, "first"))
	second := mustRelationObservationKey(t, mustRelationObservationSubject(t, topology.SubjectHostRelation, "second"))
	managed, err := hostrelation.NewManagedInstanceKey("new-request-key")
	if err != nil {
		t.Fatal(err)
	}
	second.expected, err = hostrelation.NewExpectedRelation(first.expected.SubjectKey(), managed)
	if err != nil {
		t.Fatal(err)
	}
	occurrence, err := NewOccurrence("host/global/inventory", "actual-row-0")
	if err != nil {
		t.Fatal(err)
	}
	var entries []Correlation
	for _, key := range []CorrelationKey{first, second} {
		row, err := NewRow(RowSpec{
			SubjectKey: string(key.expected.SubjectKey()), HasManagedInstanceKey: true,
			ManagedInstanceKey: string(key.expected.ManagedInstanceKey()), Occurrences: []Occurrence{occurrence},
		})
		if err != nil {
			t.Fatal(err)
		}
		inventory, err := NewInventory(InventorySpec{Availability: InventorySupported, Freshness: EvidenceFresh, Rows: []Row{row}})
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, Correlation{Key: key, Result: Correlate(key.expected, inventory)})
	}
	actual, err := NewBatch(BatchSpec{Correlations: entries})
	if err != nil {
		t.Fatal(err)
	}
	prospective, err := actual.WithoutRetiring([]CorrelationKey{first})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []CorrelationKey{first, second} {
		before, _ := actual.Correlation(key)
		after, _ := prospective.Correlation(key)
		if before.State() != StateExactCorrelation || after.State() != StateMissing {
			t.Fatalf("actual/prospective states = %s/%s", before.State(), after.State())
		}
	}
}

func TestProspectiveInventoryPreservesUncoveredAndDuplicateOccurrences(t *testing.T) {
	key := mustRelationObservationKey(t, mustRelationObservationSubject(t, topology.SubjectHostRelation, "selected"))
	first, err := NewOccurrence("document", "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewOccurrence("document", "second")
	if err != nil {
		t.Fatal(err)
	}
	retiring := mustRelationObservationKey(t, mustRelationObservationSubject(t, topology.SubjectHostRelation, "retiring"))
	oldRow, err := NewRow(RowSpec{
		SubjectKey: string(retiring.expected.SubjectKey()), HasManagedInstanceKey: true,
		ManagedInstanceKey: string(retiring.expected.ManagedInstanceKey()), Occurrences: []Occurrence{first},
	})
	if err != nil {
		t.Fatal(err)
	}
	oldInventory, err := NewInventory(InventorySpec{Availability: InventorySupported, Freshness: EvidenceFresh, Rows: []Row{oldRow}})
	if err != nil {
		t.Fatal(err)
	}
	for _, aggregate := range []bool{false, true} {
		var rows []Row
		for _, occurrence := range []Occurrence{first, second} {
			row, err := NewRow(RowSpec{
				SubjectKey: string(key.expected.SubjectKey()), HasManagedInstanceKey: true,
				ManagedInstanceKey: string(key.expected.ManagedInstanceKey()), Occurrences: []Occurrence{occurrence},
			})
			if err != nil {
				t.Fatal(err)
			}
			rows = append(rows, row)
		}
		if aggregate {
			rows[0], err = rows[0].WithOccurrences([]Occurrence{first, second})
			if err != nil {
				t.Fatal(err)
			}
			rows = rows[:1]
		}
		inventory, err := NewInventory(InventorySpec{Availability: InventorySupported, Freshness: EvidenceFresh, Rows: rows})
		if err != nil {
			t.Fatal(err)
		}
		batch, err := NewBatch(BatchSpec{Correlations: []Correlation{
			{Key: key, Result: Correlate(key.expected, inventory)},
			{Key: retiring, Result: Correlate(retiring.expected, oldInventory)},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if !aggregate {
			if _, err := batch.WithoutRetiring([]CorrelationKey{key}); err == nil {
				t.Fatal("accepted an ambiguous retirement")
			}
		}
		projected, err := batch.WithoutRetiring([]CorrelationKey{retiring})
		if err != nil {
			t.Fatal(err)
		}
		remaining, _ := projected.Correlation(key)
		if remaining.State() != StateExactCorrelation || len(remaining.CoveredOccurrences()) != 1 || remaining.CoveredOccurrences()[0] != second {
			t.Fatalf("aggregate=%t: remaining evidence = %+v", aggregate, remaining)
		}
		for _, rejected := range [][]CorrelationKey{{{}}, {retiring, retiring}} {
			if _, err := batch.WithoutRetiring(rejected); err == nil {
				t.Fatal("accepted invalid or repeated retirement")
			}
		}
	}
}

func TestProspectiveInventoryRefusesUnobservedOrUntraceableRetirements(t *testing.T) {
	key := mustRelationObservationKey(t, mustRelationObservationSubject(t, topology.SubjectHostRelation, "selected"))
	row, err := NewRow(RowSpec{
		SubjectKey: string(key.expected.SubjectKey()), HasManagedInstanceKey: true,
		ManagedInstanceKey: string(key.expected.ManagedInstanceKey()),
	})
	if err != nil {
		t.Fatal(err)
	}
	untraced, err := NewInventory(InventorySpec{Availability: InventorySupported, Freshness: EvidenceFresh, Rows: []Row{row}})
	if err != nil {
		t.Fatal(err)
	}
	for _, inventory := range []Inventory{UnsupportedInventory(), untraced} {
		batch, err := NewBatch(BatchSpec{Correlations: []Correlation{{Key: key, Result: Correlate(key.expected, inventory)}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := batch.WithoutRetiring([]CorrelationKey{key}); err == nil {
			t.Fatal("projected a retirement without resolved actual inventory origins")
		}
	}
	if _, err := (Batch{}).WithoutRetiring([]CorrelationKey{key}); err == nil {
		t.Fatal("projected a key from another batch")
	}
}
