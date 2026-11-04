package record

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
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
	mutations, err := NormalizeCaptureMutations([]ManagedPathMutation{mutation}, nil, []observe.ManagedPathEvidence{evidence})
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

func closeOrdinaryRecordDraft(t *testing.T, draft *OrdinaryDraft) OrdinaryEvidence {
	t.Helper()
	if err := draft.CloseEntries(); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyRemovalIntents(nil); err != nil {
		t.Fatal(err)
	}
	if err := draft.BeginClaimCoverage(nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, present, err := draft.NextClaimAuthority(t.Context()); err != nil || present {
		t.Fatalf("unexpected project claim demand: %v", err)
	}
	material, err := draft.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return material
}

func TestOrdinaryRecordDraftRejectsIncompleteAndReorderedEvidence(t *testing.T) {
	for _, action := range []string{"zero", "finish", "entry missing", "before missing", "binding missing", "binding scope", "double entry", "double before", "double binding", "removal early", "claims early", "canceled"} {
		t.Run(action, func(t *testing.T) {
			input, before := ordinaryRecordDraftInput(t)
			draft, err := NewOrdinaryDraft(input.operationID, input.createdAt, input.before, input.after, input.manifest, input.mutations, input.removalDemands)
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case "zero":
				draft = &OrdinaryDraft{}
				_, err = draft.BeginEntry(t.Context())
			case "finish":
				_, err = draft.Finish()
			case "entry missing":
				err = draft.CloseEntries()
			case "before missing":
				err = draft.SupplyBefore(before)
			case "binding missing":
				err = draft.SupplyGlobalBinding(nil)
			case "binding scope":
				if _, err := draft.BeginEntry(t.Context()); err != nil {
					t.Fatal(err)
				}
				if err := draft.SupplyBefore(before); err != nil {
					t.Fatal(err)
				}
				err = draft.SupplyGlobalBinding(&GlobalBinding{})
			case "double entry", "double before", "double binding":
				if _, err := draft.BeginEntry(t.Context()); err != nil {
					t.Fatal(err)
				}
				if action == "double entry" {
					_, err = draft.BeginEntry(t.Context())
					break
				}
				if err := draft.SupplyBefore(before); err != nil {
					t.Fatal(err)
				}
				if action == "double before" {
					err = draft.SupplyBefore(before)
					break
				}
				if err := draft.SupplyGlobalBinding(nil); err != nil {
					t.Fatal(err)
				}
				err = draft.SupplyGlobalBinding(nil)
			case "removal early":
				err = draft.SupplyRemovalIntents(nil)
			case "claims early":
				err = draft.BeginClaimCoverage(nil, nil)
			case "canceled":
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				_, err = draft.BeginEntry(ctx)
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("lost cancellation: %v", err)
				}
			}
			if err == nil {
				t.Fatal("invalid construction order was accepted")
			}
			if material, err := draft.Finish(); err == nil || material.operationID != "" || len(material.entries) != 0 {
				t.Fatal("failed draft returned usable record facts")
			}
		})
	}
}

