package hostroute

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/target"
)

func TestCarrierHandoffsSelectIntrinsicDependenciesNotUnrelatedDeletion(t *testing.T) {
	for _, test := range []struct {
		name   string
		before map[string]string
		after  map[string]string
		want   int
	}{
		{"swap", map[string]string{"a": "x@market", "b": "y@market", "z": "z@market"}, map[string]string{"a": "y@market", "b": "x@market"}, 2},
		{"mixed deletion", map[string]string{"a": "x@market", "b": "y@market", "z": "z@market"}, map[string]string{"a": "y@market"}, 2},
		{"retry after first retirement", map[string]string{"b": "y@market", "z": "z@market"}, map[string]string{"a": "y@market"}, 1},
		{"unpaired new acquisition", map[string]string{"a": "x@market", "z": "z@market"}, map[string]string{"new": "x@market"}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var installed []string
			for _, source := range test.before {
				installed = append(installed, source)
			}
			input := carrierHandoffInput(t, test.before, test.after, installed)
			handoffs, err := reconcile.NewCarrierHandoffs(input)
			if err != nil {
				t.Fatal(err)
			}
			if len(handoffs.Retirements()) != test.want || len(handoffs.Acquisitions()) != len(test.after) {
				t.Fatalf("retirements/acquisitions = %d/%d", len(handoffs.Retirements()), len(handoffs.Acquisitions()))
			}
			remaining := handoffs.ProspectiveClaims()
			if len(remaining) != 1 || remaining[0].Identity().RelationSubject().Key() != "z" {
				t.Fatal("retirement set included an unrelated deletion")
			}
			remainingPending := handoffs.ProspectivePendingRemovals()
			if len(remainingPending) != 1 || remainingPending[0].Claim().Identity().RelationSubject().Key() != "z" {
				t.Fatal("prospective facts did not subtract exactly the selected pending removals")
			}
			for _, acquisition := range handoffs.Acquisitions() {
				action := acquisition.Action()
				key, err := relationobserve.NewCorrelationKey(action.Subject(), action.ExpectedRelation())
				if err != nil {
					t.Fatal(err)
				}
				actual, _ := input.Observations.Correlation(key)
				prospective, _ := handoffs.ProspectiveObservations().Correlation(key)
				if actual.State() != relationobserve.StateExactCorrelation || prospective.State() != relationobserve.StateMissing {
					t.Fatalf("actual/prospective state = %s/%s", actual.State(), prospective.State())
				}
				if len(acquisition.RetirementKeys()) == 0 || !action.BlocksOrdinaryApply() {
					t.Fatal("handoff relabeled current unclaimed presence as executable absence")
				}
			}
		})
	}
}

func TestCarrierHandoffAcceptsAlreadyAbsentOldStateWithoutInventingRows(t *testing.T) {
	input := carrierHandoffInput(t, map[string]string{"a": "old@market"}, map[string]string{"a": "new@market"}, nil)
	handoffs, err := reconcile.NewCarrierHandoffs(input)
	if err != nil {
		t.Fatal(err)
	}
	if handoffs.Empty() || len(handoffs.ProspectiveClaims()) != 0 ||
		handoffs.Retirements()[0].Decision() != carrierabsence.DecisionRetireAlreadyAbsent {
		t.Fatal("already-absent old carrier did not retain its state-only handoff")
	}
	for _, action := range input.Acquisitions {
		key, err := relationobserve.NewCorrelationKey(action.Subject(), action.ExpectedRelation())
		if err != nil {
			t.Fatal(err)
		}
		actual, _ := input.Observations.Correlation(key)
		prospective, _ := handoffs.ProspectiveObservations().Correlation(key)
		if actual.State() != relationobserve.StateMissing || prospective.State() != relationobserve.StateMissing || len(actual.CoveredOccurrences()) != 0 {
			t.Fatal("already-absent handoff invented current rows")
		}
	}
}

func TestCarrierHandoffCannotBypassSharedRemovalOrMissingCurrentFacts(t *testing.T) {
	shared := carrierHandoffInput(t, map[string]string{"a": "old@market", "b": "old@market"}, map[string]string{"a": "new@market"}, []string{"old@market"})
	handoffs, err := reconcile.NewCarrierHandoffs(shared)
	if err != nil {
		t.Fatal(err)
	}
	if !handoffs.Empty() || len(handoffs.ProspectiveClaims()) != 2 {
		t.Fatal("handoff bypassed existing shared-consumer removal policy")
	}
	input := carrierHandoffInput(t, map[string]string{"a": "old@market"}, map[string]string{"a": "new@market"}, []string{"old@market"})
	missingClaim := input
	missingClaim.Claims = nil
	missingObservation := input
	missingObservation.Observations = relationobserve.Batch{}
	for _, invalid := range []reconcile.CarrierHandoffsInput{missingClaim, missingObservation} {
		if _, err := reconcile.NewCarrierHandoffs(invalid); err == nil {
			t.Fatal("retirement was not bound to exact current facts and observations")
		}
	}
}

