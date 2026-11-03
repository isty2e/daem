package projection

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestNativeDocumentWideningReusesExactExplicitRecordOnlyAdoption(t *testing.T) {
	id, err := entity.New(entity.KindHostConfig, "settings")
	if err != nil {
		t.Fatal(err)
	}
	placement, err := aggregate.HostConfigPlacementID(target.TargetPi, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(id, placement)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("@pi-agent/extension-settings/test.json")
	if err != nil {
		t.Fatal(err)
	}
	current, err := observe.NewManagedPathEvidence(subject, destination, true,
		artifact.HashFileContent([]byte(`{"model":"fast","released":9}`)), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, desired string
		approval      bool
		want          reconcile.ManagedPathDecisionKind
	}{
		{name: "no approval", desired: `{"model":"fast","released":9}`, want: reconcile.ManagedPathBlocked},
		{name: "exact approved", desired: `{"model":"fast","released":9}`, approval: true, want: reconcile.ManagedPathRecord},
		{name: "equal values different bytes", desired: `{ "released":9, "model":"fast" }`, approval: true, want: reconcile.ManagedPathBlocked},
		{name: "different content", desired: `{"model":"fast"}`, approval: true, want: reconcile.ManagedPathBlocked},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts := reconcile.ManagedPathDecisionInput{
				Subject: subject, ConsumerTargets: []target.Target{target.TargetPi}, Scope: target.ScopeGlobal, Destination: destination,
				DesiredHash: artifact.HashFileContent([]byte(test.desired)), ContentKind: realization.PathProjectionFile,
				PlacementMode: realization.PathProjectionCopy, PermissionPolicy: realization.PathPermissionsExact, DesiredFileMode: 0o600,
			}
			decision := reconcileManagedPathDesired(facts, current, durable.ManagedPathState{}, false, nil, test.approval)
			if decision.Kind() != test.want {
				t.Fatalf("whole-file adoption = %s, want %s", decision.Kind(), test.want)
			}
		})
	}
}
