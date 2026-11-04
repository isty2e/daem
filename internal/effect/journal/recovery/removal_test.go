package recovery

import (
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

func testRemovalIntent(t *testing.T) RemovalIntent {
	t.Helper()
	return testRemovalIntentForKind(t, PathKindFile)
}

func testRemovalIntentForKind(t *testing.T, kind string) RemovalIntent {
	t.Helper()
	destination, err := output.Parse("skills/runner")
	if err != nil {
		t.Fatalf("parse removal destination: %v", err)
	}
	names, err := mutationfs.NewLogicalRemovalNames(
		".daem-tombstone-"+strings.Repeat("a", 32),
		".daem-cleanup-"+strings.Repeat("a", 32),
	)
	if err != nil {
		t.Fatalf("construct removal names: %v", err)
	}
	ancestor, err := NewRootProvenance(
		"/test",
		"sha256:"+strings.Repeat("1", 64),
		"sha256:"+strings.Repeat("2", 64),
	)
	if err != nil {
		t.Fatalf("construct retained ancestor: %v", err)
	}
	namespace, err := NewInitiallyAbsentParentAuthority(ancestor, "project", names)
	if err != nil {
		t.Fatalf("construct namespace: %v", err)
	}
	pathMode := NewPermissionMode(0o600)
	if kind == PathKindDirectory {
		pathMode = nil
	}
	state, err := NewBeforeRemovalState(BeforePathState{
		Existed:       true,
		PathExisted:   true,
		ParentExisted: true,
		PathMode:      pathMode,
		Kind:          kind,
		ContentHash:   "sha256:" + strings.Repeat("3", 64),
	})
	if err != nil {
		t.Fatalf("construct removal state: %v", err)
	}
	demand, err := NewRemovalDemand(target.ScopeProject, destination, []RemovalState{state})
	if err != nil {
		t.Fatalf("construct removal demand: %v", err)
	}
	intent, err := NewRemovalIntent(demand, namespace)
	if err != nil {
		t.Fatalf("construct removal intent: %v", err)
	}
	return intent
}

func TestRemovalNamespaceAuthoritiesCarryOnlyVariantFacts(t *testing.T) {
	parent, err := NewRootProvenance(
		"/test/project",
		"sha256:"+strings.Repeat("1", 64),
		"sha256:"+strings.Repeat("2", 64),
	)
	if err != nil {
		t.Fatalf("construct parent provenance: %v", err)
	}
	names := testRemovalIntent(t).Namespace().Names()
	authority, err := NewExistingParentAuthority(parent, names)
	if err != nil {
		t.Fatalf("construct existing-parent authority: %v", err)
	}
	if _, present := authority.ParentProvenance(); !present {
		t.Fatal("existing-parent authority lost exact parent provenance")
	}
	if _, present := authority.RetainedAncestorProvenance(); present {
		t.Fatal("existing-parent authority retained initially-absent ancestor facts")
	}
	if authority.MissingSuffix() != "" {
		t.Fatalf("existing-parent missing suffix = %q, want empty", authority.MissingSuffix())
	}

	initiallyAbsent := testRemovalIntent(t).Namespace()
	if _, present := initiallyAbsent.ParentProvenance(); present {
		t.Fatal("initially-absent authority exposed exact parent provenance")
	}
	if _, present := initiallyAbsent.RetainedAncestorProvenance(); !present {
		t.Fatal("initially-absent authority lost retained ancestor provenance")
	}
	if initiallyAbsent.MissingSuffix() == "" {
		t.Fatal("initially-absent authority lost its missing suffix")
	}
}

func TestRemovalIntentAssessesNamespaceAndEntryAxesIndependently(t *testing.T) {
	intent := testRemovalIntent(t)
	matched, err := NewRemovalNamespaceObservation(RemovalNamespaceMatched, "")
	if err != nil {
		t.Fatalf("construct matched namespace: %v", err)
	}
	present, err := NewRemovalResidueEntryObservation(
		RemovalResidueEntryPresent,
		PathKindFile,
		"sha256:"+strings.Repeat("3", 64),
		NewPermissionMode(0o600),
		"",
		"",
	)
	if err != nil {
		t.Fatalf("construct present residue: %v", err)
	}
	absent, err := NewRemovalResidueEntryObservation(RemovalResidueEntryAbsent, "", "", nil, "", "")
	if err != nil {
		t.Fatalf("construct absent residue: %v", err)
	}
	obligation, err := intent.AssessCleanup(
		matched,
		NewRemovalResidueObservation(present, absent),
	)
	if err != nil {
		t.Fatalf("assess present residue: %v", err)
	}
	if obligation.Readiness() != RemovalCleanupReady || obligation.Action() != RemovalCleanupActionPromoteResidue {
		t.Fatalf("present residue obligation = %#v, want ready promotion", obligation)
	}
	discharged, err := obligation.Discharge()
	if err != nil {
		t.Fatalf("discharge present residue: %v", err)
	}
	if discharged.Readiness() != RemovalCleanupDischarged {
		t.Fatalf("discharged readiness = %q, want discharged", discharged.Readiness())
	}

	absentObligation, err := intent.AssessCleanup(
		matched,
		NewRemovalResidueObservation(absent, absent),
	)
	if err != nil {
		t.Fatalf("assess absent residue: %v", err)
	}
	if absentObligation.Readiness() != RemovalCleanupReady || absentObligation.Action() != RemovalCleanupActionConfirmAbsence {
		t.Fatalf("absent obligation = %#v, want ready confirmation", absentObligation)
	}

	changed, err := NewRemovalNamespaceObservation(RemovalNamespaceChanged, "captured parent authority no longer matches")
	if err != nil {
		t.Fatalf("construct changed namespace: %v", err)
	}
	blocked, err := intent.AssessCleanup(changed, NewRemovalResidueObservation(present, absent))
	if err != nil {
		t.Fatalf("assess changed namespace: %v", err)
	}
	if blocked.Readiness() != RemovalCleanupBlocked || blocked.Reason() != RemovalCleanupReasonNamespaceChanged {
		t.Fatalf("changed namespace obligation = %#v, want blocked namespace change", blocked)
	}

	unavailable, err := NewRemovalNamespaceObservation(RemovalNamespaceUnavailable, "captured parent authority could not be observed")
	if err != nil {
		t.Fatalf("construct unavailable namespace: %v", err)
	}
	retry, err := intent.AssessCleanup(unavailable, NewRemovalResidueObservation(present, absent))
	if err != nil {
		t.Fatalf("assess unavailable namespace: %v", err)
	}
	if retry.Readiness() != RemovalCleanupRetry || retry.Reason() != RemovalCleanupReasonNamespaceUnavailable {
		t.Fatalf("unavailable namespace obligation = %#v, want retry", retry)
	}
}

func TestRemovalIntentTreatsCleanupStageAsDurableProgress(t *testing.T) {
	intent := testRemovalIntentForKind(t, PathKindDirectory)
	matched, err := NewRemovalNamespaceObservation(RemovalNamespaceMatched, "")
	if err != nil {
		t.Fatalf("construct matched namespace: %v", err)
	}
	absent, err := NewRemovalResidueEntryObservation(RemovalResidueEntryAbsent, "", "", nil, "", "")
	if err != nil {
		t.Fatalf("construct absent observation: %v", err)
	}
	partial, err := NewRemovalResidueEntryObservation(
		RemovalResidueEntryPresent,
		PathKindDirectory,
		"sha256:"+strings.Repeat("9", 64),
		nil,
		"",
		"",
	)
	if err != nil {
		t.Fatalf("construct partial cleanup observation: %v", err)
	}

	obligation, err := intent.AssessCleanup(
		matched,
		NewRemovalResidueObservation(absent, partial),
	)
	if err != nil {
		t.Fatalf("assess cleanup progress: %v", err)
	}
	if obligation.Readiness() != RemovalCleanupReady || obligation.Action() != RemovalCleanupActionCleanupProgress {
		t.Fatalf("cleanup progress obligation = %#v, want resumable cleanup", obligation)
	}

	collision, err := intent.AssessCleanup(
		matched,
		NewRemovalResidueObservation(partial, partial),
	)
	if err != nil {
		t.Fatalf("assess cleanup collision: %v", err)
	}
	if collision.Readiness() != RemovalCleanupBlocked || collision.Reason() != RemovalCleanupReasonCleanupCollision {
		t.Fatalf("cleanup collision = %#v, want blocked", collision)
	}

	wrongKind, err := NewRemovalResidueEntryObservation(
		RemovalResidueEntryPresent,
		PathKindFile,
		"sha256:"+strings.Repeat("8", 64),
		NewPermissionMode(0o600),
		"",
		"",
	)
	if err != nil {
		t.Fatalf("construct wrong-kind cleanup observation: %v", err)
	}
	mismatch, err := intent.AssessCleanup(
		matched,
		NewRemovalResidueObservation(absent, wrongKind),
	)
	if err != nil {
		t.Fatalf("assess cleanup kind mismatch: %v", err)
	}
	if mismatch.Readiness() != RemovalCleanupBlocked || mismatch.Reason() != RemovalCleanupReasonCleanupMismatch {
		t.Fatalf("cleanup kind mismatch = %#v, want blocked", mismatch)
	}
}

func TestRemovalIntentRequiresExactFileStateDuringCleanup(t *testing.T) {
	intent := testRemovalIntent(t)
	matched, err := NewRemovalNamespaceObservation(RemovalNamespaceMatched, "")
	if err != nil {
		t.Fatalf("construct matched namespace: %v", err)
	}
	absent, err := NewRemovalResidueEntryObservation(RemovalResidueEntryAbsent, "", "", nil, "", "")
	if err != nil {
		t.Fatalf("construct absent observation: %v", err)
	}
	mismatchedFile, err := NewRemovalResidueEntryObservation(
		RemovalResidueEntryPresent,
		PathKindFile,
		"sha256:"+strings.Repeat("9", 64),
		NewPermissionMode(0o600),
		"",
		"",
	)
	if err != nil {
		t.Fatalf("construct mismatched file observation: %v", err)
	}

	obligation, err := intent.AssessCleanup(
		matched,
		NewRemovalResidueObservation(absent, mismatchedFile),
	)
	if err != nil {
		t.Fatalf("assess mismatched file cleanup: %v", err)
	}
	if obligation.Readiness() != RemovalCleanupBlocked || obligation.Reason() != RemovalCleanupReasonCleanupMismatch {
		t.Fatalf("mismatched file cleanup = %#v, want blocked", obligation)
	}
}

func TestRemovalIntentClassifiesUnavailableAndUnsupportedSlots(t *testing.T) {
	intent := testRemovalIntent(t)
	matched, err := NewRemovalNamespaceObservation(RemovalNamespaceMatched, "")
	if err != nil {
		t.Fatalf("construct matched namespace: %v", err)
	}
	absent, err := NewRemovalResidueEntryObservation(RemovalResidueEntryAbsent, "", "", nil, "", "")
	if err != nil {
		t.Fatalf("construct absent observation: %v", err)
	}

	for _, test := range []struct {
		name      string
		residue   RemovalResidueEntryObservation
		cleanup   RemovalResidueEntryObservation
		readiness RemovalCleanupReadiness
		reason    RemovalCleanupReason
	}{
		{
			name:      "unsupported residue",
			residue:   mustRemovalSlotObservation(t, RemovalResidueEntryUnsupported, "residue kind is unsupported"),
			cleanup:   absent,
			readiness: RemovalCleanupBlocked,
			reason:    RemovalCleanupReasonResidueUnsupported,
		},
		{
			name:      "unavailable residue",
			residue:   mustRemovalSlotObservation(t, RemovalResidueEntryUnavailable, "residue could not be observed"),
			cleanup:   absent,
			readiness: RemovalCleanupRetry,
			reason:    RemovalCleanupReasonResidueUnavailable,
		},
		{
			name:      "unsupported cleanup stage",
			residue:   absent,
			cleanup:   mustRemovalSlotObservation(t, RemovalResidueEntryUnsupported, "cleanup kind is unsupported"),
			readiness: RemovalCleanupBlocked,
			reason:    RemovalCleanupReasonCleanupUnsupported,
		},
		{
			name:      "unavailable cleanup stage",
			residue:   absent,
			cleanup:   mustRemovalSlotObservation(t, RemovalResidueEntryUnavailable, "cleanup could not be observed"),
			readiness: RemovalCleanupRetry,
			reason:    RemovalCleanupReasonCleanupUnavailable,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			obligation, err := intent.AssessCleanup(
				matched,
				NewRemovalResidueObservation(test.residue, test.cleanup),
			)
			if err != nil {
				t.Fatalf("assess cleanup: %v", err)
			}
			if obligation.Readiness() != test.readiness || obligation.Reason() != test.reason {
				t.Fatalf("obligation = %#v, want readiness %q reason %q", obligation, test.readiness, test.reason)
			}
		})
	}
}

