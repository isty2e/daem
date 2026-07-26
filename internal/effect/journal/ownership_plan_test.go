package journal

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestValidateRecoveryClaimCoverageRequiresExactGlobalBijection(t *testing.T) {
	entry := globalAcquireRecoveryEntry(t)
	acquire, _, active := testAcquireTransition(t, entry)
	resolver := func(destination output.Destination) (string, error) {
		return "/tmp/daem-global-config.json", nil
	}
	if err := validateRecoveryClaimCoverage([]recoveryEntry{entry}, nil, resolver); err == nil || !strings.Contains(err.Error(), "has no exact ownership transition") {
		t.Fatalf("missing transition error = %v", err)
	}
	if err := validateRecoveryClaimCoverage([]recoveryEntry{entry}, []ownershipmutation.ClaimTransition{acquire}, nil); err == nil ||
		!strings.Contains(err.Error(), "destination resolver is required") {
		t.Fatalf("missing resolver error = %v", err)
	}
	if err := validateRecoveryClaimCoverage([]recoveryEntry{entry}, []ownershipmutation.ClaimTransition{acquire}, resolver); err != nil {
		t.Fatalf("exact acquire coverage returned error: %v", err)
	}
	release, err := ownershipmutation.NewReleaseTransition(active)
	if err != nil {
		t.Fatalf("NewReleaseTransition returned error: %v", err)
	}
	if err := validateRecoveryClaimCoverage([]recoveryEntry{entry}, []ownershipmutation.ClaimTransition{release}, resolver); err == nil || !strings.Contains(err.Error(), "requires acquire") {
		t.Fatalf("wrong transition error = %v", err)
	}
	project := defaultRecoveryEntry()
	if err := validateRecoveryClaimCoverage([]recoveryEntry{project}, nil, nil); err != nil {
		t.Fatalf("project-only coverage required a global destination resolver: %v", err)
	}
	if err := validateRecoveryClaimCoverage([]recoveryEntry{project}, []ownershipmutation.ClaimTransition{acquire}, resolver); err == nil || !strings.Contains(err.Error(), "without a global output entry") {
		t.Fatalf("extra transition error = %v", err)
	}
}

func TestRecoveryClaimTransitionsRejectsZeroTransition(t *testing.T) {
	if _, err := recoveryClaimTransitions([]ownershipmutation.ClaimTransition{{}}); err == nil ||
		!strings.Contains(err.Error(), "unsupported ownership transition kind") {
		t.Fatalf("zero transition error = %v", err)
	}
}

func TestBuildRecoveryPlanClassifiesAcquireClaimPhases(t *testing.T) {
	entry := globalAcquireRecoveryEntry(t)
	journal := recoveryJournalFor(entry)
	transition, reserved, active := testAcquireTransition(t, entry)

	tests := []struct {
		name           string
		state          durable.Snapshot
		path           recoveryPathObservation
		registry       ownership.Registry
		classification recovery.Classification
		actionKind     recovery.ActionKind
	}{
		{
			name:  "before reservation",
			state: journal.StatefileBefore, path: ownershipBeforePathObservation(entry),
			registry: ownership.EmptyRegistry(), classification: recovery.ClassificationCleanBefore, actionKind: recovery.ActionKindCleanup,
		},
		{
			name:  "reserved after host and state commit",
			state: journal.StatefileAfter, path: ownershipAfterPathObservation(entry),
			registry: mustRegistry(t, reserved), classification: recovery.ClassificationNeedsFinalize, actionKind: recovery.ActionKindFinalizeClaims,
		},
		{
			name:  "active after commit",
			state: journal.StatefileAfter, path: ownershipAfterPathObservation(entry),
			registry: mustRegistry(t, active), classification: recovery.ClassificationCleanAfter, actionKind: recovery.ActionKindCleanup,
		},
		{
			name:  "reserved after host effect before state commit",
			state: journal.StatefileBefore, path: ownershipAfterPathObservation(entry),
			registry: mustRegistry(t, reserved), classification: recovery.ClassificationNeedsRollback, actionKind: recovery.ActionKindRestoreDelete,
		},
		{
			name:  "reserved before first host effect",
			state: journal.StatefileBefore, path: ownershipBeforePathObservation(entry),
			registry: mustRegistry(t, reserved), classification: recovery.ClassificationNeedsRollback, actionKind: recovery.ActionKindNoOp,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, err := buildRecoveryPlan(
				testOperationID, testOperationDir, journal, test.state,
				[]recoveryPathObservation{test.path}, nil,
				[]ownershipmutation.ClaimTransition{transition}, test.registry, testStateCodec(),
			)
			if err != nil {
				t.Fatalf("buildRecoveryPlan returned error: %v", err)
			}
			requireClassification(t, plan, test.classification)
			if actions := plan.Actions(); len(actions) != 1 || actions[0].Kind != test.actionKind {
				t.Fatalf("actions = %#v, want one %s", actions, test.actionKind)
			}
		})
	}
}

