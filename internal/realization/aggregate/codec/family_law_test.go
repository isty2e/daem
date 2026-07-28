package aggregatecodec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	"github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/aggregate/hook"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestEveryAggregateCodecObeysLifecycleAndRecoveryLaw(t *testing.T) {
	hookPlacements := aggregate.ImplementedHookPlacements()
	if len(hookPlacements) == 0 {
		t.Fatal("implemented Hook placement catalog is empty")
	}
	for _, placement := range hookPlacements {
		t.Run("hook/"+string(placement.ID()), func(t *testing.T) {
			oldCanonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{
				Event: "Stop", Type: "command", Command: "old-command",
			})
			if err != nil {
				t.Fatal(err)
			}
			newCanonical, err := hookcodec.CanonicalHookContribution(commandhook.ContributionInput{
				Event: "Stop", Type: "command", Command: "new-command",
			})
			if err != nil {
				t.Fatal(err)
			}
			oldContribution, err := placement.Contribution(oldCanonical)
			if err != nil {
				t.Fatal(err)
			}
			newContribution, err := placement.Contribution(newCanonical)
			if err != nil {
				t.Fatal(err)
			}
			subject, err := topology.NewSubjectID(
				topology.SubjectProjection,
				string(placement.ID()),
				"hook:aggregate-law",
			)
			if err != nil {
				t.Fatal(err)
			}
			assertAggregateCodecLifecycleAndRecovery(
				t,
				subject,
				oldContribution,
				newContribution,
				aggregate.ExistingDocument([]byte(`{"external":"SECRET_CANARY"}`)),
			)
		})
	}

	mcpPlacements := aggregate.ImplementedMCPPlacements()
	if len(mcpPlacements) == 0 {
		t.Fatal("implemented MCP placement catalog is empty")
	}
	for _, placement := range mcpPlacements {
		t.Run("mcp/"+string(placement.ID()), func(t *testing.T) {
			oldCanonical := aggregateLawMCPCanonical(t, placement.ID(), "context7", "old-command")
			newCanonical := aggregateLawMCPCanonical(t, placement.ID(), "context7", "new-command")
			oldContribution := aggregateLawMCPContribution(t, placement, "context7", oldCanonical)
			newContribution := aggregateLawMCPContribution(t, placement, "context7", newCanonical)
			id, err := entity.New(entity.KindMCPServer, "context7")
			if err != nil {
				t.Fatal(err)
			}
			subject, err := topologymcp.ProjectionSubject(
				placement.Target(),
				placement.Scope(),
				id.Name(),
			)
			if err != nil {
				t.Fatal(err)
			}
			before := aggregate.ExistingDocument([]byte(`{"external":"SECRET_CANARY"}`))
			if placement.ID() == aggregate.MCPPlacementCodexProject ||
				placement.ID() == aggregate.MCPPlacementCodexGlobal {
				before = aggregate.ExistingDocument([]byte("external = \"SECRET_CANARY\"\n"))
			}
			assertAggregateCodecLifecycleAndRecovery(
				t,
				subject,
				oldContribution,
				newContribution,
				before,
			)
		})
	}
}