func mustRemovalSlotObservation(
	t *testing.T,
	status RemovalResidueEntryStatus,
	detail string,
) RemovalResidueEntryObservation {
	t.Helper()
	observation, err := NewRemovalResidueEntryObservation(status, "", "", nil, "", detail)
	if err != nil {
		t.Fatalf("construct %s removal slot observation: %v", status, err)
	}
	return observation
}

func TestRemovalCleanupObligationBasisSurvivesReadinessChanges(t *testing.T) {
	intent := testRemovalIntent(t)
	pending, err := NewPendingRemovalCleanupObligation(intent)
	if err != nil {
		t.Fatalf("construct pending obligation: %v", err)
	}
	matched, err := NewRemovalNamespaceObservation(RemovalNamespaceMatched, "")
	if err != nil {
		t.Fatalf("construct matched namespace: %v", err)
	}
	absent, err := NewRemovalResidueEntryObservation(RemovalResidueEntryAbsent, "", "", nil, "", "")
	if err != nil {
		t.Fatalf("construct absent residue: %v", err)
	}
	ready, err := intent.AssessCleanup(matched, NewRemovalResidueObservation(absent, absent))
	if err != nil {
		t.Fatalf("assess cleanup: %v", err)
	}
	discharged, err := ready.Discharge()
	if err != nil {
		t.Fatalf("discharge cleanup: %v", err)
	}
	if !pending.SameBasis(discharged) || pending.Equal(discharged) {
		t.Fatalf("pending and discharged obligations have incorrect equality/basis: pending=%#v discharged=%#v", pending, discharged)
	}
	if _, err := pending.Discharge(); err == nil {
		t.Fatal("pending obligation discharged without a ready action")
	}
}

