package execute

import (
	"context"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
)

func TestMaximumForwardEffectValidationCountMatchesSuccessfulExecution(t *testing.T) {
	fixture := newApplyEventFixture(t)
	actions := []applyEventAction{
		fixture.createAction("create", "CREATE.md", "created\n"),
		fixture.updateAction("update", "UPDATE.md", "old update\n", "new update\n"),
		fixture.deleteAction("delete", "DELETE.md", "delete me\n"),
		fixture.recordAction("record", "RECORD.md", "record me\n"),
	}
	input := fixture.input(actions)
	maximum, err := MaximumForwardEffectValidationCount(input)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := ApplyEffectSegment(input)
	if err != nil {
		t.Fatal(err)
	}
	var builder operationplan.EffectStructureBuilder
	structure, err := builder.Compile(builder.ForwardPhase("apply", segment))
	if err != nil {
		t.Fatal(err)
	}
	demand, err := structure.LegacyDemand()
	if err != nil {
		t.Fatal(err)
	}
	if got := demand.EnsureCalls() + demand.StateDirValidationCalls(); got != maximum {
		t.Fatalf("typed forward demand = %d, legacy maximum = %d", got, maximum)
	}
	validationCalls := 0
	_, err = ApplyWithOptions(context.Background(), input, ApplyOptions{
		ValidateBeforeEffects: func(context.Context, mutation.PhysicalAuthoritySet) error {
			validationCalls++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if validationCalls != maximum {
		t.Fatalf("forward validation calls = %d, maximum plan = %d", validationCalls, maximum)
	}
}
