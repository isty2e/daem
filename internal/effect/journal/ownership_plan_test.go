package journal

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
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
	if err := validateRecoveryClaimCoverage([]recoveryEntry{entry}, nil, nil); err == nil || !strings.Contains(err.Error(), "has no exact ownership transition authority") {
		t.Fatalf("missing transition error = %v", err)
	}
	if _, err := bindRecoveryClaimCoverage(t.Context(), []recoveryEntry{entry}, []ownershipmutation.ClaimTransition{acquire}, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "destination resolver is required") {
		t.Fatalf("missing resolver error = %v", err)
	}
	bound, err := bindRecoveryClaimCoverage(
		t.Context(),
		[]recoveryEntry{entry},
		[]ownershipmutation.ClaimTransition{acquire},
		nil,
		resolver,
	)
	if err != nil {
		t.Fatalf("bind exact acquire coverage returned error: %v", err)
	}
	if err := validateRecoveryClaimCoverage(bound, []ownershipmutation.ClaimTransition{acquire}, nil); err != nil {
		t.Fatalf("exact acquire coverage returned error: %v", err)
	}
	if bound[0].OwnershipPathAuthority == nil {
		t.Fatal("exact acquire coverage did not persist ownership path authority")
	}
	forged := append([]recoveryEntry(nil), bound...)
	wrongAuthority, err := pathauthority.NewExact("/tmp/other-global-config.json", "exact-v1:")
	if err != nil {
		t.Fatal(err)
	}
	forged[0].OwnershipPathAuthority = persistedPathAuthority(wrongAuthority)
	if err := validateRecoveryClaimCoverage(forged, []ownershipmutation.ClaimTransition{acquire}, nil); err == nil ||
		!strings.Contains(err.Error(), "has no exact ownership transition") {
		t.Fatalf("forged authority error = %v", err)
	}
	if err := validateRecoveryClaimCoverage(
		[]recoveryEntry{bound[0], bound[0]},
		[]ownershipmutation.ClaimTransition{acquire},
		nil,
	); err == nil || !strings.Contains(err.Error(), "has no exact ownership transition") {
		t.Fatalf("duplicate entry correlation error = %v", err)
	}
	release, err := ownershipmutation.NewReleaseTransition(active)
	if err != nil {
		t.Fatalf("NewReleaseTransition returned error: %v", err)
	}
	if _, err := bindRecoveryClaimCoverage(t.Context(), []recoveryEntry{entry}, []ownershipmutation.ClaimTransition{release}, nil, resolver); err == nil || !strings.Contains(err.Error(), "requires acquire") {
		t.Fatalf("wrong transition error = %v", err)
	}
	project := defaultRecoveryEntry()
	if err := validateRecoveryClaimCoverage([]recoveryEntry{project}, nil, nil); err != nil {
		t.Fatalf("project-only coverage required a global destination resolver: %v", err)
	}
	if err := validateRecoveryClaimCoverage([]recoveryEntry{project}, []ownershipmutation.ClaimTransition{acquire}, nil); err == nil || !strings.Contains(err.Error(), "without a global output entry") {
		t.Fatalf("extra transition error = %v", err)
	}
}

func TestValidateRecoveryClaimCoverageRequiresProvisionalIntentBijection(t *testing.T) {
	entry := globalAcquireRecoveryEntry(t)
	intent := testProvisionalAcquireIntent(t, entry)
	acquire, _, _ := testAcquireTransition(t, entry)
	if err := validateRecoveryClaimCoverage(
		[]recoveryEntry{entry},
		nil,
		[]ownership.ProvisionalAcquireIntent{intent},
	); err != nil {
		t.Fatalf("exact provisional coverage returned error: %v", err)
	}
	if err := validateRecoveryClaimCoverage(
		[]recoveryEntry{entry},
		nil,
		[]ownership.ProvisionalAcquireIntent{intent, intent},
	); err == nil || !strings.Contains(err.Error(), "duplicate provisional acquisition intent") {
		t.Fatalf("duplicate intent error = %v", err)
	}
	if err := validateRecoveryClaimCoverage(
		[]recoveryEntry{entry},
		[]ownershipmutation.ClaimTransition{acquire},
		[]ownership.ProvisionalAcquireIntent{intent},
	); err == nil || !strings.Contains(err.Error(), "ownership transition without a global output entry") {
		t.Fatalf("ambiguous exact and provisional coverage error = %v", err)
	}
	if err := validateRecoveryClaimCoverage(
		[]recoveryEntry{defaultRecoveryEntry()},
		nil,
		[]ownership.ProvisionalAcquireIntent{intent},
	); err == nil || !strings.Contains(err.Error(), "provisional acquisition intent without a global output entry") {
		t.Fatalf("orphan intent error = %v", err)
	}
}

