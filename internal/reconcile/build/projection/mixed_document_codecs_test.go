package projection

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestMixedDocumentCodecsKeepIndependentSelectedEvidence(t *testing.T) {
	input := mixedDocumentCodecsInput(t)
	decisions, err := BuildAggregateDecisions(input)
	if err != nil || len(decisions) != 2 {
		t.Fatal("one logical document lost a native or MCP codec selection", err)
	}
	seen := make(map[aggregate.CodecContractID]struct{})
	for _, decision := range decisions {
		if !decision.MutatesHost() || len(decision.Projections()) != 1 || decision.Projections()[0].Kind() != reconcile.AggregateRemove || len(decision.PreviousStates()) != 1 || len(decision.DesiredContributions()) != 0 {
			t.Fatalf("mixed retirement changed transition: codec=%s %s/%s: %s", decision.CodecContractID(), decision.Kind(), decision.Reason(), decision.Detail())
		}
		seen[decision.CodecContractID()] = struct{}{}
	}
	if len(seen) != 2 || decisions[0].DocumentAddress() != decisions[1].DocumentAddress() {
		t.Fatal("fixture did not exercise distinct codecs at one logical document")
	}
	input.Evidence = append(input.Evidence, input.Evidence[0])
	if result, err := BuildAggregateDecisions(input); err == nil || len(result) != 0 {
		t.Fatal("duplicate evidence for one codec yielded usable decisions")
	}
}

func TestMixedDocumentCodecFailureDoesNotEraseOtherCodecEvidence(t *testing.T) {
	input := mixedDocumentCodecsInput(t)
	document := aggregate.ExistingDocument([]byte("theme = 'dark'\nmcp_servers = 1\n"))
	var successes []observe.AggregateEvidence
	var failures []observe.AggregateObservationFailure
	for _, current := range input.Evidence {
		selection, err := current.Snapshot().Selection()
		if err != nil {
			t.Fatal(err)
		}
		codec, _ := input.Codecs.Lookup(selection.CodecContractID())
		snapshot, failure := codec.Read(document, selection)
		if failure != nil {
			observed, err := observe.NewAggregateObservationFailure(document, selection, 0o600, failure)
			if err != nil {
				t.Fatal(err)
			}
			failures = append(failures, observed)
			continue
		}
		observed, err := observe.NewAggregateEvidence(document, snapshot, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		successes = append(successes, observed)
	}
	if len(successes) != 1 || len(failures) != 1 {
		t.Fatal("fixture did not isolate one real codec failure")
	}
	input.Evidence, input.ObservationFailures = successes, failures
	decisions, err := BuildAggregateDecisions(input)
	if err != nil || len(decisions) != 2 {
		t.Fatal("one codec's failure erased another codec's valid observation", err)
	}
	for _, decision := range decisions {
		if decision.CodecContractID() == failures[0].Selection().CodecContractID() {
			if !decision.IsBlocked() || decision.MutatesHost() || decision.MutatesState() {
				t.Fatal("failed codec produced a mutating decision")
			}
		} else if !decision.MutatesHost() || len(decision.Projections()) != 1 || decision.Projections()[0].Kind() != reconcile.AggregateRemove {
			t.Fatalf("successful codec changed transition: %s/%s: %s", decision.Kind(), decision.Reason(), decision.Detail())
		}
	}
	input.ObservationFailures = append(input.ObservationFailures, input.ObservationFailures[0])
	if result, err := BuildAggregateDecisions(input); err == nil || len(result) != 0 {
		t.Fatal("duplicate failed codec observation yielded usable decisions")
	}
}

func TestMixedDocumentCodecsRejectUnselectedCodecAtKnownDocument(t *testing.T) {
	input := mixedDocumentCodecsInput(t)
	base := input.States[0].Contribution().Contract()
	id, _ := aggregate.HostConfigCodecForSyntax("json")
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetCodex, target.ScopeProject, base.Address().Document().AggregateRoot(), base.Address().Footprint(), id)
	if err != nil {
		t.Fatal(err)
	}
	limits := configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
	catalog, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{id}, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	codec, _ := catalog.Lookup(id)
	document := aggregate.ExistingDocument([]byte(`{"theme":"dark"}`))
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	foreign, err := observe.NewAggregateEvidence(document, snapshot, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	input.Evidence = append(input.Evidence, foreign)
	if result, err := BuildAggregateDecisions(input); err == nil || len(result) != 0 {
		t.Fatal("known document silently admitted an unselected codec observation")
	}
}

func mixedDocumentCodecsInput(t *testing.T) AggregateInput {
	t.Helper()
	nativeID, _ := aggregate.HostConfigCodecForSyntax("toml")
	limits := configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
	catalog, err := aggregatecodec.ForOperation(t.Context(), []aggregate.CodecContractID{nativeID, aggregate.MCPCodecCodexProjectStdioCommand}, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath("theme")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetCodex, target.ScopeProject, outputtest.Parse(t, ".codex/config.toml"), footprint, nativeID)
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHostConfig, "settings")
	if err != nil {
		t.Fatal(err)
	}
	nativeSubject, err := topologyprojection.HostConfigKeySubject(id, target.TargetCodex, target.ScopeProject, key)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := mcpcodec.CanonicalCodexProjectMCPServerEntry(mcpcodec.MCPNoEnvServerProjection{ServerID: "peer", Command: "npx", Args: []string{}, AdapterContract: aggregate.CodexProjectMCPStdioCommandV1})
	if err != nil {
		t.Fatal(err)
	}
	locked := snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{PlacementID: aggregate.MCPPlacementCodexProject, ServerID: "peer", LauncherCommand: "npx", CanonicalProjection: string(canonical)})
	mcp, present, err := locked.ManagedAggregateContribution()
	if err != nil || !present {
		t.Fatal("fixture has no admitted MCP contribution", err)
	}
	document := aggregate.ExistingDocument([]byte("theme = 'dark'\n[mcp_servers.peer]\ncommand = 'npx'\nargs = []\n"))
	input := AggregateInput{SelectedTargets: planSelectedTargets(t, target.TargetCodex), Codecs: catalog}
	for _, item := range []struct {
		contract aggregate.ProjectionContract
		native   bool
	}{{contract, true}, {mcp.Contribution().Contract(), false}} {
		selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{item.contract})
		if err != nil {
			t.Fatal(err)
		}
		codec, _ := catalog.Lookup(item.contract.CodecContractID())
		snapshot, failure := codec.Read(document, selection)
		if failure != nil || !snapshot.States()[0].Present() {
			t.Fatal("fixture selected value is not actually present", failure)
		}
		body, err := aggregate.NewManagedContribution(item.contract, snapshot.States()[0].CanonicalProjection())
		if err != nil {
			t.Fatal(err)
		}
		subject := mcp.SubjectID()
		if item.native {
			subject = nativeSubject
		}
		row, err := durable.NewManagedAggregateState(subject, body)
		if err != nil {
			t.Fatal(err)
		}
		evidence, err := observe.NewAggregateEvidence(document, snapshot, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		input.States = append(input.States, row)
		input.Evidence = append(input.Evidence, evidence)
	}
	return input
}
