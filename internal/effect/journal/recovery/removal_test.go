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

func TestPhysicalWorkBudgetBoundsAggregateExecutionPasses(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(2)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	observed, err := NewArtifactWork(99_999, 4<<30)
	if err != nil {
		t.Fatalf("construct observed work: %v", err)
	}
	if err := budget.AdmitTree(observed); err != nil {
		t.Fatalf("admit observed work: %v", err)
	}
	if err := budget.ReserveDirectoryReobservation(observed); err != nil {
		t.Fatalf("reserve effect-time reobservation: %v", err)
	}
	if err := budget.ReserveDirectoryCleanup(observed); err != nil {
		t.Fatalf("reserve directory cleanup: %v", err)
	}
	if budget.RemainingEntries() != 1 || budget.RemainingBytes() != 0 {
		t.Fatalf(
			"remaining budget = entries:%d bytes:%d, want entries:%d bytes:%d",
			budget.RemainingEntries(), budget.RemainingBytes(), 1, int64(0),
		)
	}
	if err := budget.ReserveDirectoryReobservation(observed); err == nil {
		t.Fatal("aggregate execution passes exceeded the operation budget")
	}
}

func TestPhysicalWorkBudgetReservesForwardObservationAndDirectoryPasses(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	work, err := NewArtifactWork(99_999, 4<<30)
	if err != nil {
		t.Fatalf("construct forward work: %v", err)
	}
	if err := budget.AdmitTree(work); err != nil {
		t.Fatalf("charge capacity evidence: %v", err)
	}
	capacity, err := budget.ReserveForwardRemoval(work, true)
	if err != nil {
		t.Fatalf("reserve forward removal: %v", err)
	}
	if budget.RemainingEntries() != 3 || budget.RemainingBytes() != 0 {
		t.Fatalf(
			"remaining budget = entries:%d bytes:%d, want 3/0",
			budget.RemainingEntries(),
			budget.RemainingBytes(),
		)
	}
	observation, err := capacity.BeginObservation()
	if err != nil {
		t.Fatalf("begin forward observation: %v", err)
	}
	if err := observation.AdmitObservation(); err != nil {
		t.Fatalf("admit forward observation: %v", err)
	}
	if err := observation.AdmitTree(work); err != nil {
		t.Fatalf("realize forward observation: %v", err)
	}
	if err := observation.AdmitTreeWithin(
		ArtifactWork{entries: 1},
		ArtifactWork{},
	); err == nil {
		t.Fatal("forward observation admitted overflow capacity as semantic work")
	}
	if err := observation.AdmitTree(work); err == nil {
		t.Fatal("forward observation exceeded reserved candidate work")
	}
}

func TestForwardRemovalCapacityKeepsZeroWorkProbeSeparate(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	empty, err := NewArtifactWork(0, 0)
	if err != nil {
		t.Fatalf("construct empty work: %v", err)
	}
	capacity, err := budget.ReserveForwardRemoval(empty, true)
	if err != nil {
		t.Fatalf("reserve empty forward removal: %v", err)
	}
	observation, err := capacity.BeginObservation()
	if err != nil {
		t.Fatalf("begin empty forward observation: %v", err)
	}
	if err := observation.AdmitObservation(); err != nil {
		t.Fatalf("admit empty forward observation: %v", err)
	}
	probe, err := NewArtifactWork(1, 0)
	if err != nil {
		t.Fatalf("construct probe work: %v", err)
	}
	if err := observation.AdmitIndeterminateDirectoryWork(empty, probe); err != nil {
		t.Fatalf("charge empty proof probe: %v", err)
	}
	if capacity.Admits(probe) {
		t.Fatal("zero-work capacity admitted positive semantic growth")
	}
}

func TestForwardRemovalCapacityRejectsPerTreeOverflowBeforeReservation(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	tooManyEntries, err := NewArtifactWork(MaximumArtifactTreeEntries+1, 0)
	if err != nil {
		t.Fatalf("construct excessive tree work: %v", err)
	}
	if _, err := budget.ReserveForwardRemoval(tooManyEntries, true); err == nil {
		t.Fatal("forward removal admitted work beyond the per-tree entry limit")
	}
}

