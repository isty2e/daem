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
	ancestor, err := NewManifestRootProvenance(
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

	blockedPlan, err := newPlan(authority, ClassificationBlocked, nil, nil)
	if err != nil {
		t.Fatalf("construct blocked plan: %v", err)
	}
	if blockedPlan.RetirementReady([]RemovalCleanupObligation{discharged}) {
		t.Fatal("blocked plan became retirement-ready from matching residue evidence")
	}
}
