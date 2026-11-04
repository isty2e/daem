package journal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func ordinaryRecordDraftInput(t *testing.T) (ordinaryRecordCaptureInput, recovery.BeforePathState) {
	t.Helper()
	hash := artifact.HashFileContent([]byte("recorded output\n"))
	entry := recoveryEntryFor("recorded", "AGENTS.md", string(hash), string(hash), "files/000001")
	state := resourceState(entry, string(hash))
	mutation, err := NewManagedPathRecordMutation(state.Subject(), state.ConsumerTargets(), state.Scope(), state.Destination(), hash, hash, state.ContentKind(), 0o600, &state)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(state.Subject(), state.Destination(), true, hash, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	mutations, err := pathMutations([]ManagedPathMutation{mutation}, nil, []observe.ManagedPathEvidence{evidence})
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := testRecoveryManifestRootProvenance().canonical()
	if err != nil {
		t.Fatal(err)
	}
	before := entry.Before.canonical()
	return ordinaryRecordCaptureInput{
		operationID: "record-draft", createdAt: time.Date(2026, time.November, 4, 0, 0, 0, 0, time.UTC),
		before: statefileFor(state), after: statefileFor(state), manifest: provenance, mutations: mutations,
	}, before
}

func closeOrdinaryRecordDraft(t *testing.T, draft *ordinaryRecordDraft) ordinaryCaptureMaterial {
	t.Helper()
	if err := draft.closeEntries(); err != nil {
		t.Fatal(err)
	}
	if err := draft.supplyRemovalIntents(nil); err != nil {
		t.Fatal(err)
	}
	if err := draft.beginClaimCoverage(nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, present, err := draft.nextClaimAuthority(t.Context()); err != nil || present {
		t.Fatalf("unexpected project claim demand: %v", err)
	}
	material, err := draft.finish()
	if err != nil {
		t.Fatal(err)
	}
	return material
}

func TestOrdinaryRecordDraftRejectsIncompleteAndReorderedEvidence(t *testing.T) {
	for _, action := range []string{"zero", "finish", "entry missing", "before missing", "binding missing", "binding scope", "double entry", "double before", "double binding", "removal early", "claims early", "canceled"} {
		t.Run(action, func(t *testing.T) {
			input, before := ordinaryRecordDraftInput(t)
			draft, err := newOrdinaryRecordDraft(input)
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case "zero":
				draft = &ordinaryRecordDraft{}
				_, err = draft.beginEntry(t.Context())
			case "finish":
				_, err = draft.finish()
			case "entry missing":
				err = draft.closeEntries()
			case "before missing":
				err = draft.supplyBefore(before)
			case "binding missing":
				err = draft.supplyGlobalBinding(nil)
			case "binding scope":
				if _, err := draft.beginEntry(t.Context()); err != nil {
					t.Fatal(err)
				}
				if err := draft.supplyBefore(before); err != nil {
					t.Fatal(err)
				}
				err = draft.supplyGlobalBinding(&capturedGlobalBinding{})
			case "double entry", "double before", "double binding":
				if _, err := draft.beginEntry(t.Context()); err != nil {
					t.Fatal(err)
				}
				if action == "double entry" {
					_, err = draft.beginEntry(t.Context())
					break
				}
				if err := draft.supplyBefore(before); err != nil {
					t.Fatal(err)
				}
				if action == "double before" {
					err = draft.supplyBefore(before)
					break
				}
				if err := draft.supplyGlobalBinding(nil); err != nil {
					t.Fatal(err)
				}
				err = draft.supplyGlobalBinding(nil)
			case "removal early":
				err = draft.supplyRemovalIntents(nil)
			case "claims early":
				err = draft.beginClaimCoverage(nil, nil)
			case "canceled":
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				_, err = draft.beginEntry(ctx)
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("lost cancellation: %v", err)
				}
			}
			if err == nil {
				t.Fatal("invalid construction order was accepted")
			}
			if material, err := draft.finish(); err == nil || material.operationID != "" || len(material.entries) != 0 {
				t.Fatal("failed draft returned usable record facts")
			}
		})
	}
}

