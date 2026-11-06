package aggregatefixture

import (
	"os"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/target"
)

type MCPEntry struct {
	ServerID string
	Command  string
}

func ClaudeProjectMCP(t testing.TB, before aggregate.Document, specs []MCPEntry) []reconcile.AggregateDecision {
	t.Helper()
	contracts := make([]lock.LockedSubjectContract, 0, len(specs))
	desired := make([]aggregate.SubjectContribution, 0, len(specs))
	projectionContracts := make([]aggregate.ProjectionContract, 0, len(specs))
	for _, spec := range specs {
		canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{ServerID: spec.ServerID, Command: spec.Command, AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1})
		if err != nil {
			t.Fatal(err)
		}
		contract := snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{PlacementID: aggregate.MCPPlacementClaudeProject, ServerID: spec.ServerID, LauncherCommand: spec.Command, CanonicalProjection: string(canonical)})
		item, present, err := contract.ManagedAggregateContribution()
		if err != nil || !present {
			t.Fatal("MCP fixture has no aggregate contribution", err)
		}
		contracts = append(contracts, contract)
		desired = append(desired, item)
		projectionContracts = append(projectionContracts, item.Contribution().Contract())
	}
	locked, err := lock.NewLockedSection(contracts, nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection(projectionContracts)
	if err != nil {
		t.Fatal(err)
	}
	codecs := aggregatecodec.Catalog()
	codec, present := codecs.Lookup(selection.CodecContractID())
	if !present {
		t.Fatal("MCP fixture codec is not admitted")
	}
	snapshot, failure := codec.Read(before, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	mode := os.FileMode(0)
	if before.Exists() {
		mode = aggregate.DocumentFileMode
	}
	evidence, err := observe.NewAggregateEvidence(before, snapshot, mode)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := reconcile.NewSelectedTargets([]target.Target{target.TargetClaudeCode})
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := reconcileprojection.BuildAggregateDecisions(reconcileprojection.AggregateInput{Locked: locked, Expected: contracts, Desired: desired, Evidence: []observe.AggregateEvidence{evidence}, SelectedTargets: selected, Codecs: codecs})
	if err != nil {
		t.Fatal(err)
	}
	return decisions
}
