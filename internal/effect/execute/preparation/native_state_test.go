package preparation

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestNativeApplyStatePreservesOrdinaryRelocationInputs(t *testing.T) {
	previous := testManagedPathEffectStateAt(t, "oracle", "skill.project.opencode", []target.Target{target.TargetOpenCode}, outputtest.Parse(t, ".opencode/skills/oracle"))
	subject := testManagedPathEffectSubject(t, "oracle", "skill.project.agents")
	decision, err := reconcile.NewManagedPathDecision(reconcile.ManagedPathDecisionInput{
		Kind: reconcile.ManagedPathReplace, Subject: subject, ConsumerTargets: previous.ConsumerTargets(), Scope: previous.Scope(),
		Destination: outputtest.Parse(t, ".agents/skills/oracle"), DesiredHash: testArtifactHash("new"),
		ContentKind: previous.ContentKind(), PermissionPolicy: previous.PermissionPolicy(), Previous: &previous, Reason: reconcile.ReasonStateStale,
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{previous}})
	if err != nil {
		t.Fatal(err)
	}
	input := NativeApplyStateInput{Before: before, ManagedPaths: []reconcile.ManagedPathDecision{decision}, StateCodec: statefile.Codec{}}
	prepared, err := PrepareNativeApplyState(t.Context(), input)
	if err != nil || prepared.Validate() != nil {
		t.Fatal("exact ordinary relocation was refused by the shared preflight", err)
	}
	rows := prepared.AfterState().ManagedPaths()
	if len(rows) != 1 || rows[0].Subject() != subject || rows[0].Destination() != decision.Destination() || rows[0].ContentHash() != decision.DesiredHash() || len(prepared.ManagedPathEffects()) != 1 {
		t.Fatal("ordinary relocation lost its exact effect/state relation")
	}
	occupied, err := durable.NewManagedPathState(subject, previous.ConsumerTargets(), previous.Scope(), decision.Destination(), previous.ContentHash(), previous.ContentKind(), previous.PermissionPolicy(), previous.FileMode())
	if err != nil {
		t.Fatal(err)
	}
	input.Before, err = before.WithManagedPaths([]durable.ManagedPathState{previous, occupied})
	if err != nil {
		t.Fatal(err)
	}
	if prepared, err := PrepareNativeApplyState(t.Context(), input); err == nil || prepared.Validate() == nil {
		t.Fatal("relocation overwrote another unselected prior row")
	}
}

func TestNativeApplyStateRefusesOrdinaryNoPriorOverwrite(t *testing.T) {
	previous := testManagedPathEffectState(t, "oracle", outputtest.Parse(t, ".agents/skills/oracle"))
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{previous}})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := reconcile.NewManagedPathDecision(reconcile.ManagedPathDecisionInput{
		Kind: reconcile.ManagedPathCreate, Subject: previous.Subject(), ConsumerTargets: previous.ConsumerTargets(), Scope: previous.Scope(),
		Destination: previous.Destination(), DesiredHash: testArtifactHash("new"), ContentKind: previous.ContentKind(),
		PermissionPolicy: previous.PermissionPolicy(), Reason: reconcile.ReasonStateStale,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := NativeApplyStateInput{Before: before, ManagedPaths: []reconcile.ManagedPathDecision{decision}, StateCodec: statefile.Codec{}}
	if prepared, err := PrepareNativeApplyState(t.Context(), input); err == nil || prepared.Validate() == nil {
		t.Fatal("ordinary no-prior create silently replaced selected before ownership")
	}
	input.Before = durable.EmptySnapshot()
	prepared, err := PrepareNativeApplyState(t.Context(), input)
	if err != nil || prepared.Validate() != nil {
		t.Fatal("ordinary creation without prior ownership was refused", err)
	}
	if rows := prepared.AfterState().ManagedPaths(); len(rows) != 1 || rows[0].Subject() != previous.Subject() {
		t.Fatal("ordinary creation did not derive its own canonical state")
	}
}
