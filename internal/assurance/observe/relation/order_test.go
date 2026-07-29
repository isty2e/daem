package relation_test

import (
	"strings"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/topology"
)

func TestOneConstraintCanProjectToTwoIndependentPhysicalSequences(t *testing.T) {
	alpha := mustObservedOrderMember(t, "alpha", "@acme/alpha")
	beta := mustObservedOrderMember(t, "beta", "@acme/beta")
	constraint, err := hostrelation.NewRelationOrderConstraint(
		mustObservedOrderClassID(t),
		"opencode-plugin-package-v1",
		hostrelation.ConfigOrderOnly,
		[]hostrelation.RelationOrderMember{alpha, beta},
	)
	if err != nil {
		t.Fatalf("NewRelationOrderConstraint: %v", err)
	}

	server := mustObservedSequence(t, "opencode:project:server.plugins", []observerelation.ObservedRelationRow{
		mustCorrelatedObservedRow(t, alpha),
		mustCorrelatedObservedRow(t, beta),
	})
	tui := mustObservedSequence(t, "opencode:project:tui.plugins", []observerelation.ObservedRelationRow{
		mustCorrelatedObservedRow(t, beta),
		mustCorrelatedObservedRow(t, alpha),
	})

	if server.ClassID() != constraint.ClassID() || tui.ClassID() != constraint.ClassID() {
		t.Fatal("physical sequences do not retain the shared logical order class")
	}
	if server.SequenceID() == tui.SequenceID() {
		t.Fatal("independent physical sequences collapsed to one identity")
	}
	if got := server.OrderedRows(); got[0].HostLoadIdentity() != alpha.HostLoadIdentity() {
		t.Fatalf("server order = %#v", got)
	}
	if got := tui.OrderedRows(); got[0].HostLoadIdentity() != beta.HostLoadIdentity() {
		t.Fatalf("tui order = %#v", got)
	}
}

func TestObservedSequenceAllowsSparseMembershipAndMissingCorrelation(t *testing.T) {
	alpha := mustObservedOrderMember(t, "alpha", "@acme/alpha")
	foreignIdentity := mustObservedLoadIdentity(t, "@foreign/tool")
	foreign, err := observerelation.NewObservedRelationRow(foreignIdentity)
	if err != nil {
		t.Fatalf("NewObservedRelationRow: %v", err)
	}

	sequence := mustObservedSequence(t, "opencode:project:server.plugins", []observerelation.ObservedRelationRow{
		mustCorrelatedObservedRow(t, alpha),
		foreign,
	})
	rows := sequence.OrderedRows()
	if len(rows) != 2 {
		t.Fatalf("row count = %d, want 2", len(rows))
	}
	if subject, correlated := rows[1].CorrelatedSubject(); correlated || !subject.IsZero() {
		t.Fatalf("foreign row correlation = %s, %t", subject, correlated)
	}
}