func TestRemovalCleanupObligationBasisIncludesCompleteIntent(t *testing.T) {
	intent := testRemovalIntent(t)
	original, err := NewPendingRemovalCleanupObligation(intent)
	if err != nil {
		t.Fatalf("construct original cleanup obligation: %v", err)
	}

	before, present := intent.States()[0].Before()
	if !present {
		t.Fatal("test removal intent has no before state")
	}
	before.ContentHash = "sha256:" + strings.Repeat("4", 64)
	differentState, err := NewBeforeRemovalState(before)
	if err != nil {
		t.Fatalf("construct different removal state: %v", err)
	}
	differentDemand, err := NewRemovalDemand(
		intent.Scope(),
		intent.Destination(),
		[]RemovalState{differentState},
	)
	if err != nil {
		t.Fatalf("construct different removal demand: %v", err)
	}
	differentStateIntent, err := NewRemovalIntent(differentDemand, intent.Namespace())
	if err != nil {
		t.Fatalf("construct different-state intent: %v", err)
	}
	differentStateObligation, err := NewPendingRemovalCleanupObligation(differentStateIntent)
	if err != nil {
		t.Fatalf("construct different-state obligation: %v", err)
	}
	if original.SameBasis(differentStateObligation) {
		t.Fatal("cleanup obligations with different admitted states share one basis")
	}

	differentAncestor, err := NewRootProvenance(
		"/different",
		"sha256:"+strings.Repeat("5", 64),
		"sha256:"+strings.Repeat("6", 64),
	)
	if err != nil {
		t.Fatalf("construct different namespace provenance: %v", err)
	}
	differentNamespace, err := NewInitiallyAbsentParentAuthority(
		differentAncestor,
		"project",
		intent.Namespace().Names(),
	)
	if err != nil {
		t.Fatalf("construct different namespace: %v", err)
	}
	differentNamespaceIntent, err := NewRemovalIntent(intent.Demand(), differentNamespace)
	if err != nil {
		t.Fatalf("construct different-namespace intent: %v", err)
	}
	differentNamespaceObligation, err := NewPendingRemovalCleanupObligation(differentNamespaceIntent)
	if err != nil {
		t.Fatalf("construct different-namespace obligation: %v", err)
	}
	if original.SameBasis(differentNamespaceObligation) {
		t.Fatal("cleanup obligations with different namespace authority share one basis")
	}
}