func TestBuildRecoveryPlanBlocksForeignAcquireClaim(t *testing.T) {
	entry := globalAcquireRecoveryEntry(t)
	journal := recoveryJournalFor(entry)
	transition, _, _ := testAcquireTransition(t, entry)
	foreignOwner, err := ownership.NewOwnerAuthority("/tmp/foreign-state.json", "/tmp/foreign-daem.toml")
	if err != nil {
		t.Fatalf("NewOwnerAuthority returned error: %v", err)
	}
	foreign, err := ownership.NewActiveClaim(transition.Address(), foreignOwner)
	if err != nil {
		t.Fatalf("NewActiveClaim returned error: %v", err)
	}

	plan, err := buildRecoveryPlan(
		testOperationID, testOperationDir, journal, journal.StatefileAfter,
		[]recoveryPathObservation{ownershipAfterPathObservation(entry)}, nil,
		[]ownershipmutation.ClaimTransition{transition}, mustRegistry(t, foreign), testStateCodec(),
	)
	if err != nil {
		t.Fatalf("buildRecoveryPlan returned error: %v", err)
	}
	requireClassification(t, plan, recovery.ClassificationBlocked)
	requireErrorAction(t, plan, "claim_mismatch", "ownership claims differ from before, prepared, and expected-after phases")
}

func TestBuildRecoveryPlanClassifiesReleaseFinalization(t *testing.T) {
	entry := globalReleaseRecoveryEntry(t)
	journal := recoveryJournalFor(entry)
	canonicalPath, err := mutation.CanonicalDirectoryEntryKey("/tmp/daem-global-config.json")
	if err != nil {
		t.Fatalf("CanonicalDirectoryEntryKey returned error: %v", err)
	}
	address, err := ownership.NewManagedAddress(canonicalPath, entry.ContentPath)
	if err != nil {
		t.Fatalf("NewManagedAddress returned error: %v", err)
	}
	owner, err := ownership.NewOwnerAuthority("/tmp/daem-state.json", "/tmp/daem.toml")
	if err != nil {
		t.Fatalf("NewOwnerAuthority returned error: %v", err)
	}
	active, err := ownership.NewActiveClaim(address, owner)
	if err != nil {
		t.Fatalf("NewActiveClaim returned error: %v", err)
	}
	transition, err := ownershipmutation.NewReleaseTransition(active)
	if err != nil {
		t.Fatalf("NewReleaseTransition returned error: %v", err)
	}

	plan, err := buildRecoveryPlan(
		testOperationID, testOperationDir, journal, journal.StatefileAfter,
		[]recoveryPathObservation{ownershipAfterPathObservation(entry)}, nil,
		[]ownershipmutation.ClaimTransition{transition}, mustRegistry(t, active), testStateCodec(),
	)
	if err != nil {
		t.Fatalf("buildRecoveryPlan returned error: %v", err)
	}
	requireClassification(t, plan, recovery.ClassificationNeedsFinalize)
	requireOnlyAction(t, plan, recovery.ActionKindFinalizeClaims, string(recovery.ClassificationNeedsFinalize))
}

func globalAcquireRecoveryEntry(t *testing.T) recoveryEntry {
	t.Helper()
	entry := recoveryEntryFor("global", "~/.codex/AGENTS.md", "", testAfterHash, "")
	entry.Before = persistedBeforePathState(recovery.BeforePathState{Existed: false})
	entry.ExpectedAfter.PathExisted = false
	entry.StateBefore = recoveryManagedMembership{}
	return entry
}

func globalReleaseRecoveryEntry(t *testing.T) recoveryEntry {
	t.Helper()
	entry := globalAcquireRecoveryEntry(t)
	entry.Before = persistedBeforePathState(recovery.BeforePathState{Existed: true, PathMode: testRecoveryPermissionMode(0o600), Kind: recovery.PathKindFile, ContentHash: testBeforeHash, BackupPath: testBackupPath})
	entry.ExpectedAfter = persistedExpectedPathState(recovery.ExpectedPathState{Existed: false})
	entry.StateBefore = recoveryManagedMembership{Managed: true, ContentHash: testBeforeHash}
	entry.StateExpectedAfter = recoveryManagedMembership{}
	return entry
}

func testAcquireTransition(t *testing.T, entry recoveryEntry) (ownershipmutation.ClaimTransition, ownership.Claim, ownership.Claim) {
	t.Helper()
	canonicalPath, err := mutation.CanonicalDirectoryEntryKey("/tmp/daem-global-config.json")
	if err != nil {
		t.Fatalf("CanonicalDirectoryEntryKey returned error: %v", err)
	}
	address, err := ownership.NewManagedAddress(canonicalPath, entry.ContentPath)
	if err != nil {
		t.Fatalf("NewManagedAddress returned error: %v", err)
	}
	owner, err := ownership.NewOwnerAuthority("/tmp/daem-state.json", "/tmp/daem.toml")
	if err != nil {
		t.Fatalf("NewOwnerAuthority returned error: %v", err)
	}
	transition, err := ownershipmutation.NewAcquireTransition(address, owner, testOperationID)
	if err != nil {
		t.Fatalf("NewAcquireTransition returned error: %v", err)
	}
	reserved, _ := transition.Prepared().Get()
	active, _ := transition.After().Get()
	return transition, reserved, active
}

func ownershipBeforePathObservation(entry recoveryEntry) recoveryPathObservation {
	observation := beforePathObservation(entry)
	observation.ContentPath = entry.ContentPath
	return observation
}

func ownershipAfterPathObservation(entry recoveryEntry) recoveryPathObservation {
	observation := afterPathObservation(entry)
	observation.ContentPath = entry.ContentPath
	return observation
}

func mustRegistry(t *testing.T, claims ...ownership.Claim) ownership.Registry {
	t.Helper()
	registry, err := ownership.NewRegistry(claims)
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	return registry
}
