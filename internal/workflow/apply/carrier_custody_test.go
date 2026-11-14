package apply

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
)

func TestGlobalCarrierPromotionCandidateUsesClaimCustody(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			current, action := pendingCarrierPromotionForScope(t, scope)
			want := scope == target.ScopeGlobal
			if candidate := isGlobalCarrierPromotionCandidate(current, action); candidate != want {
				t.Fatalf("global promotion candidate = %t, want %t", candidate, want)
			}
		})
	}
}

func TestProjectCarrierCompletionLeavesGlobalPendingUnchanged(t *testing.T) {
	current, action := pendingCarrierPromotionForScope(t, target.ScopeGlobal)
	completion, err := effectpreparation.CompleteProjectCarrierInstalls(
		current,
		[]reconcile.RelationAction{action},
	)
	if err != nil {
		t.Fatal(err)
	}
	if completion.Changed() || !completion.AfterState().Equal(current) || len(completion.Claims()) != 0 {
		t.Fatal("project completion changed global pending acquisition")
	}
}

func pendingCarrierPromotionForScope(t *testing.T, scope target.Scope) (durable.Snapshot, reconcile.RelationAction) {
	t.Helper()
	record, subject := applyClaudePluginCarrierContractWithDeclarationID(t, scope, "context7")
	hostScope := observeclaudeplugin.HostScopeProject
	if scope == target.ScopeGlobal {
		hostScope = observeclaudeplugin.HostScopeUser
	}
	action := applyClaudePluginCarrierActionForSubject(t, record, subject, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			applyClaudePluginCarrierManagedRowWithScope(
				t,
				"context7@market",
				string(subject.ExpectedRelation().ManagedInstanceKey()),
				hostScope,
			),
		},
	})
	if action.Kind() != reconcile.ActionNoOp {
		t.Fatalf("pending completion action = %s, want no-op", action.Kind())
	}
	if _, present := action.Correlation(); !present {
		t.Fatal("pending completion has no correlation")
	}

	root := t.TempDir()
	owner, err := stateauthority.New(pathtest.Exact(filepath.Join(root, "state.json")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, action.CarrierIdentity(), action.RouteRequest())
	if err != nil {
		t.Fatal(err)
	}
	current, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierInstalls: []durablecarrier.PendingCarrierInstall{pending},
	})
	if err != nil {
		t.Fatal(err)
	}
	return current, action
}