func TestForwardRemovalCapacityRejectsDescendantWorkForFile(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	work, err := NewArtifactWork(1, 0)
	if err != nil {
		t.Fatalf("construct invalid file work: %v", err)
	}
	if _, err := budget.ReserveForwardRemoval(work, false); err == nil {
		t.Fatal("forward file removal admitted descendant-entry work")
	}
}

func TestForwardRemovalCapacityBoundsAggregateDirectoryPasses(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(2)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	maximum, err := NewArtifactWork(MaximumArtifactTreeEntries-1, MaximumArtifactTreeBytes)
	if err != nil {
		t.Fatalf("construct maximum directory work: %v", err)
	}
	if err := budget.AdmitTree(maximum); err != nil {
		t.Fatalf("charge maximum directory evidence: %v", err)
	}
	if _, err := budget.ReserveForwardRemoval(maximum, true); err != nil {
		t.Fatalf("reserve maximum directory removal: %v", err)
	}
	positive, err := NewArtifactWork(1, 1)
	if err != nil {
		t.Fatalf("construct positive directory work: %v", err)
	}
	if err := budget.AdmitTree(positive); err == nil {
		t.Fatal("operation budget admitted a second non-empty directory after four maximum passes")
	}
}

func TestForwardRemovalCapacityEnvelopeDoesNotAuthorizeSyntheticWork(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(2)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	entryWork, err := NewArtifactWork(3, 0)
	if err != nil {
		t.Fatalf("construct entry-heavy work: %v", err)
	}
	byteWork, err := NewArtifactWork(0, 5)
	if err != nil {
		t.Fatalf("construct byte-heavy work: %v", err)
	}
	entryCapacity, err := budget.ReserveForwardRemoval(entryWork, true)
	if err != nil {
		t.Fatalf("reserve entry-heavy capacity: %v", err)
	}
	byteCapacity, err := budget.ReserveForwardRemoval(byteWork, false)
	if err != nil {
		t.Fatalf("reserve byte-heavy capacity: %v", err)
	}
	envelope, err := entryCapacity.Envelope(byteCapacity)
	if err != nil {
		t.Fatalf("combine observation envelope: %v", err)
	}
	synthetic, err := NewArtifactWork(3, 5)
	if err != nil {
		t.Fatalf("construct synthetic work: %v", err)
	}
	if !envelope.Admits(synthetic) {
		t.Fatal("combined observation envelope did not cover independently reserved dimensions")
	}
	if entryCapacity.Admits(synthetic) || byteCapacity.Admits(synthetic) {
		t.Fatal("one state capacity authorized synthetic cross-state work")
	}
}

func TestPhysicalWorkBudgetCarriesReservedExecutionAndNamespaceWork(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	if err := budget.AdmitObservation(); err != nil {
		t.Fatalf("admit preflight observation: %v", err)
	}
	if err := budget.AdmitPathComponents(2); err != nil {
		t.Fatalf("admit preflight path: %v", err)
	}
	if err := budget.ReserveExecutionObservations(2, 2, 2, mutationfs.RootedAbsencePathObservationCount); err != nil {
		t.Fatalf("reserve execution observations: %v", err)
	}
	work, err := NewArtifactWork(3, 7)
	if err != nil {
		t.Fatalf("construct removal work: %v", err)
	}
	if err := budget.ReserveReobservation(work); err != nil {
		t.Fatalf("reserve reobservation: %v", err)
	}
	execution, err := budget.BeginReservedExecution()
	if err != nil {
		t.Fatalf("begin reserved execution: %v", err)
	}
	if execution.RemainingEntries() != 3 || execution.RemainingBytes() != 7 {
		t.Fatalf(
			"reserved execution capacity = entries:%d bytes:%d, want 3/7",
			execution.RemainingEntries(),
			execution.RemainingBytes(),
		)
	}
	for range 11 {
		if err := execution.AdmitObservation(); err != nil {
			t.Fatalf("realize reserved observation: %v", err)
		}
	}
	extraPasses := forwardRemovalNamespacePasses + 2*removalSlotObservationPasses +
		2*removalSlotMutationPasses + 2*removalSlotAbsencePasses
	for range extraPasses {
		if err := execution.AdmitPathComponents(2); err != nil {
			t.Fatalf("realize reserved cleanup path validation: %v", err)
		}
	}
	if err := execution.AdmitObservation(); err == nil {
		t.Fatal("reserved execution admitted unreserved observation")
	}
	if err := execution.AdmitPathComponents(1); err == nil {
		t.Fatal("reserved execution admitted unreserved path work")
	}
	if err := execution.AdmitTree(work); err != nil {
		t.Fatalf("realize reserved reobservation: %v", err)
	}
	if err := execution.AdmitTree(work); err == nil {
		t.Fatal("reserved execution admitted unreserved tree work")
	}
}

