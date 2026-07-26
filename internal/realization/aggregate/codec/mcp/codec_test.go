package mcpcodec

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/aggregate"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestCodecForCoversEveryImplementedMCPPlacement(t *testing.T) {
	for _, placement := range aggregate.ImplementedMCPPlacements() {
		codec, ok := For(placement.CodecContractID())
		if !ok {
			t.Fatalf("CodecFor(%q) is missing", placement.CodecContractID())
		}
		if codec.ContractID() != placement.CodecContractID() {
			t.Fatalf(
				"CodecFor(%q).ContractID() = %q",
				placement.CodecContractID(),
				codec.ContractID(),
			)
		}
	}
}

func TestMCPCodecFailureMapsSemanticReasonsWithoutPrivateDiagnosticInterpretation(t *testing.T) {
	tests := []struct {
		code MCPProjectionReasonCode
		want aggregate.CodecFailureReason
	}{
		{
			code: MCPProjectionReasonUnsupportedTransport,
			want: aggregate.CodecFailureUnsupportedTransport,
		},
		{
			code: MCPProjectionReasonUnsupportedManagedField,
			want: aggregate.CodecFailureUnsupportedManagedField,
		},
		{
			code: MCPProjectionReasonSecretLiteralForbidden,
			want: aggregate.CodecFailureSecretLiteralForbidden,
		},
	}

	for _, test := range tests {
		t.Run(string(test.code), func(t *testing.T) {
			failure := mcpCodecFailure(
				newMCPProjectionError(test.code, "/mcpServers/context7", "bounded detail"),
				aggregate.CodecFailureSelectedShapeUnsupported,
				"/mcpServers/context7",
			)
			if failure.Reason() != test.want {
				t.Fatalf("failure reason = %q, want %q", failure.Reason(), test.want)
			}
		})
	}
}

func TestValidatePersistedContributionUsesMCPCodecContract(t *testing.T) {
	operations := mustMCPCodecOperations(t, aggregate.MCPPlacementCodexProject)
	placement := operations.Placement()
	codec, ok := For(placement.CodecContractID())
	if !ok {
		t.Fatalf("For(%q) did not return MCP codec", placement.CodecContractID())
	}
	canonical := mustCanonicalCodexProjectMCPServerEntry(t, CodexProjectMCPServerProjection{
		ServerID: "context7", Command: "npx", Args: []string{"-y", "@upstash/context7-mcp"},
		AdapterContract: aggregate.CodexProjectMCPStdioCommandV1,
	})
	contribution := mcpCodecContribution(t, placement, "context7", canonical)
	if err := codec.ValidateContribution(contribution); err != nil {
		t.Fatalf("ValidateContribution(valid MCP) returned error: %v", err)
	}

	malformed := mcpCodecContribution(t, placement, "context7", []byte("not canonical TOML"))
	if err := codec.ValidateContribution(malformed); err == nil {
		t.Fatal("ValidateContribution accepted malformed MCP canonical bytes")
	}
}