func TestValidateRecoveryClaimCoverageRejectsMisplacedPathAuthority(t *testing.T) {
	global := globalAcquireRecoveryEntry(t)
	transition, _, _ := testAcquireTransition(t, global)
	global.OwnershipPathAuthority = persistedPathAuthority(transition.Address().PathAuthority())
	intent := testProvisionalAcquireIntent(t, global)
	if err := validateRecoveryClaimCoverage(
		[]recoveryEntry{global},
		nil,
		[]ownership.ProvisionalAcquireIntent{intent},
	); err == nil || !strings.Contains(err.Error(), "provisional acquisition must not carry exact") {
		t.Fatalf("provisional authority error = %v", err)
	}

	project := defaultRecoveryEntry()
	project.OwnershipPathAuthority = persistedPathAuthority(transition.Address().PathAuthority())
	if err := validateRecoveryClaimCoverage([]recoveryEntry{project}, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "must not carry ownership path authority") {
		t.Fatalf("project authority error = %v", err)
	}
}

func TestValidateRecoveryOwnershipWorkBudget(t *testing.T) {
	if err := validateRecoveryOwnershipWorkBudget(
		maximumRecoveryOwnershipWorkItems/2,
		maximumRecoveryOwnershipWorkItems/2,
		0,
	); err != nil {
		t.Fatalf("exact budget returned error: %v", err)
	}
	if err := validateRecoveryOwnershipWorkBudget(
		maximumRecoveryOwnershipWorkItems/2,
		maximumRecoveryOwnershipWorkItems/2,
		1,
	); err == nil || !strings.Contains(err.Error(), "exceed") {
		t.Fatalf("over-budget error = %v", err)
	}
	if err := validateRecoveryOwnershipWorkBudget(-1, 0, 0); err == nil ||
		!strings.Contains(err.Error(), "non-negative") {
		t.Fatalf("negative-count error = %v", err)
	}
}

func TestRecoveryContentPathOverlapDetectionDoesNotDependOnLexicalAdjacency(t *testing.T) {
	if err := validateNonOverlappingRecoveryContentPaths([]string{"/a", "/a!", "/a/b"}); err == nil {
		t.Fatal("non-adjacent ancestor content paths were accepted")
	}
	if err := validateNonOverlappingRecoveryContentPaths([]string{"/a", "/a!", "/b"}); err != nil {
		t.Fatalf("disjoint content paths returned error: %v", err)
	}
}

func TestRecoveryAddressOverlapDetectionDoesNotDependOnLexicalAdjacency(t *testing.T) {
	address := func(path string, contentPath string) ownership.ManagedAddress {
		t.Helper()
		authority, err := pathauthority.NewExact(path, "exact-v1:")
		if err != nil {
			t.Fatal(err)
		}
		managed, err := ownership.NewManagedAddress(authority, contentPath)
		if err != nil {
			t.Fatal(err)
		}
		return managed
	}
	if err := validateNonOverlappingRecoveryAddresses([]ownership.ManagedAddress{
		address("/tmp/a", "/one"),
		address("/tmp/a!", "/one"),
		address("/tmp/a/b", "/two"),
	}); err == nil {
		t.Fatal("non-adjacent ancestor filesystem paths were accepted")
	}
	if err := validateNonOverlappingRecoveryAddresses([]ownership.ManagedAddress{
		address("/tmp/a", "/one"),
		address("/tmp/a!", "/one"),
		address("/tmp/b", "/two"),
	}); err != nil {
		t.Fatalf("disjoint filesystem paths returned error: %v", err)
	}

	exact, err := pathauthority.NewExact("/tmp/a", "exact-v1:")
	if err != nil {
		t.Fatal(err)
	}
	darwin, err := pathauthority.NewExact("/tmp/a", "darwin-case-v1:ss")
	if err != nil {
		t.Fatal(err)
	}
	exactAddress, err := ownership.NewManagedAddress(exact, "/one")
	if err != nil {
		t.Fatal(err)
	}
	darwinAddress, err := ownership.NewManagedAddress(darwin, "/two")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateNonOverlappingRecoveryAddresses([]ownership.ManagedAddress{
		exactAddress,
		darwinAddress,
	}); err == nil {
		t.Fatal("one path key with conflicting authority witnesses was accepted")
	}
}