func TestOrdinaryRecordDraftRejectsUninitializedCaptureInput(t *testing.T) {
	input, _ := ordinaryRecordDraftInput(t)
	input.mutations = []captureMutation{{}}
	if draft, err := newOrdinaryRecordDraft(input); err == nil || draft != nil {
		t.Fatal("uninitialized capture input produced a usable record draft")
	}
}

func TestOrdinaryRecordDraftFreezesCaptureInputsAndBeforeMode(t *testing.T) {
	input, before := ordinaryRecordDraftInput(t)
	original := input.mutations[0]
	draft, err := newOrdinaryRecordDraft(input)
	if err != nil {
		t.Fatal(err)
	}
	input.mutations[0].facts.ConsumerTargets[0] = target.TargetClaudeCode
	input.mutations[0].facts.PreviousState.ContentHash = "mutated"
	input.mutations[0].facts.PreviousState.ConsumerTargets[0] = target.TargetClaudeCode
	action, err := draft.beginEntry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if action.facts.Subject != original.facts.Subject || action.facts.ConsumerTargets[0] != target.TargetCodex || action.facts.PreviousState.ContentHash != original.facts.DesiredHash {
		t.Fatal("draft retained mutable caller input")
	}
	action.facts.ConsumerTargets[0], action.facts.PreviousState.ContentHash = target.TargetClaudeCode, "mutated again"
	if err := draft.supplyBefore(before); err != nil {
		t.Fatal(err)
	}
	*before.PathMode = 0
	if err := draft.supplyGlobalBinding(nil); err != nil {
		t.Fatal(err)
	}
	material := closeOrdinaryRecordDraft(t, draft)
	if len(material.entries) != 1 || len(material.entries[0].Targets) != 1 || material.entries[0].Targets[0] != string(target.TargetCodex) || material.entries[0].Before.PathMode == nil || *material.entries[0].Before.PathMode != 0o600 {
		t.Fatal("completed record lost frozen consumers or before mode")
	}
	if !material.before.Equal(input.before) || !material.after.Equal(input.after) {
		t.Fatal("record construction changed its complete snapshots")
	}
	if _, err := draft.finish(); err == nil {
		t.Fatal("record completion was replayed")
	}

	contract, present := claudeRecoveryBaselineMutation(t, target.ScopeProject, "copy").AggregateContract()
	if !present {
		t.Fatal("aggregate fixture has no contract")
	}
	cloned := cloneCaptureMutation(pathMutation{AggregateContract: &contract})
	contract = aggregate.ProjectionContract{}
	if cloned.AggregateContract == nil || cloned.AggregateContract.Validate() != nil {
		t.Fatal("capture mutation retained its caller's contract pointer")
	}
}

func TestOrdinaryRecordDraftPreservesStateErrorBeforeExpectedOrBinding(t *testing.T) {
	input, before := ordinaryRecordDraftInput(t)
	input.mutations[0].facts.PreviousState.ContentHash = "wrong previous hash"
	input.mutations[0].facts.DesiredHash = ""
	draft, err := newOrdinaryRecordDraft(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.beginEntry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := draft.supplyBefore(before); err == nil || !strings.Contains(err.Error(), "action previous state hash") {
		t.Fatalf("state error did not precede expected/binding derivation: %v", err)
	}
	if material, err := draft.finish(); err == nil || len(material.entries) != 0 {
		t.Fatal("failed state join retained a partial record")
	}
}

func TestOrdinaryRecordDraftRetainsNativeSnapshotsWithoutAdmittingOrdinary15(t *testing.T) {
	fixture := nativeJournalFixture(t)
	input, _ := ordinaryRecordDraftInput(t)
	input.mutations, input.before, input.after = nil, fixture.before, fixture.after
	draft, err := newOrdinaryRecordDraft(input)
	if err != nil {
		t.Fatal("ordinary facts prematurely rejected a containing native family", err)
	}
	material := closeOrdinaryRecordDraft(t, draft)
	if !material.before.Equal(fixture.before) || !material.after.Equal(fixture.after) {
		t.Fatal("ordinary material dropped native snapshot members")
	}
	before, after, err := encodeRecoveryJournalSnapshots(fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	if content, err := material.encodeOrdinary(before, after); err == nil || len(content) != 0 {
		t.Fatal("family-neutral completion widened ordinary15 admission")
	}
}