func TestRemovalResidueEntryObservationRejectsStatusIncompatibleFacts(t *testing.T) {
	mode := NewPermissionMode(0o600)
	for _, test := range []struct {
		name        string
		status      RemovalResidueEntryStatus
		kind        string
		contentHash string
		mode        *PermissionMode
		detail      string
	}{
		{name: "absent detail", status: RemovalResidueEntryAbsent, detail: "unexpected"},
		{
			name:        "present detail",
			status:      RemovalResidueEntryPresent,
			kind:        PathKindFile,
			contentHash: "sha256:" + strings.Repeat("7", 64),
			mode:        mode,
			detail:      "unexpected",
		},
		{
			name:        "unsupported entry facts",
			status:      RemovalResidueEntryUnsupported,
			kind:        PathKindFile,
			contentHash: "sha256:" + strings.Repeat("8", 64),
			mode:        mode,
			detail:      "unsupported kind",
		},
		{name: "unavailable blank detail", status: RemovalResidueEntryUnavailable, detail: "   "},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewRemovalResidueEntryObservation(
				test.status,
				test.kind,
				test.contentHash,
				test.mode,
				"",
				test.detail,
			); err == nil {
				t.Fatal("status-incompatible residue observation was accepted")
			}
		})
	}
}

func TestPlanRetirementReadyRequiresCompleteDischargedObligations(t *testing.T) {
	intent := testRemovalIntent(t)
	authority := Authority{removalIntents: []RemovalIntent{intent}}
	plan, err := newPlan(authority, ClassificationCleanBefore, nil, nil)
	if err != nil {
		t.Fatalf("construct clean plan: %v", err)
	}
	if plan.RetirementReady(nil) {
		t.Fatal("clean plan became retirement-ready without residue reconciliation")
	}
	matched, err := NewRemovalNamespaceObservation(RemovalNamespaceMatched, "")
	if err != nil {
		t.Fatalf("construct matched namespace: %v", err)
	}
	absent, err := NewRemovalResidueEntryObservation(RemovalResidueEntryAbsent, "", "", nil, "", "")
	if err != nil {
		t.Fatalf("construct absent residue: %v", err)
	}
	ready, err := intent.AssessCleanup(matched, NewRemovalResidueObservation(absent, absent))
	if err != nil {
		t.Fatalf("assess clean plan obligation: %v", err)
	}
	discharged, err := ready.Discharge()
	if err != nil {
		t.Fatalf("discharge clean plan obligation: %v", err)
	}
	if !plan.RetirementReady([]RemovalCleanupObligation{discharged}) {
		t.Fatal("clean plan did not become retirement-ready after complete reconciliation")
	}

	differentAncestor, err := NewRootProvenance(
		"/different",
		"sha256:"+strings.Repeat("4", 64),
		"sha256:"+strings.Repeat("5", 64),
	)
	if err != nil {
		t.Fatalf("construct substituted cleanup ancestor: %v", err)
	}
	differentNamespace, err := NewInitiallyAbsentParentAuthority(
		differentAncestor,
		intent.Namespace().MissingSuffix(),
		intent.Namespace().Names(),
	)
	if err != nil {
		t.Fatalf("construct substituted cleanup namespace: %v", err)
	}
	differentIntent, err := NewRemovalIntent(intent.Demand(), differentNamespace)
	if err != nil {
		t.Fatalf("construct substituted cleanup intent: %v", err)
	}
	differentReady, err := differentIntent.AssessCleanup(
		matched,
		NewRemovalResidueObservation(absent, absent),
	)
	if err != nil {
		t.Fatalf("assess substituted cleanup obligation: %v", err)
	}
	differentDischarged, err := differentReady.Discharge()
	if err != nil {
		t.Fatalf("discharge substituted cleanup obligation: %v", err)
	}
	if plan.RetirementReady([]RemovalCleanupObligation{differentDischarged}) {
		t.Fatal("clean plan accepted a discharged obligation with different namespace authority")
	}

	blockedPlan, err := newPlan(authority, ClassificationBlocked, nil, nil)
	if err != nil {
		t.Fatalf("construct blocked plan: %v", err)
	}
	if blockedPlan.RetirementReady([]RemovalCleanupObligation{discharged}) {
		t.Fatal("blocked plan became retirement-ready from matching residue evidence")
	}
}

