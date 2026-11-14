package record

import (
	"context"
	"errors"
	"fmt"
	"testing"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

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
	wire := decoded.native16Record()
	group := &wire.DocumentGroups[0]
	for index := 1; index < 8; index++ {
		alias, binding := group.Aliases[0], group.BindingIntents[0]
		alias.Address.Destination = fmt.Sprintf("@pi-agent/cancel-%d.json", index)
		binding.Destination = alias.Address.Destination
		group.Aliases = append(group.Aliases, alias)
		group.BindingIntents = append(group.BindingIntents, binding)
	}
	decoded, err = newNative16Value(wire, decoded.owner, decoded.before, decoded.after)
	if err != nil {
		t.Fatal(err)
	}
	if groups, err := decoded.documentBindings(traversal); !errors.Is(err, context.Canceled) || groups != nil {
		t.Fatalf("binding traversal returned partial groups after cancellation: %v", err)
	}
	ctx, cancelIndex := context.WithCancel(t.Context())
	defer cancelIndex()
	traversal = &documentTraversalContext{Context: ctx, cancel: cancelIndex, cancelAt: 3}
	partial, err := journaldocument.IndexSnapshot(traversal, fixture.before)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled index returned partial facts: %v", err)
	}
	partialSubjects, err := partial.Subjects(t.Context(), []aggregate.DocumentAddress{fixture.claims.alias})
	if err != nil || len(partialSubjects) != 0 {
		t.Fatalf("canceled index retained partial membership: %v", err)
	}
	partialFootprints, err := partial.Footprints(t.Context(), []aggregate.DocumentAddress{fixture.claims.alias})
	if err != nil || len(partialFootprints) != 0 {
		t.Fatalf("canceled index retained partial claim footprints: %v", err)
	}
	index, err := journaldocument.IndexSnapshot(t.Context(), fixture.before)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancelQuery := context.WithCancel(t.Context())
	defer cancelQuery()
	traversal = &documentTraversalContext{Context: ctx, cancel: cancelQuery, cancelAt: 4}
	if result, err := index.Footprints(traversal, []aggregate.DocumentAddress{fixture.claims.alias}); !errors.Is(err, context.Canceled) || result != nil {
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
