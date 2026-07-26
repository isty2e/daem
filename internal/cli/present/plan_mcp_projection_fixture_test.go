package clipresent

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/reconcile"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/target"
)

func mcpProjectionPlan(t *testing.T) reconcile.Result {
	t.Helper()
	canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(
		mcpcodec.ClaudeProjectMCPServerProjection{
			ServerID:        "context7",
			Command:         "npx",
			AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
		},
	)
	if err != nil {
		t.Fatalf("CanonicalClaudeProjectMCPServerEntry returned error: %v", err)
	}
	contract := snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{
		PlacementID:         aggregate.MCPPlacementClaudeProject,
		ServerID:            "context7",
		LauncherCommand:     "npx",
		CanonicalProjection: string(canonical),
	})
	contribution, present, err := contract.ManagedAggregateContribution()
	if err != nil || !present {
		t.Fatalf("ManagedAggregateContribution = %#v, %t, %v", contribution, present, err)
	}
	projectionContract := contribution.Contribution().Contract()
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{projectionContract})
	if err != nil {
		t.Fatal(err)
	}
	codec, present := aggregatecodec.Catalog().Lookup(selection.CodecContractID())
	if !present {
		t.Fatalf("codec %q is not admitted", selection.CodecContractID())
	}
	before := aggregate.AbsentDocument()
	snapshot, failure := codec.Read(before, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	evidence, err := observe.NewAggregateEvidence(before, snapshot, 0)
	if err != nil {
		t.Fatal(err)
	}
	selectedTargets, err := reconcile.NewSelectedTargets([]target.Target{target.TargetClaudeCode})
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := reconcileprojection.BuildAggregateDecisions(reconcileprojection.AggregateInput{
		Locked:          snapshottest.Section(t, contract),
		Expected:        []lock.LockedSubjectContract{contract},
		Desired:         []aggregate.SubjectContribution{contribution},
		Evidence:        []observe.AggregateEvidence{evidence},
		SelectedTargets: selectedTargets,
		Codecs:          aggregatecodec.Catalog(),
	})
	if err != nil {
		t.Fatalf("BuildAggregateDecisions returned error: %v", err)
	}
	return mustReconciliationPlan(t, nil, decisions)
}
