package publication

import (
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func backupEntryForTest(t *testing.T, before recovery.BeforePathState) journalrecord.PathEntry {
	t.Helper()
	hash := testContentHash("backup capture basis")
	destination := outputtest.Parse(t, "managed-file")
	mutation := normalizedPathCapture(t, journalrecord.CaptureRecord, target.ScopeProject, destination, hash, realization.PathProjectionFile)
	state, err := durable.NewManagedPathState(testManagedPathSubject(t, "capture"), []target.Target{target.TargetCodex}, target.ScopeProject, destination, hash, realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := rootedpath.NewAuthorityProvenance(t.TempDir(), testContentHashString("backup manifest"), testContentHashString("backup mount"))
	if err != nil {
		t.Fatal(err)
	}
	demands, err := recovery.NewRemovalDemandSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := journalrecord.NewOrdinaryDraft("backup-view", time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), statefileFor(state), statefileFor(state), provenance, []journalrecord.CaptureMutation{mutation}, demands)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.BeginEntry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyBefore(before); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyGlobalBinding(nil); err != nil {
		t.Fatal(err)
	}
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
		t.Fatal("unexpected project ownership demand", err)
	}
	evidence, err := draft.Finish()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := evidence.RecordedEntries()
	if err != nil || len(entries) != 1 {
		t.Fatal("completed backup capture lost its entry", err)
	}
	return entries[0]
}

func globalEntryForTest(t *testing.T, destination output.Destination, path string, binding *journalrecord.GlobalBinding) journalrecord.PathEntry {
	t.Helper()
	provenance, err := rootedpath.NewAuthorityProvenance(t.TempDir(), testContentHashString("global manifest"), testContentHashString("global mount"))
	if err != nil {
		t.Fatal(err)
	}
	value := normalizedPathCapture(t, journalrecord.CaptureCreate, target.ScopeGlobal, destination, "", realization.PathProjectionDirectory)
	demands, err := recovery.NewRemovalDemandSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewManagedPathState(value.Subject(), value.ConsumerTargets(), value.Scope(), value.Destination(), value.DesiredHash(), value.ContentKind(), realization.PathPermissionsNone, 0)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := journalrecord.NewOrdinaryDraft("global-view", time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), durable.EmptySnapshot(), statefileFor(state), provenance, []journalrecord.CaptureMutation{value}, demands)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.BeginEntry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyBefore(recovery.BeforePathState{}); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyGlobalBinding(binding); err != nil {
		t.Fatal(err)
	}
	if err := draft.CloseEntries(); err != nil {
		t.Fatal(err)
	}
	if err := draft.SupplyRemovalIntents(nil); err != nil {
		t.Fatal(err)
	}
	observation, err := mutation.ObserveDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := documentClaimsForTest(t).owner
	var transitions []ownershipmutation.ClaimTransition
	var intents []ownership.ProvisionalAcquireIntent
	if exact, present := observation.Exact(); present {
		address, err := ownership.NewManagedAddressFromLegacy(exact, "")
		if err != nil {
			t.Fatal(err)
		}
		transition, err := ownershipmutation.NewAcquireTransition(address, owner, "global-view")
		if err != nil {
			t.Fatal(err)
		}
		transitions = []ownershipmutation.ClaimTransition{transition}
	} else {
		provisional, present := observation.Provisional()
		if !present {
			t.Fatal("global capture has no path observation")
		}
		intent, err := ownership.NewProvisionalAcquireIntentFromLegacy(destination, "", provisional, owner, "global-view")
		if err != nil {
			t.Fatal(err)
		}
		intents = []ownership.ProvisionalAcquireIntent{intent}
	}
	if err := draft.BeginClaimCoverage(transitions, intents); err != nil {
		t.Fatal(err)
	}
	if _, present, err := draft.NextClaimAuthority(t.Context()); err != nil {
		t.Fatal(err)
	} else if present {
		if err := draft.BindClaimAuthority(observation); err != nil {
			t.Fatal(err)
		}
		if _, present, err := draft.NextClaimAuthority(t.Context()); err != nil || present {
			t.Fatal("global capture has another authority demand", err)
		}
	}
	evidence, err := draft.Finish()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := evidence.RecordedEntries()
	if err != nil || len(entries) != 1 {
		t.Fatal("global capture lost its entry", err)
	}
	return entries[0]
}