func TestRemovalDemandDuplicateIndexUsesRemovalStateIdentity(t *testing.T) {
	mode := PermissionMode(0o600)
	left := RemovalState{before: &BeforePathState{
		Existed: true, PathExisted: true, ParentExisted: true,
		PathMode: &mode, Kind: PathKindFile, ContentHash: strings.Repeat("a", 64),
		BackupPath: "backup-a",
	}}
	right := left
	rightBefore := cloneBeforePathState(*left.before)
	right.before = &rightBefore
	right.before.BackupPath = "backup-b"
	if !left.Equal(right) {
		t.Fatal("backup representation unexpectedly changed removal-state identity")
	}
	destination, err := output.Parse("skills/runner")
	if err != nil {
		t.Fatalf("parse destination: %v", err)
	}
	if _, err := NewRemovalDemand(
		target.ScopeProject,
		destination,
		[]RemovalState{left, right},
	); err == nil {
		t.Fatal("removal demand accepted duplicate authority with different backup representation")
	}
}

func TestRemovalDemandRejectsMoreThanBeforeAndExpectedStates(t *testing.T) {
	state, err := NewExpectedRemovalState(ExpectedPathState{})
	if err != nil {
		t.Fatalf("construct removal state: %v", err)
	}
	destination, err := output.Parse("skills/runner")
	if err != nil {
		t.Fatalf("parse destination: %v", err)
	}
	if _, err := NewRemovalDemand(
		target.ScopeProject,
		destination,
		[]RemovalState{state, state, state},
	); err == nil {
		t.Fatal("removal demand accepted more than before/expected states")
	}
}