func TestMCPCodecRendersOneMixedMultiProjectionBatch(t *testing.T) {
	operations := mustMCPCodecOperations(t, aggregate.MCPPlacementClaudeProject)
	placement := operations.Placement()
	alphaBefore := mustCanonicalClaudeProjectMCPServerEntry(t, ClaudeProjectMCPServerProjection{
		ServerID: "alpha", Command: "node", Args: []string{"old.js"},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})
	alphaAfter := mustCanonicalClaudeProjectMCPServerEntry(t, ClaudeProjectMCPServerProjection{
		ServerID: "alpha", Command: "node", Args: []string{"new.js"},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})
	betaBefore := mustCanonicalClaudeProjectMCPServerEntry(t, ClaudeProjectMCPServerProjection{
		ServerID: "beta", Command: "node", Args: []string{"beta.js"},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})
	gammaAfter := mustCanonicalClaudeProjectMCPServerEntry(t, ClaudeProjectMCPServerProjection{
		ServerID: "gamma", Command: "node", Args: []string{"gamma.js"},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})
	sibling := mustCanonicalClaudeProjectMCPServerEntry(t, ClaudeProjectMCPServerProjection{
		ServerID: "sibling", Command: "node", Args: []string{"manual.js"},
		AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
	})

	existing := []byte(`{"unmanaged":true}`)
	for serverID, canonical := range map[string][]byte{
		"alpha":   alphaBefore,
		"beta":    betaBefore,
		"sibling": sibling,
	} {
		var err error
		existing, err = operations.MergeCanonicalEntry(existing, serverID, canonical)
		if err != nil {
			t.Fatalf("MergeCanonicalEntry(%q): %v", serverID, err)
		}
	}

	alpha := mcpCodecContribution(t, placement, "alpha", alphaAfter)
	beta := mcpCodecContribution(t, placement, "beta", betaBefore)
	gamma := mcpCodecContribution(t, placement, "gamma", gammaAfter)
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{
		gamma.Contract(),
		alpha.Contract(),
		beta.Contract(),
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, ok := For(placement.CodecContractID())
	if !ok {
		t.Fatal("MCP codec is missing")
	}
	document := aggregate.ExistingDocument(existing)
	before, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	beforeByPath := mcpCodecStatesByPath(before)
	alphaDesired := mcpCodecExclusiveSet(t, alpha)
	gammaDesired := mcpCodecExclusiveSet(t, gamma)
	alphaIntent, err := aggregate.NewProjectionIntent(beforeByPath[alpha.ContentPath()], &alphaDesired)
	if err != nil {
		t.Fatal(err)
	}
	betaIntent, err := aggregate.NewProjectionIntent(beforeByPath[beta.ContentPath()], nil)
	if err != nil {
		t.Fatal(err)
	}
	gammaIntent, err := aggregate.NewProjectionIntent(beforeByPath[gamma.ContentPath()], &gammaDesired)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := aggregate.NewPlan(before, []aggregate.ProjectionIntent{gammaIntent, betaIntent, alphaIntent})
	if err != nil {
		t.Fatal(err)
	}
	rendered, failure := codec.Render(document, plan)
	if failure != nil {
		t.Fatal(failure)
	}

	content := string(rendered.Document().Content())
	for _, want := range []string{`"unmanaged": true`, `"sibling"`, `"alpha"`, `"gamma"`, `"new.js"`} {
		if !strings.Contains(content, want) {
			t.Fatalf("rendered MCP document does not contain %q", want)
		}
	}
	if strings.Contains(content, `"beta"`) || strings.Contains(content, `"old.js"`) {
		t.Fatal("rendered MCP document retained a removed or replaced selected entry")
	}
	expected := mcpCodecStatesByPath(rendered.Expected())
	if !expected[alpha.ContentPath()].Present() ||
		expected[alpha.ContentPath()].CanonicalProjection() != string(alphaAfter) {
		t.Fatal("alpha expected state does not contain the replacement")
	}
	if expected[beta.ContentPath()].Present() {
		t.Fatal("beta expected state remains present")
	}
	if !expected[gamma.ContentPath()].Present() {
		t.Fatal("gamma expected state is absent")
	}
}

func TestMCPCodecRestorePreservesConcurrentUnmanagedSibling(t *testing.T) {
	operations := mustMCPCodecOperations(t, aggregate.MCPPlacementCodexProject)
	placement := operations.Placement()
	alphaCanonical := mustCanonicalCodexProjectMCPServerEntry(t, CodexProjectMCPServerProjection{
		ServerID: "alpha", Command: "node", Args: []string{"alpha.js"},
		AdapterContract: aggregate.CodexProjectMCPStdioCommandV1,
	})
	betaCanonical := mustCanonicalCodexProjectMCPServerEntry(t, CodexProjectMCPServerProjection{
		ServerID: "beta", Command: "node", Args: []string{"beta.js"},
		AdapterContract: aggregate.CodexProjectMCPStdioCommandV1,
	})
	concurrentCanonical := mustCanonicalCodexProjectMCPServerEntry(t, CodexProjectMCPServerProjection{
		ServerID: "concurrent", Command: "node", Args: []string{"manual.js"},
		AdapterContract: aggregate.CodexProjectMCPStdioCommandV1,
	})
	alpha := mcpCodecContribution(t, placement, "alpha", alphaCanonical)
	beta := mcpCodecContribution(t, placement, "beta", betaCanonical)
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{beta.Contract(), alpha.Contract()})
	if err != nil {
		t.Fatal(err)
	}
	codec, ok := For(placement.CodecContractID())
	if !ok {
		t.Fatal("MCP codec is missing")
	}
	baseline, failure := codec.Read(aggregate.AbsentDocument(), selection)
	if failure != nil {
		t.Fatal(failure)
	}

	current := []byte(nil)
	for serverID, canonical := range map[string][]byte{
		"alpha":      alphaCanonical,
		"beta":       betaCanonical,
		"concurrent": concurrentCanonical,
	} {
		current, err = operations.MergeCanonicalEntry(current, serverID, canonical)
		if err != nil {
			t.Fatalf("MergeCanonicalEntry(%q): %v", serverID, err)
		}
	}
	restored, failure := codec.Restore(aggregate.ExistingDocument(current), baseline)
	if failure != nil {
		t.Fatal(failure)
	}
	if !restored.Document().Exists() {
		t.Fatal("restore removed a document containing a concurrent unmanaged sibling")
	}
	for _, serverID := range []string{"alpha", "beta"} {
		present, err := operations.EntryPresent(restored.Document().Content(), serverID)
		if err != nil {
			t.Fatal(err)
		}
		if present {
			t.Fatalf("restore retained selected entry %q", serverID)
		}
	}
	present, err := operations.EntryPresent(restored.Document().Content(), "concurrent")
	if err != nil || !present {
		t.Fatalf("concurrent sibling present = %t, error = %v", present, err)
	}
	for _, state := range restored.Expected().States() {
		if state.Present() || !state.ParentPresent() {
			t.Fatalf(
				"restored expected state = present %t parent %t, want absent selected entry under retained parent",
				state.Present(),
				state.ParentPresent(),
			)
		}
	}
}

