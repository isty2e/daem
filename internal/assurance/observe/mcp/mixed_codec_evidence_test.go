package mcp

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/target"
)

func TestMCPIndexRetainsItsCodecBesideNativeObservation(t *testing.T) {
	canonical, err := mcpcodec.CanonicalCodexProjectMCPServerEntry(mcpcodec.MCPNoEnvServerProjection{ServerID: "peer", Command: "npx", Args: []string{}, AdapterContract: aggregate.CodexProjectMCPStdioCommandV1})
	if err != nil {
		t.Fatal(err)
	}
	locked := snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{PlacementID: aggregate.MCPPlacementCodexProject, ServerID: "peer", LauncherCommand: "npx", CanonicalProjection: string(canonical)})
	item, present, err := locked.ManagedAggregateContribution()
	if err != nil || !present {
		t.Fatal("fixture lacks a real MCP contribution", err)
	}
	mcp := item.Contribution().Contract()
	key, err := output.NewKeyPath("theme")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	nativeID, _ := aggregate.HostConfigCodecForSyntax("toml")
	native, err := aggregate.NewHostConfigProjectionContract(target.TargetCodex, target.ScopeProject, mcp.Address().Document().AggregateRoot(), footprint, nativeID)
	if err != nil {
		t.Fatal(err)
	}
	limits := configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
	catalog, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{nativeID, mcp.CodecContractID()}, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	document := aggregate.ExistingDocument([]byte("theme = 'dark'\n[mcp_servers.peer]\ncommand = 'npx'\nargs = []\n"))
	var observations []observe.AggregateEvidence
	for _, contract := range []aggregate.ProjectionContract{native, mcp} {
		selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
		if err != nil {
			t.Fatal(err)
		}
		codec, _ := catalog.Lookup(contract.CodecContractID())
		snapshot, failure := codec.Read(document, selection)
		if failure != nil {
			t.Fatal(failure)
		}
		evidence, err := observe.NewAggregateEvidence(document, snapshot, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		observations = append(observations, evidence)
	}
	index, err := newAggregateObservationIndex(observations, nil, nil)
	if err != nil {
		t.Fatal("MCP lookup discarded its native sibling before classification", err)
	}
	current, err := index.projection(mcp)
	want, covered := observations[1].Snapshot().State(mcp)
	if err != nil || !current.observed || !covered || current.state.CanonicalProjection() != want.CanonicalProjection() || current.failureReason != "" || current.unsupportedAlternateConfig {
		t.Fatal("MCP lookup consumed another codec's projection or failure", err)
	}
}