func TestObservedSequenceRejectsDuplicateLoadIdentityOrCorrelation(t *testing.T) {
	alpha := mustObservedOrderMember(t, "alpha", "@acme/alpha")
	sameIdentity, err := observerelation.NewCorrelatedObservedRelationRow(
		alpha.HostLoadIdentity(),
		mustObservedSubject(t, "other"),
	)
	if err != nil {
		t.Fatal(err)
	}
	sameSubject, err := observerelation.NewCorrelatedObservedRelationRow(
		mustObservedLoadIdentity(t, "@acme/other"),
		alpha.Subject(),
	)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		rows []observerelation.ObservedRelationRow
		want string
	}{
		{
			name: "load identity",
			rows: []observerelation.ObservedRelationRow{
				mustCorrelatedObservedRow(t, alpha),
				sameIdentity,
			},
			want: "host load identity",
		},
		{
			name: "subject",
			rows: []observerelation.ObservedRelationRow{
				mustCorrelatedObservedRow(t, alpha),
				sameSubject,
			},
			want: "relation subject",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newObservedSequence("opencode:project:server.plugins", test.rows)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestObservedSequenceRequiresAuthorityRevisionAndDefensiveCopies(t *testing.T) {
	alpha := mustObservedOrderMember(t, "alpha", "@acme/alpha")
	input := []observerelation.ObservedRelationRow{mustCorrelatedObservedRow(t, alpha)}
	sequence := mustObservedSequence(t, "opencode:project:server.plugins", input)

	input[0] = observerelation.ObservedRelationRow{}
	returned := sequence.OrderedRows()
	returned[0] = observerelation.ObservedRelationRow{}
	if len(sequence.OrderedRows()) != 1 {
		t.Fatal("caller mutation changed the observed sequence")
	}

	sequenceID := mustObservedSequenceID(t, "opencode:project:server.plugins")
	authority := mustObservedAuthority(t)
	revision := mustObservedRevision(t)
	if _, err := observerelation.NewObservedRelationSequence(
		mustObservedOrderClassID(t),
		sequenceID,
		"",
		revision,
		nil,
	); err == nil {
		t.Fatal("sequence accepted missing authority")
	}
	if _, err := observerelation.NewObservedRelationSequence(
		mustObservedOrderClassID(t),
		sequenceID,
		authority,
		"",
		nil,
	); err == nil {
		t.Fatal("sequence accepted missing revision")
	}
}

func TestFixedSlotPermutationPreservesForeignSlotsAndReportsCrossings(t *testing.T) {
	alpha := mustObservedOrderMember(t, "alpha", "@acme/alpha")
	beta := mustObservedOrderMember(t, "beta", "@acme/beta")
	constraint, err := hostrelation.NewRelationOrderConstraint(
		mustObservedOrderClassID(t),
		"opencode-plugin-package-v1",
		hostrelation.ConfigOrderOnly,
		[]hostrelation.RelationOrderMember{alpha, beta},
	)
	if err != nil {
		t.Fatalf("NewRelationOrderConstraint: %v", err)
	}
	foreign, err := observerelation.NewObservedRelationRow(
		mustObservedLoadIdentity(t, "@foreign/tool"),
	)
	if err != nil {
		t.Fatalf("NewObservedRelationRow: %v", err)
	}
	rows := []observerelation.ObservedRelationRow{
		mustCorrelatedObservedRow(t, beta),
		foreign,
		mustCorrelatedObservedRow(t, alpha),
	}

	order, changes, err := observerelation.FixedSlotPermutation(constraint, rows)
	if err != nil {
		t.Fatalf("FixedSlotPermutation: %v", err)
	}
	wantOrder := []int{2, 1, 0}
	for index, want := range wantOrder {
		if order[index] != want {
			t.Fatalf("order = %v, want %v", order, wantOrder)
		}
	}
	if len(changes) != 2 {
		t.Fatalf("precedence changes = %d, want 2", len(changes))
	}
	if changes[0].ManagedSubject() != beta.Subject() ||
		!changes[0].ManagedWasBefore() ||
		changes[0].ManagedWillBeBefore() {
		t.Fatalf("first precedence change = %#v", changes[0])
	}
	if changes[1].ManagedSubject() != alpha.Subject() ||
		changes[1].ManagedWasBefore() ||
		!changes[1].ManagedWillBeBefore() {
		t.Fatalf("second precedence change = %#v", changes[1])
	}
}

func mustObservedSequence(
	t *testing.T,
	sequenceID string,
	rows []observerelation.ObservedRelationRow,
) observerelation.ObservedRelationSequence {
	t.Helper()
	sequence, err := newObservedSequence(sequenceID, rows)
	if err != nil {
		t.Fatalf("NewObservedRelationSequence: %v", err)
	}
	return sequence
}

func newObservedSequence(
	sequenceID string,
	rows []observerelation.ObservedRelationRow,
) (observerelation.ObservedRelationSequence, error) {
	return observerelation.NewObservedRelationSequence(
		mustOrderClassIDWithoutTest(),
		mustSequenceIDWithoutTest(sequenceID),
		observerelation.SequenceAuthority("document:project-config"),
		observerelation.SequenceRevision("sha256:revision"),
		rows,
	)
}

func mustObservedOrderMember(
	t *testing.T,
	subjectKey string,
	loadIdentity string,
) hostrelation.RelationOrderMember {
	t.Helper()
	member, err := hostrelation.NewRelationOrderMember(
		mustObservedSubject(t, subjectKey),
		mustObservedLoadIdentity(t, loadIdentity),
	)
	if err != nil {
		t.Fatalf("NewRelationOrderMember: %v", err)
	}
	return member
}

func mustCorrelatedObservedRow(
	t *testing.T,
	member hostrelation.RelationOrderMember,
) observerelation.ObservedRelationRow {
	t.Helper()
	row, err := observerelation.NewCorrelatedObservedRelationRow(
		member.HostLoadIdentity(),
		member.Subject(),
	)
	if err != nil {
		t.Fatalf("NewCorrelatedObservedRelationRow: %v", err)
	}
	return row
}

func mustObservedSubject(t *testing.T, key string) topology.SubjectID {
	t.Helper()
	subject, err := topology.NewSubjectID(topology.SubjectHostRelation, "opencode.plugin-carrier", key)
	if err != nil {
		t.Fatalf("topology.NewSubjectID: %v", err)
	}
	return subject
}

func mustObservedLoadIdentity(t *testing.T, value string) hostrelation.HostLoadIdentity {
	t.Helper()
	identity, err := hostrelation.NewHostLoadIdentity(value)
	if err != nil {
		t.Fatalf("NewHostLoadIdentity: %v", err)
	}
	return identity
}

func mustObservedOrderClassID(t *testing.T) hostrelation.OrderClassID {
	t.Helper()
	return mustOrderClassIDWithoutTest()
}

func mustOrderClassIDWithoutTest() hostrelation.OrderClassID {
	id, err := hostrelation.NewOrderClassID("extension:opencode:project:plugins")
	if err != nil {
		panic(err)
	}
	return id
}

func mustObservedSequenceID(t *testing.T, value string) hostrelation.PhysicalSequenceID {
	t.Helper()
	return mustSequenceIDWithoutTest(value)
}

func mustSequenceIDWithoutTest(value string) hostrelation.PhysicalSequenceID {
	id, err := hostrelation.NewPhysicalSequenceID(value)
	if err != nil {
		panic(err)
	}
	return id
}

func mustObservedAuthority(t *testing.T) observerelation.SequenceAuthority {
	t.Helper()
	authority, err := observerelation.NewSequenceAuthority("document:project-config")
	if err != nil {
		t.Fatalf("NewSequenceAuthority: %v", err)
	}
	return authority
}

func mustObservedRevision(t *testing.T) observerelation.SequenceRevision {
	t.Helper()
	revision, err := observerelation.NewSequenceRevision("sha256:revision")
	if err != nil {
		t.Fatalf("NewSequenceRevision: %v", err)
	}
	return revision
}
