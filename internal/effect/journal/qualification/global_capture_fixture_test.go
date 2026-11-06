package qualification_test

import (
	"testing"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func claudeRecoveryBaselineMutation(t *testing.T, scope target.Scope, serverID string) journalrecord.CaptureMutation {
	t.Helper()
	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetClaudeCode, scope)
	if !ok {
		t.Fatalf("Claude MCP placement for %s is missing", scope)
	}
	var canonical []byte
	var err error
	if scope == target.ScopeProject {
		canonical, err = mcpcodec.CanonicalClaudeProjectMCPServerEntry(mcpcodec.ClaudeProjectMCPServerProjection{ServerID: serverID, Command: "npx", AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1})
	} else {
		canonical, err = mcpcodec.CanonicalClaudeGlobalMCPServerEntry(mcpcodec.ClaudeGlobalMCPServerProjection{ServerID: serverID, Command: "npx", AdapterContract: aggregate.ClaudeGlobalMCPStdioEnvAdapterV1})
	}
	if err != nil {
		t.Fatal(err)
	}
	contribution, err := placement.Contribution(serverID, string(canonical))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologymcp.ProjectionSubject(target.TargetClaudeCode, scope, serverID)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := aggregate.NewSubjectContribution(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	desired, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{selected})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
	if err != nil {
		t.Fatal(err)
	}
	codec, ok := journalTestCodecs().Lookup(placement.CodecContractID())
	if !ok {
		t.Fatal("MCP fixture codec is missing")
	}
	before := aggregate.AbsentDocument()
	snapshot, failure := codec.Read(before, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	intent, err := aggregate.NewProjectionIntent(snapshot.States()[0], &desired)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := aggregate.NewPlan(snapshot, []aggregate.ProjectionIntent{intent})
	if err != nil {
		t.Fatal(err)
	}
	expected, failure := codec.Render(before, plan)
	if failure != nil {
		t.Fatal(failure)
	}
	mutation, err := journalrecord.NewManagedAggregateMutation(journalrecord.AggregateMutationCreate, subject, contribution.Contract(), before, snapshot, expected, 0)
	if err != nil {
		t.Fatal(err)
	}
	values, err := journalrecord.NormalizeCaptureMutations(nil, []journalrecord.ManagedAggregateMutation{mutation}, nil)
	if err != nil || len(values) != 1 {
		t.Fatal("normalize aggregate capture fixture", err)
	}
	return values[0]
}
