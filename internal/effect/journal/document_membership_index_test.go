package journal

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestDocumentMembershipLookupDoesNotRescanUnrelatedState(t *testing.T) {
	const count = 512
	var rows []durable.ManagedAggregateState
	var aliases []aggregate.DocumentAddress
	canonical := nativeRecordCanonical(t, "true")
	for index := range count {
		destination, err := output.Parse(fmt.Sprintf("@pi-agent/documents/document-%d.json", index))
		if err != nil {
			t.Fatal(err)
		}
		alias, err := aggregate.NewDocumentAddress(target.TargetPi, target.ScopeGlobal, destination)
		if err != nil {
			t.Fatal(err)
		}
		contract := stateGroupContract(t, alias, fmt.Sprintf("key-%d", index))
		rows = append(rows, selectionNativeRow(t, contract, canonical))
		aliases = append(aliases, alias)
	}
	ctx := &documentTraversalContext{Context: t.Context()}
	index, err := indexDocumentSnapshot(ctx, selectionState(t, rows...))
	if err != nil {
		t.Fatal(err)
	}
	for position, alias := range aliases {
		footprints, err := index.documentFootprints(ctx, []aggregate.DocumentAddress{alias})
		if err != nil || len(footprints) != 1 {
			t.Fatalf("indexed document membership: %v", err)
		}
		want := rows[position].Contribution().Contract().Address().Footprint()
		if _, found := footprints[want]; !found {
			t.Fatal("document lookup borrowed another address's footprint")
		}
		delete(footprints, want)
		again, err := index.documentFootprints(ctx, []aggregate.DocumentAddress{alias})
		if err != nil || len(again) != 1 {
			t.Fatal("query result mutation changed indexed state")
		}
	}
	// A generous linear traversal ceiling detects a full Snapshot scan per query,
	// without depending on exact check placement or wall-clock timing.
	if ctx.checks > 32*(len(rows)+2*len(aliases)) {
		t.Fatalf("membership traversal multiplies state by document count: %d checks", ctx.checks)
	}
}

func TestDocumentMembershipIndexKeepsWholeNativeAndProjectScopesDistinct(t *testing.T) {
	fixture := documentClaimsForTest(t)
	literal := membershipNativeRow(t, fixture.alias, "native-json-v1", "a/b")
	nested := membershipNativeRow(t, fixture.alias, "native-json-v1", "a", "b")
	whole := nativeWholeFileJournalBaseline(t, target.TargetCodex, target.ScopeGlobal)
	wholeAlias, err := aggregate.NewDocumentAddress(target.TargetCodex, whole.Scope(), whole.Destination())
	if err != nil {
		t.Fatal(err)
	}
	project := selectionNativeRow(t, piSelectionContract(t, ".pi/project.json", "project"), "true")
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{whole}, ManagedAggregates: []durable.ManagedAggregateState{literal, nested, project}})
	if err != nil {
		t.Fatal(err)
	}
	index, err := indexDocumentSnapshot(t.Context(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	footprints, err := index.documentFootprints(t.Context(), []aggregate.DocumentAddress{fixture.alias, wholeAlias, project.Contribution().Contract().Address().Document()})
	if err != nil || len(footprints) != 3 {
		t.Fatalf("scope or native footprint identity was lost: %v", err)
	}
	for _, want := range []output.Footprint{literal.Contribution().Contract().Address().Footprint(), nested.Contribution().Contract().Address().Footprint(), output.WholePathFootprint()} {
		if _, found := footprints[want]; !found {
			t.Fatal("complete global membership was lost")
		}
	}
	projectOnly, err := index.documentFootprints(t.Context(), []aggregate.DocumentAddress{project.Contribution().Contract().Address().Document()})
	if err != nil || len(projectOnly) != 0 {
		t.Fatal("project membership became global claims")
	}
}

func TestDocumentMembershipAndBindingTraversalHonorMidOperationCancellation(t *testing.T) {
	fixture := nativeJournalFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	traversal := &documentTraversalContext{Context: ctx, cancel: cancel, cancelAt: 12}
	content, err := marshalNativeJournalRecord(t.Context(), fixture.record, fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeJournalRecord(t.Context(), content, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	group := &decoded.record.DocumentGroups[0]
	for index := 1; index < 8; index++ {
		alias, binding := group.Aliases[0], group.BindingIntents[0]
		alias.Address.Destination = fmt.Sprintf("@pi-agent/cancel-%d.json", index)
		binding.Destination = alias.Address.Destination
		group.Aliases = append(group.Aliases, alias)
		group.BindingIntents = append(group.BindingIntents, binding)
	}
	if groups, err := decoded.documentBindings(traversal); !errors.Is(err, context.Canceled) || groups != nil {
		t.Fatalf("binding traversal returned partial groups after cancellation: %v", err)
	}
	ctx, cancelIndex := context.WithCancel(t.Context())
	defer cancelIndex()
	traversal = &documentTraversalContext{Context: ctx, cancel: cancelIndex, cancelAt: 3}
	if index, err := indexDocumentSnapshot(traversal, fixture.before); !errors.Is(err, context.Canceled) || index.paths != nil || index.aggregates != nil || index.globalFootprints != nil {
		t.Fatalf("canceled index returned partial facts: %v", err)
	}
	index, err := indexDocumentSnapshot(t.Context(), fixture.before)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancelQuery := context.WithCancel(t.Context())
	defer cancelQuery()
	traversal = &documentTraversalContext{Context: ctx, cancel: cancelQuery, cancelAt: 4}
	if result, err := index.documentFootprints(traversal, []aggregate.DocumentAddress{fixture.claims.alias}); !errors.Is(err, context.Canceled) || result != nil {
		t.Fatalf("canceled membership query returned partial footprints: %v", err)
	}
}

type documentTraversalContext struct {
	context.Context
	checks   int
	cancel   context.CancelFunc
	cancelAt int
}

func (ctx *documentTraversalContext) Err() error {
	ctx.checks++
	if ctx.cancel != nil && ctx.checks == ctx.cancelAt {
		ctx.cancel()
	}
	return ctx.Context.Err()
}
