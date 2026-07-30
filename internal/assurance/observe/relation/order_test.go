package relation_test

import (
	"errors"
	"fmt"
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

func TestFixedSlotPermutationRejectsDuplicateLoadIdentity(t *testing.T) {
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
	duplicate, err := observerelation.NewObservedRelationRow(alpha.HostLoadIdentity())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = observerelation.FixedSlotPermutation(
		constraint,
		[]observerelation.ObservedRelationRow{
			mustCorrelatedObservedRow(t, alpha),
			duplicate,
		},
	)
	if err == nil || !strings.Contains(err.Error(), "host load identity") {
		t.Fatalf("FixedSlotPermutation error = %v", err)
	}
}

func TestObservedRelationSequenceEnforcesRowLimit(t *testing.T) {
	for _, rowCount := range []int{
		0,
		1,
		observerelation.MaximumOrderRows - 1,
		observerelation.MaximumOrderRows,
		observerelation.MaximumOrderRows + 1,
	} {
		t.Run(fmt.Sprintf("rows_%d", rowCount), func(t *testing.T) {
			rows := orderLimitForeignRows(t, rowCount)
			_, err := newObservedSequence("opencode:project:server.plugins", rows)
			if rowCount <= observerelation.MaximumOrderRows {
				if err != nil {
					t.Fatalf("NewObservedRelationSequence: %v", err)
				}
				return
			}
			assertOrderLimitError(
				t,
				err,
				observerelation.OrderLimitObservedRows,
				rowCount,
				observerelation.MaximumOrderRows,
			)
		})
	}
}

func TestOrderConstraintEnforcesManagedMemberLimit(t *testing.T) {
	for _, memberCount := range []int{
		observerelation.MaximumOrderMembers - 1,
		observerelation.MaximumOrderMembers,
		observerelation.MaximumOrderMembers + 1,
	} {
		t.Run(fmt.Sprintf("members_%d", memberCount), func(t *testing.T) {
			constraint, _ := orderLimitFixture(t, memberCount, 0, false)
			err := observerelation.ValidateOrderConstraintBudget(constraint)
			if memberCount <= observerelation.MaximumOrderMembers {
				if err != nil {
					t.Fatalf("ValidateOrderConstraintBudget: %v", err)
				}
				return
			}
			assertOrderLimitError(
				t,
				err,
				observerelation.OrderLimitManagedMembers,
				memberCount,
				observerelation.MaximumOrderMembers,
			)
		})
	}
}

func TestFixedSlotPermutationEnforcesObservedManagedMemberLimit(t *testing.T) {
	constraint, rows := orderLimitFixture(
		t,
		observerelation.MaximumOrderMembers,
		1,
		false,
	)
	extraMember := orderLimitMember(t, observerelation.MaximumOrderMembers)
	foreignRows := append(
		[]observerelation.ObservedRelationRow(nil),
		rows[observerelation.MaximumOrderMembers:]...,
	)
	rows = append(
		rows[:observerelation.MaximumOrderMembers],
		mustOrderLimitCorrelatedRow(t, extraMember),
	)
	rows = append(rows, foreignRows...)

	_, _, err := observerelation.FixedSlotPermutation(constraint, rows)
	assertOrderLimitError(
		t,
		err,
		observerelation.OrderLimitManagedMembers,
		observerelation.MaximumOrderMembers+1,
		observerelation.MaximumOrderMembers,
	)
}

func TestFixedSlotPermutationEnforcesPrecedencePairLimit(t *testing.T) {
	tests := []struct {
		name         string
		managedCount int
		foreignCount int
		wantPairs    int
		wantError    bool
	}{
		{
			name:         "n_minus_1",
			managedCount: 127,
			foreignCount: 129,
			wantPairs:    observerelation.MaximumOrderPrecedencePairs - 1,
		},
		{
			name:         "n",
			managedCount: 128,
			foreignCount: 128,
			wantPairs:    observerelation.MaximumOrderPrecedencePairs,
		},
		{
			name:         "n_plus_1",
			managedCount: 5,
			foreignCount: 3_277,
			wantPairs:    observerelation.MaximumOrderPrecedencePairs + 1,
			wantError:    true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			constraint, rows := orderLimitFixture(
				t,
				test.managedCount,
				test.foreignCount,
				false,
			)
			_, _, err := observerelation.FixedSlotPermutation(constraint, rows)
			if !test.wantError {
				if err != nil {
					t.Fatalf("FixedSlotPermutation: %v", err)
				}
				return
			}
			assertOrderLimitError(
				t,
				err,
				observerelation.OrderLimitPrecedencePairs,
				test.wantPairs,
				observerelation.MaximumOrderPrecedencePairs,
			)
		})
	}
}