func TestRecoveryAddressOverlapDetectionMatchesManagedAddressContract(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		root,
		filepath.Join(root, "a"),
		filepath.Join(root, "a", "b"),
		filepath.Join(root, "a!"),
		filepath.Join(root, "b"),
	}
	contentPaths := []string{"", "/a", "/a/b", "/a!", "/b"}
	addresses := make([]ownership.ManagedAddress, 0, len(paths)*len(contentPaths))
	for _, path := range paths {
		authority, err := pathauthority.NewExact(path, "exact-v1:")
		if err != nil {
			t.Fatal(err)
		}
		for _, contentPath := range contentPaths {
			address, err := ownership.NewManagedAddress(authority, contentPath)
			if err != nil {
				t.Fatal(err)
			}
			addresses = append(addresses, address)
		}
	}
	for left := range addresses {
		for right := left + 1; right < len(addresses); right++ {
			err := validateNonOverlappingRecoveryAddresses([]ownership.ManagedAddress{
				addresses[left],
				addresses[right],
			})
			if got, want := err != nil, addresses[left].Overlaps(addresses[right]); got != want {
				t.Fatalf(
					"overlap verdict for %#v and %#v = %t, want %t (error %v)",
					addresses[left],
					addresses[right],
					got,
					want,
					err,
				)
			}
		}
	}
}

func TestBindRecoveryClaimCoverageRejectsInvalidCanonicalInputs(t *testing.T) {
	entry := globalAcquireRecoveryEntry(t)
	resolver := func(destination output.Destination) (string, error) {
		return "/tmp/daem-global-config.json", nil
	}
	if _, err := bindRecoveryClaimCoverage(
		t.Context(),
		[]recoveryEntry{entry},
		[]ownershipmutation.ClaimTransition{{}},
		nil,
		resolver,
	); err == nil || !strings.Contains(err.Error(), "unsupported ownership transition kind") {
		t.Fatalf("zero transition error = %v", err)
	}
	if _, err := bindRecoveryClaimCoverage(
		t.Context(),
		[]recoveryEntry{entry},
		nil,
		[]ownership.ProvisionalAcquireIntent{{}},
		resolver,
	); err == nil || !strings.Contains(err.Error(), "provisional acquisition destination") {
		t.Fatalf("zero provisional intent error = %v", err)
	}
}

func TestRecoveryClaimRemovalCandidatesIncludeRollbackDeletionsOnly(t *testing.T) {
	entry := globalAcquireRecoveryEntry(t)
	acquire, reserved, active := testAcquireTransition(t, entry)
	release, err := ownershipmutation.NewReleaseTransition(active)
	if err != nil {
		t.Fatal(err)
	}
	retain, err := ownershipmutation.NewRetainTransition(active)
	if err != nil {
		t.Fatal(err)
	}

	candidates := recoveryClaimRemovalCandidates(
		[]ownershipmutation.ClaimTransition{retain, release, acquire},
	)
	if len(candidates) != 2 || !candidates[0].Equal(active) || !candidates[1].Equal(reserved) {
		t.Fatalf("removal candidates = %#v, want release active and acquire reserved", candidates)
	}
}

func TestRecoveryClaimTransitionsRejectsZeroTransition(t *testing.T) {
	if _, err := recoveryClaimTransitions([]ownershipmutation.ClaimTransition{{}}); err == nil ||
		!strings.Contains(err.Error(), "unsupported ownership transition kind") {
		t.Fatalf("zero transition error = %v", err)
	}
}