func TestOrdinaryRecordDraftRequiresCompleteGlobalBindingBeforeAppend(t *testing.T) {
	input, before := ordinaryRecordDraftInput(t)
	previous := input.before.ManagedPaths()[0]
	destination, err := output.Parse("~/.codex/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewManagedPathState(previous.Subject(), previous.ConsumerTargets(), target.ScopeGlobal, destination, previous.ContentHash(), previous.ContentKind(), realization.PathPermissionsExact, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := NewManagedPathRecordMutation(state.Subject(), state.ConsumerTargets(), state.Scope(), destination, state.ContentHash(), state.ContentHash(), state.ContentKind(), 0o600, &state)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := observe.NewManagedPathEvidence(state.Subject(), destination, true, state.ContentHash(), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	input.mutations, err = NormalizeCaptureMutations([]ManagedPathMutation{mutation}, nil, []observe.ManagedPathEvidence{evidence})
	if err != nil {
		t.Fatal(err)
	}
	input.before, input.after = statefileFor(state), statefileFor(state)
	provenance := input.manifest
	path := filepath.Join(provenance.PhysicalRoot(), ".codex", "AGENTS.md")

	for _, variant := range []string{"complete", "empty path", "zero provenance", "zero value"} {
		t.Run(variant, func(t *testing.T) {
			draft, err := NewOrdinaryDraft(input.operationID, input.createdAt, input.before, input.after, input.manifest, input.mutations, input.removalDemands)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := draft.BeginEntry(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := draft.SupplyBefore(before); err != nil {
				t.Fatal(err)
			}
			binding := NewGlobalBinding(path, provenance)
			switch variant {
			case "empty path":
				binding = NewGlobalBinding("", provenance)
			case "zero provenance":
				binding = NewGlobalBinding(path, rootedpath.AuthorityProvenance{})
			case "zero value":
				binding = &GlobalBinding{}
			}
			err = draft.SupplyGlobalBinding(binding)
			if variant == "complete" {
				if err != nil {
					t.Fatal(err)
				}
				if len(draft.entries) != 1 || draft.entries[0].GlobalPathBinding == nil || draft.entries[0].GlobalPathBinding.ResolvedPath != path {
					t.Fatal("complete binding was not retained")
				}
				return
			}
			if err == nil || len(draft.entries) != 0 {
				t.Fatal("incomplete binding appended an entry")
			}
			if material, err := draft.Finish(); err == nil || material.operationID != "" || len(material.entries) != 0 {
				t.Fatal("failed binding returned partial record facts")
			}
		})
	}
}

func TestOrdinaryRecordDraftRejectsUninitializedCaptureInput(t *testing.T) {
	input, _ := ordinaryRecordDraftInput(t)
	input.mutations = []CaptureMutation{{}}
	if draft, err := NewOrdinaryDraft(input.operationID, input.createdAt, input.before, input.after, input.manifest, input.mutations, input.removalDemands); err == nil || draft != nil {
		t.Fatal("uninitialized capture input produced a usable record draft")
	}
}

func TestOrdinaryRecordDraftFreezesCaptureInputsAndBeforeMode(t *testing.T) {
	input, before := ordinaryRecordDraftInput(t)
	original := input.mutations[0]
	draft, err := NewOrdinaryDraft(input.operationID, input.createdAt, input.before, input.after, input.manifest, input.mutations, input.removalDemands)
	if err != nil {
		t.Fatal(err)
	}
	input.mutations[0].facts.ConsumerTargets[0] = target.TargetClaudeCode
	input.mutations[0].facts.PreviousState.ContentHash = "mutated"
	input.mutations[0].facts.PreviousState.ConsumerTargets[0] = target.TargetClaudeCode
	action, err := draft.BeginEntry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if action.facts.Subject != original.facts.Subject || action.facts.ConsumerTargets[0] != target.TargetCodex || action.facts.PreviousState.ContentHash != original.facts.DesiredHash {
		t.Fatal("draft retained mutable caller input")
	}
	action.facts.ConsumerTargets[0], action.facts.PreviousState.ContentHash = target.TargetClaudeCode, "mutated again"
	if err := draft.SupplyBefore(before); err != nil {
		t.Fatal(err)
	}
	*before.PathMode = 0
	if err := draft.SupplyGlobalBinding(nil); err != nil {
		t.Fatal(err)
	}
	material := closeOrdinaryRecordDraft(t, draft)
	if len(material.entries) != 1 || len(material.entries[0].Targets) != 1 || material.entries[0].Targets[0] != string(target.TargetCodex) || material.entries[0].Before.PathMode == nil || *material.entries[0].Before.PathMode != 0o600 {
		t.Fatal("completed record lost frozen consumers or before mode")
	}
	if !material.before.Equal(input.before) || !material.after.Equal(input.after) {
		t.Fatal("record construction changed its complete snapshots")
	}
	if _, err := draft.Finish(); err == nil {
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
	draft, err := NewOrdinaryDraft(input.operationID, input.createdAt, input.before, input.after, input.manifest, input.mutations, input.removalDemands)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.BeginEntry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyBefore(before); err == nil || !strings.Contains(err.Error(), "action previous state hash") {
		t.Fatalf("state error did not precede expected/binding derivation: %v", err)
	}
	if material, err := draft.Finish(); err == nil || len(material.entries) != 0 {
		t.Fatal("failed state join retained a partial record")
	}
}

func TestOrdinaryRecordDraftRetainsNativeSnapshotsWithoutAdmittingOrdinary15(t *testing.T) {
	fixture := nativeJournalFixture(t)
	input, _ := ordinaryRecordDraftInput(t)
	input.mutations, input.before, input.after = nil, fixture.before, fixture.after
	draft, err := NewOrdinaryDraft(input.operationID, input.createdAt, input.before, input.after, input.manifest, input.mutations, input.removalDemands)
	if err != nil {
		t.Fatal("ordinary facts prematurely rejected a containing native family", err)
	}
	material := closeOrdinaryRecordDraft(t, draft)
	if !material.before.Equal(fixture.before) || !material.after.Equal(fixture.after) {
		t.Fatal("ordinary material dropped native snapshot members")
	}
	before, after, err := EncodeSnapshots(fixture.before, fixture.after, fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	if content, err := material.EncodeOrdinary(before, after); err == nil || len(content) != 0 {
		t.Fatal("family-neutral completion widened ordinary15 admission")
	}
}
