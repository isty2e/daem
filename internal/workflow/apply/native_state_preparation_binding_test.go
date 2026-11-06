package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	projectionbuild "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestNativeApplyStateBindsAllPathEffectsToExactBefore(t *testing.T) {
	_, adoption, _ := nativeStateAdoptionFixture(t, `{"a":1,"b":2}`)
	adopted, err := effectpreparation.PrepareNativeApplyState(t.Context(), adoption)
	if err != nil {
		t.Fatal(err)
	}
	baseline := adopted.AfterState().ManagedPaths()[0]
	remove, err := reconcile.NewManagedPathDecision(reconcile.ManagedPathDecisionInput{
		Kind: reconcile.ManagedPathRemove, Subject: baseline.Subject(), Scope: baseline.Scope(), Destination: baseline.Destination(),
		ContentKind: baseline.ContentKind(), PermissionPolicy: baseline.PermissionPolicy(), LiveHash: baseline.ContentHash(),
		LiveFileMode: baseline.FileMode(), Previous: &baseline, Reason: reconcile.ReasonRemovedFromManifest,
	})
	if err != nil {
		t.Fatal(err)
	}
	valid := effectpreparation.NativeApplyStateInput{Before: adopted.AfterState(), ManagedPaths: []reconcile.ManagedPathDecision{remove}, StateCodec: adoption.StateCodec}
	prepared, err := effectpreparation.PrepareNativeApplyState(t.Context(), valid)
	if err != nil || !prepared.AfterState().Equal(durable.EmptySnapshot()) || len(prepared.ManagedPathEffects()) != 1 {
		t.Fatal("exact prior whole removal was not preserved", err)
	}
	changed, err := durable.NewManagedPathState(baseline.Subject(), baseline.ConsumerTargets(), baseline.Scope(), baseline.Destination(),
		artifact.HashFileContent([]byte(`{"different":true}`)), baseline.ContentKind(), baseline.PermissionPolicy(), baseline.FileMode())
	if err != nil {
		t.Fatal(err)
	}
	stale, err := valid.Before.WithManagedPaths([]durable.ManagedPathState{changed})
	if err != nil {
		t.Fatal(err)
	}
	recordInput := nativeStateDecisionInput(adoption.ManagedPaths[0])
	recordInput.Previous, recordInput.Reason = &baseline, reconcile.ReasonStateStale
	record, err := reconcile.NewManagedPathDecision(recordInput)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		before  durable.Snapshot
		effects []reconcile.ManagedPathDecision
	}{
		{"absent prior", durable.EmptySnapshot(), []reconcile.ManagedPathDecision{remove}},
		{"stale prior", stale, []reconcile.ManagedPathDecision{remove}},
		{"keyed document", adoption.Before, []reconcile.ManagedPathDecision{remove}},
		{"duplicate remove", valid.Before, []reconcile.ManagedPathDecision{remove, remove}},
		{"remove then record", valid.Before, []reconcile.ManagedPathDecision{remove, record}},
		{"record then remove", valid.Before, []reconcile.ManagedPathDecision{record, remove}},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			candidate.Before, candidate.ManagedPaths, candidate.WholeSources = test.before, test.effects, adoption.WholeSources
			if prepared, err := effectpreparation.PrepareNativeApplyState(t.Context(), candidate); err == nil || prepared.Validate() == nil {
				t.Fatal("misbound or multiply consumed path state exposed a preparation")
			}
		})
	}
}