func assertAggregateCodecLifecycleAndRecovery(
	t *testing.T,
	subject topology.SubjectID,
	oldContribution aggregate.ManagedContribution,
	newContribution aggregate.ManagedContribution,
	beforeDocument aggregate.Document,
) {
	t.Helper()
	if !oldContribution.Contract().Equal(newContribution.Contract()) {
		t.Fatal("aggregate lifecycle fixture changed its static projection contract")
	}
	if err := aggregate.ValidateSubjectContract(subject, oldContribution.Contract()); err != nil {
		t.Fatalf("ValidateSubjectContract returned error: %v", err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{oldContribution.Contract()})
	if err != nil {
		t.Fatal(err)
	}
	codec, ok := Catalog().Lookup(selection.CodecContractID())
	if !ok {
		t.Fatalf("codec %q is not admitted", selection.CodecContractID())
	}

	beforeSnapshot := aggregateLawRead(t, codec, beforeDocument, selection)
	created := aggregateLawRender(t, codec, beforeDocument, beforeSnapshot, subject, &oldContribution)
	createdSnapshot := aggregateLawRead(t, codec, created.Document(), selection)
	createdCanonical := created.Expected().States()[0].CanonicalProjection()
	assertAggregateLawProjection(t, createdSnapshot, createdCanonical, true)
	assertAggregateLawCanary(t, created.Document())

	updated := aggregateLawRender(t, codec, created.Document(), createdSnapshot, subject, &newContribution)
	updatedSnapshot := aggregateLawRead(t, codec, updated.Document(), selection)
	updatedCanonical := updated.Expected().States()[0].CanonicalProjection()
	if updatedCanonical == createdCanonical {
		t.Fatal("aggregate lifecycle update did not change the selected projection")
	}
	assertAggregateLawProjection(t, updatedSnapshot, updatedCanonical, true)
	assertAggregateLawCanary(t, updated.Document())

	recoveredUpdate, failure := codec.Restore(updated.Document(), createdSnapshot)
	if failure != nil {
		t.Fatalf("Restore(update): %v", failure)
	}
	recoveredUpdateSnapshot := aggregateLawRead(t, codec, recoveredUpdate.Document(), selection)
	assertAggregateLawProjection(t, recoveredUpdateSnapshot, createdCanonical, true)
	assertAggregateLawCanary(t, recoveredUpdate.Document())

	removed := aggregateLawRender(t, codec, recoveredUpdate.Document(), recoveredUpdateSnapshot, subject, nil)
	removedSnapshot := aggregateLawRead(t, codec, removed.Document(), selection)
	assertAggregateLawProjection(t, removedSnapshot, "", false)
	assertAggregateLawCanary(t, removed.Document())

	recoveredRemoval, failure := codec.Restore(removed.Document(), recoveredUpdateSnapshot)
	if failure != nil {
		t.Fatalf("Restore(remove): %v", failure)
	}
	recoveredRemovalSnapshot := aggregateLawRead(t, codec, recoveredRemoval.Document(), selection)
	assertAggregateLawProjection(t, recoveredRemovalSnapshot, createdCanonical, true)
	assertAggregateLawCanary(t, recoveredRemoval.Document())

	recoveredCreate, failure := codec.Restore(created.Document(), beforeSnapshot)
	if failure != nil {
		t.Fatalf("Restore(create): %v", failure)
	}
	recoveredCreateSnapshot := aggregateLawRead(t, codec, recoveredCreate.Document(), selection)
	assertAggregateLawProjection(t, recoveredCreateSnapshot, "", false)
	assertAggregateLawCanary(t, recoveredCreate.Document())
}

func aggregateLawRead(
	t *testing.T,
	codec aggregate.Codec,
	document aggregate.Document,
	selection aggregate.Selection,
) aggregate.Snapshot {
	t.Helper()
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatalf("Read: %v", failure)
	}
	if len(snapshot.States()) != 1 {
		t.Fatalf("snapshot states = %d, want one", len(snapshot.States()))
	}
	return snapshot
}