func TestCarrierSourceChangePreservesDeclarationFamilyAndScope(t *testing.T) {
	before, _ := statusClaudePluginExtensionLockfileWithScope(t, "a", "old@market", target.ScopeGlobal)
	claim := retainedCarrierFixtureFor(t, before).claim
	for _, test := range []struct {
		id     string
		source string
		scope  target.Scope
		want   bool
	}{
		{"a", "new@market", target.ScopeGlobal, true},
		{"a", "old@market", target.ScopeGlobal, false},
		{"a", "new@market", target.ScopeProject, false},
		{"different", "new@market", target.ScopeGlobal, false},
	} {
		after, _ := statusClaudePluginExtensionLockfileWithScope(t, test.id, test.source, test.scope)
		identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(after.Locked.Subjects()[0])
		if err != nil || !admitted {
			t.Fatalf("after identity: %t %v", admitted, err)
		}
		if claim.Identity().SourceChangeTo(identity) != test.want {
			t.Fatalf("source change admitted the wrong declaration/locality: %+v", test)
		}
	}
	otherFamily, _ := statusCodexPluginExtensionLockfile(t, "a", "new@market")
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(otherFamily.Locked.Subjects()[0])
	if err != nil || !admitted || claim.Identity().SourceChangeTo(identity) {
		t.Fatal("source compatibility crossed host/carrier families", err)
	}
}