func mustMCPCodecOperations(t *testing.T, placementID aggregate.MCPPlacementID) MCPPlacementOperations {
	t.Helper()
	operations, ok := ImplementedMCPPlacementOperationsForID(placementID)
	if !ok {
		t.Fatalf("MCP placement operations %q are missing", placementID)
	}
	return operations
}

func mcpCodecContribution(
	t *testing.T,
	placement aggregate.MCPPlacement,
	serverID string,
	canonical []byte,
) aggregate.ManagedContribution {
	t.Helper()
	contentPath, err := placement.ContentPath(serverID)
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := aggregate.NewManagedContribution(aggregate.ManagedContributionInput{
		PlacementID:           string(placement.ID()),
		Target:                placement.Target(),
		Scope:                 placement.Scope(),
		AggregateRoot:         placement.ConfigPath(),
		ContentPath:           string(contentPath),
		MergeUnit:             aggregate.MergeUnit(placement.MergeUnit()),
		Cardinality:           aggregate.ContributionExclusive,
		SiblingRetention:      aggregate.SiblingRetention(placement.SiblingRetention()),
		SiblingPreservation:   aggregate.PreserveSiblingsSemantic,
		Equivalence:           aggregate.EquivalenceCanonicalSemantic,
		CanonicalContribution: string(canonical),
		CodecContractID:       placement.CodecContractID(),
		ComparedFields:        placement.ComparedFields(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return contribution
}

func mcpCodecExclusiveSet(t *testing.T, contribution aggregate.ManagedContribution) aggregate.ContributionSet {
	t.Helper()
	id, err := entity.New(entity.KindMCPServer, mcpCodecServerID(t, contribution))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologymcp.ProjectionSubject(
		contribution.Target(),
		contribution.Scope(),
		id.Name(),
	)
	if err != nil {
		t.Fatal(err)
	}
	item, err := aggregate.NewSubjectContribution(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func mcpCodecServerID(t *testing.T, contribution aggregate.ManagedContribution) string {
	t.Helper()
	operations, ok := ImplementedMCPPlacementOperationsForCodecContract(contribution.CodecContractID())
	if !ok {
		t.Fatal("MCP placement operations are missing")
	}
	serverID, ok := operations.ServerIDFromContentPath(contribution.Address().ContentPath())
	if !ok {
		t.Fatalf("MCP contribution content path %q is invalid", contribution.ContentPath())
	}
	return serverID
}

func mcpCodecStatesByPath(snapshot aggregate.Snapshot) map[string]aggregate.ProjectionState {
	result := make(map[string]aggregate.ProjectionState)
	for _, state := range snapshot.States() {
		result[string(state.Contract().Address().ContentPath())] = state
	}
	return result
}