func aggregateLawRender(
	t *testing.T,
	codec aggregate.Codec,
	document aggregate.Document,
	before aggregate.Snapshot,
	subject topology.SubjectID,
	contribution *aggregate.ManagedContribution,
) aggregate.RenderedDocument {
	t.Helper()
	var desired *aggregate.ContributionSet
	if contribution != nil {
		item, err := aggregate.NewSubjectContribution(subject, *contribution)
		if err != nil {
			t.Fatal(err)
		}
		set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
		if err != nil {
			t.Fatal(err)
		}
		desired = &set
	}
	intent, err := aggregate.NewProjectionIntent(before.States()[0], desired)
	if err != nil {
		t.Fatal(err)
	}
	codecPlan, err := aggregate.NewPlan(before, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	rendered, failure := codec.Render(document, codecPlan)
	if failure != nil {
		t.Fatalf("Render: %v", failure)
	}
	return rendered
}

func assertAggregateLawProjection(
	t *testing.T,
	snapshot aggregate.Snapshot,
	wantCanonical string,
	wantPresent bool,
) {
	t.Helper()
	state := snapshot.States()[0]
	if state.Present() != wantPresent {
		t.Fatalf("projection present = %t, want %t", state.Present(), wantPresent)
	}
	if state.CanonicalProjection() != wantCanonical {
		t.Fatalf("canonical projection = %q, want %q", state.CanonicalProjection(), wantCanonical)
	}
}

func assertAggregateLawCanary(t *testing.T, document aggregate.Document) {
	t.Helper()
	if !document.Exists() || !strings.Contains(string(document.Content()), "SECRET_CANARY") {
		t.Fatalf("aggregate lifecycle lost unmanaged canary: %s", document.Content())
	}
}

func aggregateLawMCPCanonical(
	t *testing.T,
	placementID aggregate.MCPPlacementID,
	serverID string,
	command string,
) []byte {
	t.Helper()
	args := []string{"server.js"}
	var (
		canonical []byte
		err       error
	)
	switch placementID {
	case aggregate.MCPPlacementClaudeProject:
		canonical, err = mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{
			ServerID: serverID, Command: command, Args: args, AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
		})
	case aggregate.MCPPlacementClaudeGlobal:
		canonical, err = mcpcodec.CanonicalClaudeGlobalMCPServerEntry(mcpcodec.ClaudeGlobalMCPServerProjection{
			ServerID: serverID, Command: command, Args: args,
			Env:             map[string]string{"API_TOKEN": "${CONTEXT7_API_TOKEN}"},
			AdapterContract: aggregate.ClaudeGlobalMCPStdioEnvAdapterV1,
		})
	case aggregate.MCPPlacementAntigravityGlobal:
		canonical, err = mcpcodec.CanonicalAntigravityGlobalMCPServerEntry(mcpcodec.AntigravityGlobalMCPServerProjection{
			ServerID: serverID, Command: command, Args: args, AdapterContract: aggregate.AntigravityGlobalMCPAmbientEnvV1,
		})
	case aggregate.MCPPlacementOpenCodeProject:
		canonical, err = mcpcodec.CanonicalOpenCodeProjectMCPServerEntry(mcpcodec.MCPNoEnvServerProjection{
			ServerID: serverID, Command: command, Args: args, AdapterContract: aggregate.OpenCodeProjectMCPLocalCommandV1,
		})
	case aggregate.MCPPlacementOpenCodeGlobal:
		canonical, err = mcpcodec.CanonicalOpenCodeGlobalMCPServerEntry(mcpcodec.OpenCodeGlobalMCPServerProjection{
			ServerID: serverID, Command: command, Args: args,
			AdapterContract: aggregate.OpenCodeGlobalMCPLocalEnvV1,
		})
	case aggregate.MCPPlacementCodexProject:
		canonical, err = mcpcodec.CanonicalCodexProjectMCPServerEntry(mcpcodec.MCPNoEnvServerProjection{
			ServerID: serverID, Command: command, Args: args, AdapterContract: aggregate.CodexProjectMCPStdioCommandV1,
		})
	case aggregate.MCPPlacementCodexGlobal:
		canonical, err = mcpcodec.CanonicalCodexGlobalMCPServerEntry(mcpcodec.CodexGlobalMCPServerProjection{
			ServerID: serverID, Command: command, Args: args, EnvVars: []string{"CODEX_TOKEN"},
			AdapterContract: aggregate.CodexGlobalMCPStdioEnvVarsV1,
		})
	default:
		t.Fatalf("unhandled MCP placement %q", placementID)
	}
	if err != nil {
		t.Fatalf("canonical MCP contribution for %q: %v", placementID, err)
	}
	return canonical
}

func aggregateLawMCPContribution(
	t *testing.T,
	placement aggregate.MCPPlacement,
	serverID string,
	canonical []byte,
) aggregate.ManagedContribution {
	t.Helper()
	contribution, err := placement.Contribution(serverID, string(canonical))
	if err != nil {
		t.Fatal(err)
	}
	return contribution
}