func TestCarrierHandoffExpectationPreservesCurrentTruthAndProspectiveRole(t *testing.T) {
	input := carrierHandoffInput(t, map[string]string{"a": "old@market"}, map[string]string{"a": "new@market"}, []string{"old@market"})
	handoffs, err := reconcile.NewCarrierHandoffs(input)
	if err != nil {
		t.Fatal(err)
	}
	locked, _ := statusClaudePluginExtensionLockfileWithScope(t, "a", "new@market", target.ScopeGlobal)
	actions, err := BuildRelationActions(RelationInput{
		Locked: locked, SelectedTargets: statusClaudeSelection(t, "claude-code"), CurrentOwner: input.CurrentOwner,
		ManagedClaims: handoffs.ProspectiveClaims(), PendingRemovals: handoffs.ProspectivePendingRemovals(),
		Observations: handoffs.ProspectiveObservations(),
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextApply, Relations: actions})
	if err != nil {
		t.Fatal(err)
	}
	expectation, err := reconcile.NewCarrierHandoffExpectation(handoffs, after)
	if err != nil {
		t.Fatal(err)
	}
	current, err := reconcile.NewResult(reconcile.ResultInput{
		Context: reconcile.ContextApply, Relations: input.Acquisitions, CarrierAbsences: input.Absences,
		CarrierHandoff: &expectation,
	})
	if err != nil {
		t.Fatal(err)
	}
	expectation = reconcile.CarrierHandoffExpectation{}
	retained, present := current.Clone().CarrierHandoff()
	if !present || retained.Handoffs().Empty() || current.IsProspective() || after.IsProspective() {
		t.Fatal("binding or caller assignment erased current/conditional separation")
	}
	prospective := retained.After()
	rewritten, err := prospective.WithRelationOrders(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !prospective.IsProspective() || !prospective.Clone().IsProspective() || !rewritten.IsProspective() {
		t.Fatal("copying or replacing order decisions qualified a prospective remainder")
	}
	for _, nested := range []reconcile.Result{current, prospective} {
		if _, err := reconcile.NewCarrierHandoffExpectation(handoffs, nested); err == nil {
			t.Fatal("nested/reused expectations bypassed fresh assessment")
		}
	}
	missing, err := reconcile.NewResult(reconcile.ResultInput{Context: reconcile.ContextApply})
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := reconcile.NewResult(reconcile.ResultInput{
		Context: reconcile.ContextApply, Relations: actions, CarrierAbsences: input.Absences,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []reconcile.Result{missing, repeated} {
		if _, err := reconcile.NewCarrierHandoffExpectation(handoffs, invalid); err == nil {
			t.Fatal("expectation omitted its new contract or repeated an old retirement")
		}
	}
}

func carrierHandoffInput(t *testing.T, before, after map[string]string, installed []string) reconcile.CarrierHandoffsInput {
	t.Helper()
	beforeFiles := make(map[string]lock.File, len(before))
	var names []string
	for name, source := range before {
		file, _ := statusClaudePluginExtensionLockfileWithScope(t, name, source, target.ScopeGlobal)
		beforeFiles[name] = file
		names = append(names, name)
	}
	sort.Strings(names)
	owner := retainedCarrierFixtureFor(t, beforeFiles[names[0]]).claim.Owner()
	input := reconcile.CarrierHandoffsInput{CurrentOwner: owner}
	contracts := make(map[relationobserve.CorrelationKey]lock.LockedSubjectContract)
	for _, name := range names {
		contract := beforeFiles[name].Locked.Subjects()[0]
		identity, _, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contract)
		if err != nil {
			t.Fatal(err)
		}
		request, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
		if err != nil {
			t.Fatal(err)
		}
		claim, err := durablecarrier.NewManagedCarrierClaim(owner, identity, request, durablecarrier.ClaimProvenanceInstalledObserved)
		if err != nil {
			t.Fatal(err)
		}
		input.Claims = append(input.Claims, claim)
		for _, source := range installed {
			if source == before[name] {
				input.PendingRemovals = append(input.PendingRemovals, pendingCarrierRemovalFor(t, claim))
				break
			}
		}
		key, err := relationobserve.NewCorrelationKey(identity.RelationSubject(), identity.ExpectedRelation())
		if err != nil {
			t.Fatal(err)
		}
		contracts[key] = contract
	}
	var afterFiles []lock.File
	for name, source := range after {
		file, relation := statusClaudePluginExtensionLockfileWithScope(t, name, source, target.ScopeGlobal)
		key, err := relationobserve.NewCorrelationKey(file.Locked.Subjects()[0].SubjectID(), relation.ExpectedRelation())
		if err != nil {
			t.Fatal(err)
		}
		contracts[key] = file.Locked.Subjects()[0]
		afterFiles = append(afterFiles, file)
	}
	var scoped []observeclaudeplugin.ScopedRelation
	for key := range contracts {
		relation, err := observeclaudeplugin.NewScopedRelation(key, target.ScopeGlobal)
		if err != nil {
			t.Fatal(err)
		}
		scoped = append(scoped, relation)
	}
	configRoot := t.TempDir()
	type installedRecord struct {
		Scope string `json:"scope"`
	}
	plugins := make(map[string][]installedRecord)
	for _, source := range installed {
		plugins[source] = []installedRecord{{Scope: "user"}}
	}
	data, err := json.Marshal(struct {
		Version int                          `json:"version"`
		Plugins map[string][]installedRecord `json:"plugins"`
	}{Version: 2, Plugins: plugins})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(configRoot, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configRoot, "plugins", "installed_plugins.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, err := observeclaudeplugin.ReadInstalledInventory(observeclaudeplugin.InstalledInventoryInput{ConfigRoot: configRoot, Relations: scoped})
	if err != nil {
		t.Fatal(err)
	}
	var correlations []relationobserve.Correlation
	for _, relation := range scoped {
		correlations = append(correlations, relationobserve.Correlation{Key: relation.CorrelationKey(), Result: observeclaudeplugin.Correlate(relation, inventory)})
	}
	input.Observations, err = relationobserve.NewBatch(relationobserve.BatchSpec{Correlations: correlations})
	if err != nil {
		t.Fatal(err)
	}
	selection := statusClaudeSelection(t, "claude-code")
	input.Absences, err = BuildCarrierAbsenceActions(CarrierAbsenceInput{
		Locked: lock.File{Version: lock.CurrentVersion}, SelectedTargets: selection,
		CurrentOwner: owner, AllClaims: input.Claims, Observations: input.Observations,
		PendingRemovals: input.PendingRemovals,
		ResolveRoute: func(durablecarrier.ManagedCarrierClaim) (carrierabsence.RouteAdmission, error) {
			return admittedCarrierRemovalRoute(t, false), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range afterFiles {
		actions, err := BuildRelationActions(RelationInput{Locked: file, SelectedTargets: selection, Observations: input.Observations, CurrentOwner: owner, ManagedClaims: input.Claims})
		if err != nil {
			t.Fatal(err)
		}
		input.Acquisitions = append(input.Acquisitions, actions...)
	}
	return input
}