func TestPhysicalWorkBudgetTransfersReservedHostAndRemainingControlCapacity(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitPathComponents(7); err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveBackupPathComponents(11); err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveGeneralPathComponents(13); err != nil {
		t.Fatal(err)
	}
	emptyScratch, err := NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveScratchCleanup(emptyScratch); err != nil {
		t.Fatal(err)
	}
	if _, err := budget.BeginReservedCleanupLifecycle(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := budget.BeginReservedScratchCleanup(); err != nil {
		t.Fatal(err)
	}
	if err := budget.ConcludeRetirementNotApplicable(); err != nil {
		t.Fatal(err)
	}
	host, control, err := budget.BeginGeneralExecution()
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.AdmitPathComponents(1); err == nil {
		t.Fatal("planning budget admitted general path work after transfer")
	}
	if _, _, err := budget.BeginGeneralExecution(); err == nil {
		t.Fatal("general execution capacity transferred twice")
	}
	if err := host.AdmitPathComponents(13); err != nil {
		t.Fatalf("host execution budget rejected reserved capacity: %v", err)
	}
	if err := host.AdmitPathComponents(1); err == nil {
		t.Fatal("host execution admitted unreserved path work")
	}
	if err := control.AdmitPathComponents(1); err != nil {
		t.Fatalf("control execution rejected remaining path capacity: %v", err)
	}
	backup, err := budget.BeginReservedBackupExecution()
	if err != nil {
		t.Fatalf("begin reserved backup execution after general transfer: %v", err)
	}
	if err := backup.AdmitPathComponents(11); err != nil {
		t.Fatalf("reserved backup capacity was lost: %v", err)
	}
}

func TestPhysicalWorkBudgetRejectsRetirementPassesBeyondOperationCeiling(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	work, err := NewArtifactWork(MaximumPhysicalEntries/6, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveRetirementDirectoryPasses(work, 6); err == nil {
		t.Fatal("retirement reservation exceeded the operation entry ceiling")
	}
	if _, err := budget.BeginReservedRetirementExecution(); err != nil {
		t.Fatalf("failed reservation changed retirement disposition: %v", err)
	}
}

func TestPhysicalWorkBudgetRequiresExplicitRetirementDisposition(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := budget.BeginReservedCleanupLifecycle(); err != nil {
		t.Fatal(err)
	}
	if err := budget.ConcludeScratchCleanupNotApplicable(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := budget.BeginGeneralExecution(); err == nil {
		t.Fatal("general execution began with pending retirement capacity")
	}
	if err := budget.ConcludeRetirementNotApplicable(); err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveRetirementPathComponents(1); err == nil {
		t.Fatal("not-applicable retirement accepted a later reservation")
	}
	if _, _, err := budget.BeginGeneralExecution(); err != nil {
		t.Fatalf("general execution rejected explicit retirement disposition: %v", err)
	}
}

func TestPhysicalWorkBudgetRequiresExplicitScratchDisposition(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := budget.BeginReservedScratchCleanup(); err == nil {
		t.Fatal("unreserved recovery scratch cleanup was transferred")
	}
	if err := budget.ConcludeScratchCleanupNotApplicable(); err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveScratchCleanup(ArtifactWork{}); err == nil {
		t.Fatal("not-applicable recovery scratch accepted a later reservation")
	}
}

func TestPhysicalWorkBudgetTransfersOnlyReservedSemanticPathCapacity(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveSemanticPathComponents(7); err != nil {
		t.Fatalf("reserve semantic path work: %v", err)
	}
	semantic, err := budget.BeginReservedSemanticExecution()
	if err != nil {
		t.Fatalf("begin semantic execution: %v", err)
	}
	if err := budget.ReserveSemanticPathComponents(1); err == nil {
		t.Fatal("planning budget admitted semantic work after transfer")
	}
	if _, err := budget.BeginReservedSemanticExecution(); err == nil {
		t.Fatal("semantic execution capacity transferred twice")
	}
	if err := semantic.AdmitPathComponents(7); err != nil {
		t.Fatalf("semantic execution rejected reserved path work: %v", err)
	}
	if err := semantic.AdmitPathComponents(1); err == nil {
		t.Fatal("semantic execution admitted unreserved path work")
	}
}

func TestPhysicalWorkBudgetTransfersOnlyReservedGeneralContent(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	emptyFile, err := NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := NewArtifactWork(2, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveGeneralFileObservation(emptyFile); err != nil {
		t.Fatalf("reserve empty-file observation: %v", err)
	}
	if err := budget.ReserveGeneralDirectoryObservation(directory); err != nil {
		t.Fatalf("reserve directory observation: %v", err)
	}
	if err := budget.ReserveScratchCleanup(emptyFile); err != nil {
		t.Fatal(err)
	}
	if _, err := budget.BeginReservedCleanupLifecycle(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := budget.BeginReservedScratchCleanup(); err != nil {
		t.Fatal(err)
	}
	if err := budget.ConcludeRetirementNotApplicable(); err != nil {
		t.Fatal(err)
	}
	host, _, err := budget.BeginGeneralExecution()
	if err != nil {
		t.Fatal(err)
	}
	emptyFileReader, err := NewArtifactWork(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.AdmitIndeterminateTreeWork(emptyFile, emptyFileReader); err != nil {
		t.Fatalf("consume reserved empty-file proof: %v", err)
	}
	directoryReader, err := NewArtifactWork(3, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.AdmitIndeterminateDirectoryWork(directory, directoryReader); err != nil {
		t.Fatalf("consume reserved directory observation: %v", err)
	}
	positiveFile, err := NewArtifactWork(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.AdmitTree(positiveFile); err == nil {
		t.Fatal("host execution admitted unreserved content work")
	}
}

func TestPhysicalWorkBudgetTransfersOnlyReservedForwardPathWork(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(2)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	if err := budget.ReserveForwardExecutionPathWork([]int{3, 5}); err != nil {
		t.Fatalf("reserve forward path work: %v", err)
	}
	execution, err := budget.BeginReservedForwardExecution()
	if err != nil {
		t.Fatalf("begin reserved forward execution: %v", err)
	}
	passCount := forwardRemovalCapabilityPasses +
		forwardRemovalNamespacePasses +
		forwardRemovalCandidatePasses
	depths := make([]int, 0, passCount*2)
	for _, depth := range []int{3, 5} {
		for range passCount {
			depths = append(depths, depth)
		}
	}
	for index, depth := range depths {
		if index%passCount == 0 {
			if err := execution.AdmitObservation(); err != nil {
				t.Fatalf("consume forward observation %d: %v", index/passCount, err)
			}
		}
		if err := execution.AdmitPathComponents(depth); err != nil {
			t.Fatalf("consume forward path %d: %v", index, err)
		}
	}
	if err := execution.AdmitObservation(); err == nil {
		t.Fatal("forward execution admitted an unreserved observation")
	}
	if err := execution.AdmitPathComponents(1); err == nil {
		t.Fatal("forward execution admitted unreserved path work")
	}
	if execution.RemainingEntries() != 0 || execution.RemainingBytes() != 0 {
		t.Fatalf(
			"forward execution inherited content capacity entries:%d bytes:%d, want 0/0",
			execution.RemainingEntries(),
			execution.RemainingBytes(),
		)
	}
	if _, err := budget.BeginReservedForwardExecution(); err == nil {
		t.Fatal("forward execution capacity was transferred more than once")
	}
	if err := budget.ReserveForwardExecutionPathWork([]int{1}); err == nil {
		t.Fatal("forward path capacity changed after execution transfer")
	}
}

func TestPhysicalWorkBudgetTransfersOnlyReservedBackupWork(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(2)
	if err != nil {
		t.Fatal(err)
	}
	fileWork, err := NewArtifactWork(0, 3)
	if err != nil {
		t.Fatal(err)
	}
	directoryWork, err := NewArtifactWork(2, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveBackupPathComponents(7); err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveBackupFileExecution(fileWork); err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveBackupDirectoryExecution(directoryWork); err != nil {
		t.Fatal(err)
	}

	execution, err := budget.BeginReservedBackupExecution()
	if err != nil {
		t.Fatal(err)
	}
	if execution.pathComponentLimit != 7 || execution.entryLimit != 3 || execution.byteLimit != 9 {
		t.Fatalf(
			"backup execution limits = path:%d entries:%d bytes:%d, want 7/3/9",
			execution.pathComponentLimit,
			execution.entryLimit,
			execution.byteLimit,
		)
	}
	if err := execution.AdmitPathComponents(7); err != nil {
		t.Fatalf("consume exact backup path capacity: %v", err)
	}
	if err := execution.AdmitTree(fileWork); err != nil {
		t.Fatalf("consume file backup work: %v", err)
	}
	if err := execution.AdmitTree(directoryWork); err != nil {
		t.Fatalf("consume directory backup work: %v", err)
	}
	if execution.RemainingEntries() != 1 || execution.RemainingBytes() != 1 {
		t.Fatalf(
			"backup probe capacity = entries:%d bytes:%d, want 1/1",
			execution.RemainingEntries(),
			execution.RemainingBytes(),
		)
	}
	if _, err := budget.BeginReservedBackupExecution(); err == nil {
		t.Fatal("backup execution capacity transferred twice")
	}
	if err := budget.ReserveBackupPathComponents(1); err == nil {
		t.Fatal("backup path capacity reserved after transfer")
	}
}

func TestPhysicalWorkBudgetReservesEmptyBackupProbeCapacity(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	zero, err := NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveBackupFileExecution(zero); err != nil {
		t.Fatal(err)
	}
	execution, err := budget.BeginReservedBackupExecution()
	if err != nil {
		t.Fatal(err)
	}
	if execution.entryLimit != 0 || execution.byteLimit != 1 {
		t.Fatalf(
			"empty backup execution limits = entries:%d bytes:%d, want 0/1",
			execution.entryLimit,
			execution.byteLimit,
		)
	}
	if err := execution.AdmitTree(zero); err != nil {
		t.Fatalf("admit empty backup semantic work: %v", err)
	}
}

func TestPhysicalWorkBudgetRejectsForwardPathReservationAtomically(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	remaining := budget.pathComponentLimit - 3
	for remaining > 0 {
		charge := min(remaining, MaximumPhysicalPathDepth)
		if err := budget.AdmitPathComponents(charge); err != nil {
			t.Fatalf("consume path capacity: %v", err)
		}
		remaining -= charge
	}
	before := *budget
	if err := budget.ReserveForwardExecutionPathWork([]int{1}); err == nil {
		t.Fatal("forward reservation exceeded remaining aggregate path capacity")
	}
	if *budget != before {
		t.Fatal("failed forward reservation partially consumed operation capacity")
	}
}

func TestPhysicalWorkBudgetReservesObservationCapacityForEmptyEntry(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	if err := budget.ReserveExecutionObservations(1, 1, 1, mutationfs.RootedAbsencePathObservationCount); err != nil {
		t.Fatalf("reserve execution observations: %v", err)
	}
	empty, err := NewArtifactWork(0, 0)
	if err != nil {
		t.Fatalf("construct empty removal work: %v", err)
	}
	if err := budget.ReserveReobservation(empty); err != nil {
		t.Fatalf("reserve empty reobservation: %v", err)
	}
	execution, err := budget.BeginReservedExecution()
	if err != nil {
		t.Fatalf("begin reserved execution: %v", err)
	}
	if execution.RemainingEntries() != 0 || execution.RemainingBytes() != 0 {
		t.Fatalf(
			"empty-entry semantic work = entries:%d bytes:%d, want 0/0",
			execution.RemainingEntries(),
			execution.RemainingBytes(),
		)
	}
}

func TestPhysicalWorkBudgetChargesIndeterminateObservationMaximum(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	maximum, err := NewArtifactWork(7, 11)
	if err != nil {
		t.Fatalf("construct indeterminate observation maximum: %v", err)
	}
	if err := budget.AdmitIndeterminateTreeWork(maximum, maximum); err != nil {
		t.Fatalf("charge indeterminate observation: %v", err)
	}
	if budget.RemainingEntries() != MaximumPhysicalEntries-7 ||
		budget.RemainingBytes() != MaximumPhysicalBytes-11 {
		t.Fatalf(
			"remaining work = entries:%d bytes:%d, want entries:%d bytes:%d",
			budget.RemainingEntries(),
			budget.RemainingBytes(),
			MaximumPhysicalEntries-7,
			MaximumPhysicalBytes-11,
		)
	}
}

func TestPhysicalWorkBudgetChargesDirectoryOverflowProbeAsEntryWork(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	consumed, err := NewArtifactWork(MaximumPhysicalEntries-2, 0)
	if err != nil {
		t.Fatalf("construct consumed work: %v", err)
	}
	if err := budget.AdmitTree(consumed); err != nil {
		t.Fatalf("consume operation entry capacity: %v", err)
	}
	maximum, err := NewArtifactWork(1, 1)
	if err != nil {
		t.Fatalf("construct directory semantic maximum: %v", err)
	}
	readerCapacity, err := NewArtifactWork(2, 1)
	if err != nil {
		t.Fatalf("construct directory reader capacity: %v", err)
	}
	if err := budget.AdmitIndeterminateDirectoryWork(maximum, readerCapacity); err != nil {
		t.Fatalf("charge N+1 directory overflow evidence: %v", err)
	}
	if budget.RemainingEntries() != 0 {
		t.Fatalf("remaining entries = %d, want 0 after N+1 probe", budget.RemainingEntries())
	}
}

func TestPhysicalWorkBudgetAccumulatesEachDirectoryOverflowEntry(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(4)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	maximum, err := NewArtifactWork(MaximumArtifactTreeEntries, 0)
	if err != nil {
		t.Fatalf("construct directory semantic maximum: %v", err)
	}
	readerCapacity, err := NewArtifactWork(MaximumArtifactTreeEntries+1, 0)
	if err != nil {
		t.Fatalf("construct directory reader capacity: %v", err)
	}
	for index := range 3 {
		if err := budget.AdmitIndeterminateDirectoryWork(maximum, readerCapacity); err != nil {
			t.Fatalf("charge directory overflow %d: %v", index, err)
		}
	}
	wantRemaining := MaximumPhysicalEntries - 3*(MaximumArtifactTreeEntries+1)
	if budget.RemainingEntries() != wantRemaining {
		t.Fatalf(
			"remaining entries = %d, want %d after three N+1 probes",
			budget.RemainingEntries(),
			wantRemaining,
		)
	}
	if err := budget.AdmitIndeterminateDirectoryWork(maximum, readerCapacity); err == nil {
		t.Fatal("aggregate budget admitted a fourth N+1 directory overflow")
	}
}

func TestPhysicalWorkBudgetSeparatelyAccountsForEmptyProofFailure(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	empty, err := NewArtifactWork(0, 0)
	if err != nil {
		t.Fatalf("construct empty removal work: %v", err)
	}
	if err := budget.ReserveReobservation(empty); err != nil {
		t.Fatalf("reserve empty reobservation: %v", err)
	}
	execution, err := budget.BeginReservedExecution()
	if err != nil {
		t.Fatalf("begin reserved execution: %v", err)
	}
	probe, err := NewArtifactWork(0, 1)
	if err != nil {
		t.Fatalf("construct empty-proof probe: %v", err)
	}
	if err := execution.AdmitIndeterminateTreeWork(empty, probe); err != nil {
		t.Fatalf("charge reserved empty-proof failure: %v", err)
	}
	if execution.RemainingEntries() != 0 || execution.RemainingBytes() != 0 {
		t.Fatalf(
			"empty-proof failure changed semantic work = entries:%d bytes:%d",
			execution.RemainingEntries(),
			execution.RemainingBytes(),
		)
	}
	if err := execution.AdmitIndeterminateTreeWork(empty, probe); err == nil {
		t.Fatal("empty-proof failure exceeded separately reserved probe capacity")
	}
}

func TestPhysicalWorkBudgetDerivesObservationCapacityFromIntentCount(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	for index := 0; index < removalObservationsPerIntent; index++ {
		if err := budget.AdmitObservation(); err != nil {
			t.Fatalf("admit observation %d: %v", index, err)
		}
	}
	if err := budget.AdmitObservation(); err == nil {
		t.Fatal("one-intent budget admitted an unowned observation")
	}
	maximum, err := NewPhysicalWorkBudget(MaximumRemovalIntents)
	if err != nil {
		t.Fatalf("construct maximum removal work budget: %v", err)
	}
	if maximum.observationLimit != 90_112 {
		t.Fatalf(
			"maximum physical observations = %d, want documented complete-lifecycle ceiling 90112",
			maximum.observationLimit,
		)
	}
}

func TestPhysicalWorkBudgetPartitionsGeneralAndCleanupExecution(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveCleanupLifecycle(
		4,
		5,
		5,
		mutationfs.RootedAbsencePathObservationCount,
	); err != nil {
		t.Fatalf("reserve cleanup lifecycle: %v", err)
	}
	if err := budget.ReserveGeneralPathComponents(1); err != nil {
		t.Fatalf("reserve general host path: %v", err)
	}
	emptyScratch, err := NewArtifactWork(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.ReserveScratchCleanup(emptyScratch); err != nil {
		t.Fatal(err)
	}
	cleanup, err := budget.BeginReservedCleanupLifecycle()
	if err != nil {
		t.Fatalf("begin cleanup lifecycle: %v", err)
	}
	if _, _, err := budget.BeginReservedScratchCleanup(); err != nil {
		t.Fatal(err)
	}
	if err := budget.ConcludeRetirementNotApplicable(); err != nil {
		t.Fatal(err)
	}
	host, control, err := budget.BeginGeneralExecution()
	if err != nil {
		t.Fatalf("begin general execution: %v", err)
	}
	if err := host.AdmitObservation(); err == nil {
		t.Fatal("general execution admitted cleanup observation work")
	}
	if err := host.AdmitTree(ArtifactWork{entries: 1}); err == nil {
		t.Fatal("general execution admitted cleanup tree work")
	}
	if err := host.AdmitPathComponents(1); err != nil {
		t.Fatalf("host execution rejected reserved path capacity: %v", err)
	}
	if err := control.AdmitPathComponents(1); err != nil {
		t.Fatalf("control execution rejected remaining path capacity: %v", err)
	}
	if err := cleanup.AdmitObservation(); err != nil {
		t.Fatalf("cleanup lifecycle rejected reserved observation: %v", err)
	}
	if err := cleanup.AdmitTree(ArtifactWork{entries: 1}); err != nil {
		t.Fatalf("cleanup lifecycle rejected reserved tree capacity: %v", err)
	}
}

func TestPhysicalWorkBudgetRejectsIntentCardinalityBeforeWork(t *testing.T) {
	if _, err := NewPhysicalWorkBudget(MaximumRemovalIntents + 1); err == nil {
		t.Fatal("removal work budget accepted excessive intent cardinality")
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

func TestPhysicalWorkBudgetBoundsPathDepthAndFixedAggregateComponents(t *testing.T) {
	budget, err := NewPhysicalWorkBudget(1)
	if err != nil {
		t.Fatalf("construct removal work budget: %v", err)
	}
	if err := budget.AdmitPathComponents(MaximumPhysicalPathDepth + 1); err == nil {
		t.Fatal("removal work budget accepted excessive path depth")
	}

	fullDepthAdmissions := maximumPhysicalPathComponentVisits / MaximumPhysicalPathDepth
	for index := 0; index < fullDepthAdmissions; index++ {
		if err := budget.AdmitPathComponents(MaximumPhysicalPathDepth); err != nil {
			t.Fatalf("admit path within aggregate budget at %d: %v", index, err)
		}
	}
	if err := budget.AdmitPathComponents(1); err == nil {
		t.Fatal("removal work budget accepted aggregate path-component overflow")
	}
}
