package apply

import (
	"path/filepath"
	"testing"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/reconcile"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func TestNativeApplyStateRejectsNarrowingWithWholeDeletion(t *testing.T) {
	input := nativeActionPreparationInput(t, configdocument.JSON, `{"a":1}`, `{"a":1}`, `{}`, hostconfig.OwnershipKeys)
	action, err := readiness.BuildNativeNarrowingAction(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	baseline := input.Baseline
	remove, err := reconcile.NewManagedPathDecision(reconcile.ManagedPathDecisionInput{
		Kind: reconcile.ManagedPathRemove, Subject: baseline.Subject(), Scope: baseline.Scope(), Destination: baseline.Destination(),
		ContentKind: baseline.ContentKind(), PermissionPolicy: baseline.PermissionPolicy(), LiveHash: baseline.ContentHash(),
		LiveFileMode: baseline.FileMode(), Previous: &baseline, Reason: reconcile.ReasonRemovedFromManifest,
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{baseline}})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := effectpreparation.PrepareNativeApplyState(t.Context(), effectpreparation.NativeApplyStateInput{
		Before: before, Narrowings: []reconcile.NativeNarrowingAction{action}, ManagedPaths: []reconcile.ManagedPathDecision{remove}, StateCodec: nativePreparationStateCodec(t),
	})
	if err == nil || prepared.Validate() == nil {
		t.Fatal("empty-scope ownership release accepted an overlapping whole-file deletion")
	}
}

func TestNativeApplyStatePreservesIndependentAggregateWork(t *testing.T) {
	_, input, _ := nativeStateAdoptionFixture(t, `{"a":1,"b":2}`)
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	writeApplyFile(t, manifestPath, `version = 1
targets = ["claude-code"]

[[mcp_server]]
name = "context7"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp"]
`)
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatal(err)
	}
	planned, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatal(err)
	}
	input.Aggregates = planned.Reconciliation.Aggregates()
	if len(input.Aggregates) != 1 || !input.Aggregates[0].MutatesHost() {
		t.Fatal("actual MCP workflow did not produce independent aggregate work")
	}
	nativeID, _ := aggregate.HostConfigCodecForSyntax("json")
	catalog, err := aggregatecodec.ForContributions(t.Context(), []aggregate.CodecContractID{nativeID, input.Aggregates[0].CodecContractID()}, narrowingLimits)
	if err != nil {
		t.Fatal(err)
	}
	input.StateCodec, err = statefile.NewNativeCodec(catalog)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := effectpreparation.PrepareNativeApplyState(t.Context(), input)
	if err != nil || prepared.Validate() != nil {
		t.Fatal("independent native aggregate work was confused with adoption retirement", err)
	}
	if len(prepared.AggregateEffects()) != 1 || len(prepared.AfterState().ManagedAggregates()) != 1 || len(prepared.AfterState().ManagedPaths()) != 1 {
		t.Fatal("joint fold lost independent aggregate work or adoption state")
	}
	copied := prepared.AggregateEffects()
	copied[0] = effectpreparation.AggregateEffect{}
	if len(prepared.AggregateEffects()[0].DesiredContributions()) != 1 {
		t.Fatal("caller changed the prepared aggregate membership")
	}
	managed := prepared.ManagedPathEffects()
	managed[0] = effectpreparation.ManagedPathEffect{}
	if prepared.ManagedPathEffects()[0].Kind() != effectpreparation.ManagedPathEffectRecord {
		t.Fatal("caller changed the prepared managed-path membership")
	}
}
