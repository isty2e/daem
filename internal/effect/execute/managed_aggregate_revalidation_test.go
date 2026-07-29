package execute

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	desiredhook "github.com/isty2e/daem/internal/desired/hook"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/aggregate"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/target"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
)

func TestAggregateEffectRejectsDocumentAndModeChangesAfterPlanning(t *testing.T) {
	before := aggregate.ExistingDocument([]byte(`{"unmanaged":{"value":"before"}}` + "\n"))
	effect := hookAggregateInsertionEffect(t, before)

	changed := aggregate.ExistingDocument([]byte(`{"unmanaged":{"value":"after"}}` + "\n"))
	if _, err := validateAndRenderAggregate(effect, changed, aggregate.DocumentFileMode, testAggregateCodecs()); err == nil ||
		!strings.Contains(err.Error(), "document changed after planning") {
		t.Fatalf("document drift error = %v, want effect-time document rejection", err)
	}
	if _, err := validateAndRenderAggregate(effect, before, 0o644, testAggregateCodecs()); err == nil ||
		!strings.Contains(err.Error(), "mode changed after planning") {
		t.Fatalf("mode drift error = %v, want effect-time mode rejection", err)
	}
}

func TestAggregateEffectRechecksOperationPreconditionBeforeMutation(t *testing.T) {
	root := t.TempDir()
	placement, ok := aggregate.MCPPlacementForID(aggregate.MCPPlacementOpenCodeProject)
	if !ok {
		t.Fatal("OpenCode project MCP placement is missing")
	}
	contract, err := placement.ProjectionContract("context7")
	if err != nil {
		t.Fatalf("ProjectionContract returned error: %v", err)
	}
	preconditions, admitted, err := aggregate.OperationPreconditionsForContract(contract)
	if err != nil {
		t.Fatalf("OperationPreconditionsForContract returned error: %v", err)
	}
	if !admitted || len(preconditions) != 1 {
		t.Fatalf("preconditions = %#v, admitted = %t, want one", preconditions, admitted)
	}

	authority, err := captureMutationAuthority(
		Paths{ManifestRoot: root, DataDir: filepath.Join(root, ".daem")},
		true,
		nil,
		destinationResolver(Paths{ManifestRoot: root, DataDir: filepath.Join(root, ".daem")}),
		testFilesystem(),
	)
	if err != nil {
		t.Fatalf("captureMutationAuthority returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := authority.close(); err != nil {
			t.Errorf("close mutation authority: %v", err)
		}
	})
	for _, precondition := range preconditions {
		document := precondition.DocumentAddress()
		if err := authority.bindPhysicalAuthority(
			document.Scope(),
			document.AggregateRoot(),
			[]target.Target{document.Target()},
		); err != nil {
			t.Fatalf("bind precondition authority: %v", err)
		}
	}
	effect := AggregateEffect{preconditions: preconditions}
	if err := verifyAggregateOperationPreconditions(
		context.Background(),
		authority,
		effect,
	); err != nil {
		t.Fatalf("absent alternate config rejected: %v", err)
	}

	alternatePath := filepath.Join(root, aggregate.OpenCodeProjectMCPConfigPath+"c")
	if err := os.WriteFile(alternatePath, []byte(`{"mcp":{}}`), 0o600); err != nil {
		t.Fatalf("write alternate config after planning: %v", err)
	}
	err = verifyAggregateOperationPreconditions(context.Background(), authority, effect)
	if err == nil || !strings.Contains(err.Error(), "document_absent") {
		t.Fatalf("effect-time precondition error = %v, want document_absent rejection", err)
	}
}

func hookAggregateInsertionEffect(t *testing.T, before aggregate.Document) AggregateEffect {
	t.Helper()
	value := desiredtest.Hook(t, desiredhook.Spec{
		Name: "guard", Event: "Stop", Type: desiredhook.TypeCommand, Command: "echo guard",
		Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
	})
	lowered, err := topologyhook.Lower(nil, []desiredhook.Hook{value})
	if err != nil {
		t.Fatalf("hook topology Lower returned error: %v", err)
	}
	contracts, err := refine.HookContributions(
		[]desiredhook.Hook{value},
		lowered,
		hookcodec.CanonicalHookContribution,
	)
	if err != nil {
		t.Fatalf("HookContributions returned error: %v", err)
	}
	if len(contracts) != 1 {
		t.Fatalf("HookContributions returned %d contracts, want 1", len(contracts))
	}
	lockedContract := contracts[0]
	realization, present := lockedContract.Realization()
	if !present {
		t.Fatal("Hook contribution lock has no realization")
	}
	contribution, present := realization.ManagedAggregateContribution()
	if !present {
		t.Fatal("Hook contribution lock has no aggregate realization")
	}
	item, err := aggregate.NewSubjectContribution(lockedContract.SubjectID(), contribution)
	if err != nil {
		t.Fatalf("NewSubjectContribution returned error: %v", err)
	}
	locked, err := lock.NewLockedSection([]lock.LockedSubjectContract{lockedContract}, nil)
	if err != nil {
		t.Fatalf("NewLockedSection returned error: %v", err)
	}
	projectionSelection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
	if err != nil {
		t.Fatalf("NewSelection returned error: %v", err)
	}
	codec, present := testAggregateCodecs().Lookup(contribution.Contract().CodecContractID())
	if !present {
		t.Fatal("Hook codec is not admitted")
	}
	snapshot, failure := codec.Read(before, projectionSelection)
	if failure != nil {
		t.Fatalf("Hook codec Read returned failure: %v", failure)
	}
	evidence, err := observe.NewAggregateEvidence(before, snapshot, aggregate.DocumentFileMode)
	if err != nil {
		t.Fatalf("NewAggregateEvidence returned error: %v", err)
	}
	decisions, err := reconcileprojection.BuildAggregateDecisions(reconcileprojection.AggregateInput{
		Locked: locked, Expected: []lock.LockedSubjectContract{lockedContract},
		Desired: []aggregate.SubjectContribution{item}, Evidence: []observe.AggregateEvidence{evidence},
		SelectedTargets: testSelectedTargets(t, target.TargetCodex),
		Codecs:          testAggregateCodecs(),
	})
	if err != nil {
		t.Fatalf("BuildAggregateDecisions returned error: %v", err)
	}
	effects, err := AggregateEffects(decisions)
	if err != nil {
		t.Fatalf("AggregateEffects returned error: %v", err)
	}
	if len(effects) != 1 || effects[0].Kind() != AggregateEffectReplace {
		t.Fatalf("effects = %#v, want one document replacement", effects)
	}
	return effects[0]
}