func TestRecoveryClaimTransitionsRejectMissingAndUnknownPathAuthorityWitness(t *testing.T) {
	entry := globalAcquireRecoveryEntry(t)
	transition, _, _ := testAcquireTransition(t, entry)
	persisted, err := recoveryClaimTransitions([]ownershipmutation.ClaimTransition{transition})
	if err != nil {
		t.Fatal(err)
	}

	missing := append([]recoveryClaimTransition(nil), persisted...)
	missing[0].Prepared.PathAuthority = nil
	if _, err := canonicalClaimTransitions(missing); err == nil ||
		!strings.Contains(err.Error(), "requires path_authority") {
		t.Fatalf("missing path authority error = %v", err)
	}

	unknown := append([]recoveryClaimTransition(nil), persisted...)
	unknownPath := *unknown[0].Prepared.PathAuthority
	unknownPath.Witness = "future-v1:"
	unknown[0].Prepared.PathAuthority = &unknownPath
	if _, err := canonicalClaimTransitions(unknown); err == nil ||
		!strings.Contains(err.Error(), "unsupported path authority semantics witness") {
		t.Fatalf("unknown path witness error = %v", err)
	}
}

func TestValidateRecoveryClaimAuthoritiesRejectsForeignKeyBeforeObservation(t *testing.T) {
	entry := globalAcquireRecoveryEntry(t)
	transition, _, _ := testAcquireTransition(t, entry)
	statefilePath := filepath.Join(t.TempDir(), "other-state.json")
	authority, err := mutation.ObservePersistedDirectoryEntryAuthority(statefilePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRecoveryClaimAuthorities(
		[]ownershipmutation.ClaimTransition{transition},
		authority,
	); err == nil || !strings.Contains(err.Error(), "incompatible state authority") ||
		strings.Contains(err.Error(), "legacy-darwin-path-authority") {
		t.Fatalf("recovery authority error = %v", err)
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
	foreignOwner, err := stateauthority.New(
		mustObservedPathAuthority(t, "/tmp/foreign-state.json"),
		"/tmp/foreign-daem.toml",
	)
	if err != nil {
		t.Fatalf("stateauthority.New returned error: %v", err)
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
	address, err := ownership.NewManagedAddress(mustObservedPathAuthority(t, canonicalPath), entry.ContentPath)
	if err != nil {
		t.Fatalf("NewManagedAddress returned error: %v", err)
	}
	owner, err := stateauthority.New(mustObservedPathAuthority(t, "/tmp/daem-state.json"), "/tmp/daem.toml")
	if err != nil {
		t.Fatalf("stateauthority.New returned error: %v", err)
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
	entry.GlobalPathBinding = testRecoveryGlobalPathBinding("/tmp/daem-global-config.json")
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
	address, err := ownership.NewManagedAddress(mustObservedPathAuthority(t, canonicalPath), entry.ContentPath)
	if err != nil {
		t.Fatalf("NewManagedAddress returned error: %v", err)
	}
	owner, err := stateauthority.New(mustObservedPathAuthority(t, "/tmp/daem-state.json"), "/tmp/daem.toml")
	if err != nil {
		t.Fatalf("stateauthority.New returned error: %v", err)
	}
	transition, err := ownershipmutation.NewAcquireTransition(address, owner, testOperationID)
	if err != nil {
		t.Fatalf("NewAcquireTransition returned error: %v", err)
	}
	reserved, _ := transition.Prepared().Get()
	active, _ := transition.After().Get()
	return transition, reserved, active
}

func testProvisionalAcquireIntent(t *testing.T, entry recoveryEntry) ownership.ProvisionalAcquireIntent {
	t.Helper()
	namespace := filepath.Join(string(filepath.Separator), "tmp", "daem-provisional")
	candidate := filepath.Join(namespace, "Caf\u00e9")
	provisional, err := pathauthority.NewProvisional(
		candidate,
		pathtest.DarwinCaseSensitive(candidate).Witness(),
		namespace,
		pathtest.DarwinCaseSensitive(namespace).Witness(),
	)
	if err != nil {
		t.Fatalf("NewProvisional returned error: %v", err)
	}
	destination, err := output.Parse(entry.Path)
	if err != nil {
		t.Fatalf("parse provisional destination: %v", err)
	}
	transition, _, _ := testAcquireTransition(t, entry)
	intent, err := ownership.NewProvisionalAcquireIntent(
		destination,
		output.ContentPath(entry.ContentPath),
		provisional,
		transition.Owner(),
		testOperationID,
	)
	if err != nil {
		t.Fatalf("NewProvisionalAcquireIntent returned error: %v", err)
	}
	return intent
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

func mustObservedPathAuthority(t *testing.T, path string) pathauthority.Exact {
	t.Helper()
	authority, err := mutation.ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatalf("ObservePersistedDirectoryEntryAuthority(%q): %v", path, err)
	}
	return authority.Exact()
}
