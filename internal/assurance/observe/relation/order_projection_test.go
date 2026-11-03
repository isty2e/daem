package relation_test

import (
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
)

func TestSameOrderingExcludesOnlyTheWholeSurfaceRevision(t *testing.T) {
	a := mustCorrelatedObservedRow(t, mustObservedOrderMember(t, "a", "npm:a"))
	b := mustCorrelatedObservedRow(t, mustObservedOrderMember(t, "b", "npm:b"))
	rows := []observerelation.ObservedRelationRow{a, b}
	baseline := mustObservedSequence(t, "project.plugins", rows)
	uncorrelated, err := observerelation.NewObservedRelationRow(a.HostLoadIdentity())
	if err != nil {
		t.Fatal(err)
	}
	otherCorrelation := mustCorrelatedObservedRow(t, mustObservedOrderMember(t, "other", "npm:a"))
	class, err := hostrelation.NewOrderClassID("extension:pi:project:packages")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name      string
		class     hostrelation.OrderClassID
		sequence  hostrelation.PhysicalSequenceID
		authority observerelation.SequenceAuthority
		revision  observerelation.SequenceRevision
		rows      []observerelation.ObservedRelationRow
		equal     bool
	}{
		{"same facts", baseline.ClassID(), baseline.SequenceID(), baseline.Authority(), baseline.Revision(), rows, true},
		{"different revision", baseline.ClassID(), baseline.SequenceID(), baseline.Authority(), "sha256:new", rows, true},
		{"different class", class, baseline.SequenceID(), baseline.Authority(), baseline.Revision(), rows, false},
		{"different sequence", baseline.ClassID(), mustObservedSequenceID(t, "global.plugins"), baseline.Authority(), baseline.Revision(), rows, false},
		{"different authority", baseline.ClassID(), baseline.SequenceID(), "document:other", baseline.Revision(), rows, false},
		{"different order", baseline.ClassID(), baseline.SequenceID(), baseline.Authority(), baseline.Revision(), []observerelation.ObservedRelationRow{b, a}, false},
		{"missing row", baseline.ClassID(), baseline.SequenceID(), baseline.Authority(), baseline.Revision(), []observerelation.ObservedRelationRow{a}, false},
		{"missing correlation", baseline.ClassID(), baseline.SequenceID(), baseline.Authority(), baseline.Revision(), []observerelation.ObservedRelationRow{uncorrelated, b}, false},
		{"different correlation", baseline.ClassID(), baseline.SequenceID(), baseline.Authority(), baseline.Revision(), []observerelation.ObservedRelationRow{otherCorrelation, b}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			sequence, err := observerelation.NewObservedRelationSequence(test.class, test.sequence, test.authority, test.revision, test.rows)
			if err != nil {
				t.Fatal(err)
			}
			if baseline.SameOrdering(sequence) != test.equal || sequence.SameOrdering(baseline) != test.equal {
				t.Fatal("ordering equality collapsed authority, identity, row or correlation facts")
			}
		})
	}
}
