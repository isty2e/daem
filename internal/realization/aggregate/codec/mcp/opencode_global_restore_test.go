package mcpcodec

import (
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestOpenCodeGlobalRestoreUsesGlobalEnvironmentGrammar(t *testing.T) {
	operations, ok := ImplementedMCPPlacementOperationsForPlacement(aggregate.MCPPlacementOpenCodeGlobal)
	if !ok {
		t.Fatal("OpenCode global placement operations missing")
	}
	canonical, err := CanonicalOpenCodeGlobalMCPServerEntry(OpenCodeGlobalMCPServerProjection{
		ServerID:        "context7",
		Command:         "npx",
		Args:            []string{"-y"},
		Environment:     map[string]string{"TOKEN": "{env:SOURCE_TOKEN}"},
		AdapterContract: aggregate.OpenCodeGlobalMCPLocalEnvV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := NewMCPProjectionUpsert("context7", canonical)
	if err != nil {
		t.Fatal(err)
	}

	content, keep, err := operations.RestoreMutations(
		nil,
		[]MCPProjectionMutation{mutation},
		false,
	)
	if err != nil {
		t.Fatalf("RestoreMutations returned error: %v", err)
	}
	if !keep {
		t.Fatal("RestoreMutations removed a present global projection document")
	}
	entry, present, err := ExtractOpenCodeGlobalMCPServerProjection(content, "context7")
	if err != nil || !present {
		t.Fatalf("restored global entry = (%#v, %t, %v)", entry, present, err)
	}
	if entry.Environment["TOKEN"] != "{env:SOURCE_TOKEN}" {
		t.Fatalf("restored environment = %#v, want exact host reference", entry.Environment)
	}
}

func TestOpenCodeGlobalEnvironmentRoundTripsThroughCodecRecovery(t *testing.T) {
	operations := mustMCPCodecOperations(t, aggregate.MCPPlacementOpenCodeGlobal)
	placement := operations.Placement()
	canonical, err := CanonicalOpenCodeGlobalMCPServerEntry(OpenCodeGlobalMCPServerProjection{
		ServerID:        "context7",
		Command:         "npx",
		Args:            []string{"-y"},
		Environment:     map[string]string{"TOKEN": "{env:SOURCE_TOKEN}"},
		AdapterContract: aggregate.OpenCodeGlobalMCPLocalEnvV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	contribution := mcpCodecContribution(t, placement, "context7", canonical)
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
	if err != nil {
		t.Fatal(err)
	}
	codec, ok := For(placement.CodecContractID())
	if !ok {
		t.Fatal("OpenCode global codec missing")
	}
	before, failure := codec.Read(aggregate.AbsentDocument(), selection)
	if failure != nil {
		t.Fatal(failure)
	}
	desired := mcpCodecExclusiveSet(t, contribution)
	intent, err := aggregate.NewProjectionIntent(before.States()[0], &desired)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := aggregate.NewPlan(before, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	rendered, failure := codec.Render(aggregate.AbsentDocument(), plan)
	if failure != nil {
		t.Fatal(failure)
	}
	baseline, failure := codec.Read(rendered.Document(), selection)
	if failure != nil {
		t.Fatal(failure)
	}
	restored, failure := codec.Restore(aggregate.AbsentDocument(), baseline)
	if failure != nil {
		t.Fatal(failure)
	}
	readBack, failure := codec.Read(restored.Document(), selection)
	if failure != nil || !readBack.Equal(baseline) {
		t.Fatalf("Read(restored) = %#v, %v, want exact baseline", readBack, failure)
	}
}