func nativeOrdinaryAggregateFacts(t *testing.T, command string) (reconcile.AggregateDecision, reconcile.AggregateDecision, durable.Snapshot) {
	t.Helper()
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	writeApplyFile(t, manifestPath, fmt.Sprintf(`version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = %q
args = ["-y", "@upstash/context7-mcp"]
`, command))
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatal(err)
	}
	planned, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatal(err)
	}
	if len(planned.Reconciliation.Aggregates()) != 1 {
		t.Fatal("actual MCP fixture did not produce a document decision")
	}
	create := planned.Reconciliation.Aggregates()[0]
	var rows []durable.ManagedAggregateState
	var contracts []aggregate.ProjectionContract
	for _, item := range create.DesiredContributions() {
		state, err := durable.NewManagedAggregateState(item.SubjectID(), item.Contribution())
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, state)
		contracts = append(contracts, item.Contribution().Contract())
	}
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: rows})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	catalog := aggregatecodec.Catalog()
	codec, present := catalog.Lookup(create.CodecContractID())
	if !present {
		t.Fatal("actual MCP codec is unavailable")
	}
	document := create.Rendered().Document()
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	evidence, err := observe.NewAggregateEvidence(document, snapshot, aggregate.DocumentFileMode)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := lock.NewLockedSection(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := reconcile.NewSelectedTargets([]target.Target{target.TargetClaudeCode})
	if err != nil {
		t.Fatal(err)
	}
	remove, err := projectionbuild.BuildAggregateDecisions(projectionbuild.AggregateInput{
		Locked: locked, States: rows, Evidence: []observe.AggregateEvidence{evidence}, SelectedTargets: selected, Codecs: catalog,
	})
	if err != nil || len(remove) != 1 || remove[0].IsBlocked() || !remove[0].MutatesHost() {
		t.Fatal("actual MCP before/absence did not produce a removing decision", err)
	}
	if effects, err := effectpreparation.AggregateEffects(remove); err != nil || len(effects) != 1 {
		t.Fatal("fixture removal is not an admitted ordinary effect", err)
	}
	return create, remove[0], before
}

func TestNativeApplyStateBindsAllAggregateEffectsToExactBefore(t *testing.T) {
	create, remove, before := nativeOrdinaryAggregateFacts(t, "npx")
	_, _, stale := nativeOrdinaryAggregateFacts(t, "different-launcher")
	codec, err := statefile.NewNativeCodec(aggregatecodec.Catalog().Contributions())
	if err != nil {
		t.Fatal(err)
	}
	valid := effectpreparation.NativeApplyStateInput{Before: before, Aggregates: []reconcile.AggregateDecision{remove}, StateCodec: codec}
	prepared, err := effectpreparation.PrepareNativeApplyState(t.Context(), valid)
	if err != nil || !prepared.AfterState().Equal(durable.EmptySnapshot()) || len(prepared.AggregateEffects()) != 1 {
		t.Fatal("exact prior ordinary aggregate removal was not preserved", err)
	}
	for _, test := range []struct {
		name    string
		before  durable.Snapshot
		effects []reconcile.AggregateDecision
	}{
		{"absent prior", durable.EmptySnapshot(), []reconcile.AggregateDecision{remove}},
		{"stale prior", stale, []reconcile.AggregateDecision{remove}},
		{"duplicate remove", before, []reconcile.AggregateDecision{remove, remove}},
		{"no-prior replacement", before, []reconcile.AggregateDecision{create}},
		{"remove then create", before, []reconcile.AggregateDecision{remove, create}},
		{"create then remove", before, []reconcile.AggregateDecision{create, remove}},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			candidate.Before, candidate.Aggregates = test.before, test.effects
			if prepared, err := effectpreparation.PrepareNativeApplyState(t.Context(), candidate); err == nil || prepared.Validate() == nil {
				t.Fatal("misbound or multiply consumed aggregate state exposed a preparation")
			}
		})
	}
}

func TestNativeApplyStateWholeRecordMatchesSourceMode(t *testing.T) {
	_, input, hostPath := nativeStateAdoptionFixture(t, `{"a":1,"b":2}`)
	if err := os.Chmod(hostPath, 0o644); err != nil {
		t.Fatal(err)
	}
	facts := nativeStateDecisionInput(input.ManagedPaths[0])
	facts.DesiredFileMode, facts.LiveFileMode = 0o644, 0o644
	record, err := reconcile.NewManagedPathDecision(facts)
	if err != nil {
		t.Fatal(err)
	}
	input.ManagedPaths = []reconcile.ManagedPathDecision{record}
	if prepared, err := effectpreparation.PrepareNativeApplyState(t.Context(), input); err == nil || prepared.Validate() == nil {
		t.Fatal("self-consistent record modes retired keys despite mismatching source mode")
	}
	info, err := os.Stat(hostPath)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatal("pure refusal modified the observed mode", err)
	}
}