func TestFixedSlotPermutationAdmitsMaximumPrecedenceChanges(t *testing.T) {
	constraint, rows := orderLimitFixture(t, 128, 128, true)

	_, changes, err := observerelation.FixedSlotPermutation(constraint, rows)
	if err != nil {
		t.Fatalf("FixedSlotPermutation: %v", err)
	}
	if len(changes) != observerelation.MaximumOrderPrecedencePairs {
		t.Fatalf(
			"precedence changes = %d, want %d",
			len(changes),
			observerelation.MaximumOrderPrecedencePairs,
		)
	}
}

func BenchmarkFixedSlotPermutationAtPrecedenceLimit(b *testing.B) {
	constraint, rows := orderLimitFixture(b, 128, 128, true)
	b.ResetTimer()
	for range b.N {
		if _, _, err := observerelation.FixedSlotPermutation(constraint, rows); err != nil {
			b.Fatal(err)
		}
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

func orderLimitFixture(
	tb testing.TB,
	managedCount int,
	foreignCount int,
	reverse bool,
) (hostrelation.RelationOrderConstraint, []observerelation.ObservedRelationRow) {
	tb.Helper()
	members := make([]hostrelation.RelationOrderMember, managedCount)
	for index := range members {
		members[index] = orderLimitMember(tb, index)
	}
	desiredMembers := append([]hostrelation.RelationOrderMember(nil), members...)
	if reverse {
		for left, right := 0, len(desiredMembers)-1; left < right; left, right = left+1, right-1 {
			desiredMembers[left], desiredMembers[right] = desiredMembers[right], desiredMembers[left]
		}
	}
	constraint, err := hostrelation.NewRelationOrderConstraint(
		mustOrderClassIDWithoutTest(),
		"opencode-plugin-package-v1",
		hostrelation.ConfigOrderOnly,
		desiredMembers,
	)
	if err != nil {
		tb.Fatalf("NewRelationOrderConstraint: %v", err)
	}

	rows := make([]observerelation.ObservedRelationRow, 0, managedCount+foreignCount)
	split := managedCount
	if reverse {
		split = managedCount / 2
	}
	for _, member := range members[:split] {
		rows = append(rows, mustOrderLimitCorrelatedRow(tb, member))
	}
	rows = append(rows, orderLimitForeignRows(tb, foreignCount)...)
	for _, member := range members[split:] {
		rows = append(rows, mustOrderLimitCorrelatedRow(tb, member))
	}
	return constraint, rows
}

func orderLimitMember(tb testing.TB, index int) hostrelation.RelationOrderMember {
	tb.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"opencode.plugin-carrier",
		fmt.Sprintf("member-%04d", index),
	)
	if err != nil {
		tb.Fatalf("topology.NewSubjectID: %v", err)
	}
	identity, err := hostrelation.NewHostLoadIdentity(fmt.Sprintf("@managed/pkg-%04d", index))
	if err != nil {
		tb.Fatalf("NewHostLoadIdentity: %v", err)
	}
	member, err := hostrelation.NewRelationOrderMember(subject, identity)
	if err != nil {
		tb.Fatalf("NewRelationOrderMember: %v", err)
	}
	return member
}

func orderLimitForeignRows(
	tb testing.TB,
	count int,
) []observerelation.ObservedRelationRow {
	tb.Helper()
	rows := make([]observerelation.ObservedRelationRow, count)
	for index := range rows {
		identity, err := hostrelation.NewHostLoadIdentity(
			fmt.Sprintf("@foreign/pkg-%04d", index),
		)
		if err != nil {
			tb.Fatalf("NewHostLoadIdentity: %v", err)
		}
		row, err := observerelation.NewObservedRelationRow(identity)
		if err != nil {
			tb.Fatalf("NewObservedRelationRow: %v", err)
		}
		rows[index] = row
	}
	return rows
}

func mustOrderLimitCorrelatedRow(
	tb testing.TB,
	member hostrelation.RelationOrderMember,
) observerelation.ObservedRelationRow {
	tb.Helper()
	row, err := observerelation.NewCorrelatedObservedRelationRow(
		member.HostLoadIdentity(),
		member.Subject(),
	)
	if err != nil {
		tb.Fatalf("NewCorrelatedObservedRelationRow: %v", err)
	}
	return row
}

func assertOrderLimitError(
	t *testing.T,
	err error,
	wantKind observerelation.OrderLimitKind,
	wantObserved int,
	wantLimit int,
) {
	t.Helper()
	if !errors.Is(err, observerelation.ErrOrderLimitExceeded) {
		t.Fatalf("error = %v, want ErrOrderLimitExceeded", err)
	}
	var limitError *observerelation.OrderLimitError
	if !errors.As(err, &limitError) {
		t.Fatalf("error = %T %v, want *OrderLimitError", err, err)
	}
	if limitError.Kind() != wantKind ||
		limitError.Observed() != wantObserved ||
		limitError.Limit() != wantLimit {
		t.Fatalf(
			"limit error = %s observed=%d limit=%d, want %s observed=%d limit=%d",
			limitError.Kind(),
			limitError.Observed(),
			limitError.Limit(),
			wantKind,
			wantObserved,
			wantLimit,
		)
	}
}
