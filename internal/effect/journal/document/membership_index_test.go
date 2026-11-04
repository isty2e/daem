package document

import (
	"context"
	"fmt"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

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

func TestDocumentMembershipLookupDoesNotRescanUnrelatedState(t *testing.T) {
	const count = 512
	var rows []durable.ManagedAggregateState
	var aliases []aggregate.DocumentAddress
	canonical := "true"
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
	index, err := IndexSnapshot(ctx, selectionState(t, rows...))
	if err != nil {
		t.Fatal(err)
	}
	for position, alias := range aliases {
		footprints, err := index.Footprints(ctx, []aggregate.DocumentAddress{alias})
		if err != nil || len(footprints) != 1 {
			t.Fatalf("indexed document membership: %v", err)
		}
		want := rows[position].Contribution().Contract().Address().Footprint()
		if _, found := footprints[want]; !found {
			t.Fatal("document lookup borrowed another address's footprint")
		}
		delete(footprints, want)
		again, err := index.Footprints(ctx, []aggregate.DocumentAddress{alias})
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
	index, err := IndexSnapshot(t.Context(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	footprints, err := index.Footprints(t.Context(), []aggregate.DocumentAddress{fixture.alias, wholeAlias, project.Contribution().Contract().Address().Document()})
	if err != nil || len(footprints) != 3 {
		t.Fatalf("scope or native footprint identity was lost: %v", err)
	}
	for _, want := range []output.Footprint{literal.Contribution().Contract().Address().Footprint(), nested.Contribution().Contract().Address().Footprint(), output.WholePathFootprint()} {
		if _, found := footprints[want]; !found {
			t.Fatal("complete global membership was lost")
		}
	}
	projectOnly, err := index.Footprints(t.Context(), []aggregate.DocumentAddress{project.Contribution().Contract().Address().Document()})
	if err != nil || len(projectOnly) != 0 {
		t.Fatal("project membership became global claims")
	}
}
