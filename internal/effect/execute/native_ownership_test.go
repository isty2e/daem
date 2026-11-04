package execute

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/test/outputtest"
)

func TestOwnershipExecutionCarriesNativeScopesIntoAcquisition(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "Café.json")
	exact := pathtest.DarwinCaseSensitive(physical)
	provisional, err := pathauthority.NewProvisional(physical, exact.Witness(), root, pathtest.DarwinCaseSensitive(root).Witness())
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(pathtest.Exact(filepath.Join(root, "state.json")), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	destination := outputtest.Parse(t, "@pi-agent/settings.json")
	var observations []observe.OwnershipObservation
	for _, scope := range []output.Footprint{
		output.WholePathFootprint(), outputtest.KeyFootprint(t, ""),
		outputtest.KeyFootprint(t, "a/b"), outputtest.KeyFootprint(t, "a", "b"),
		outputtest.KeyFootprint(t, "a~1b"), outputtest.KeyFootprint(t, "line\n\x00key"),
	} {
		future, err := observe.NewProvisionalOwnershipObservation(destination, scope, provisional, ownership.NoClaim())
		if err != nil {
			t.Fatal(err)
		}
		plan := ownershipMutationPlan{}
		if err := appendOwnershipAcquire(&plan, future, owner, "operation-1"); err != nil {
			t.Fatal(err)
		}
		if len(plan.intents) != 1 || len(plan.transitions) != 0 || !plan.intents[0].Footprint().Equal(scope) {
			t.Fatal("future native scope changed or gained exact claim authority")
		}
		address, err := ownership.NewManagedAddress(exact, scope)
		if err != nil {
			t.Fatal(err)
		}
		visible, err := observe.NewExactOwnershipObservation(destination, scope, address, ownership.NoClaim())
		if err != nil {
			t.Fatal(err)
		}
		plan = ownershipMutationPlan{}
		if err := appendOwnershipAcquire(&plan, visible, owner, "operation-1"); err != nil {
			t.Fatal(err)
		}
		if len(plan.transitions) != 1 || len(plan.intents) != 0 || !plan.transitions[0].Address().Footprint().Equal(scope) {
			t.Fatal("visible acquisition changed native scope or retained provisional authority")
		}
		observations = append(observations, visible)
	}
	indexed, err := ownershipObservationsByOutput(observations)
	if err != nil || len(indexed) != len(observations) {
		t.Fatalf("execution index conflated native scopes: %d, %v", len(indexed), err)
	}
	if _, err := ownershipObservationsByOutput(append(observations, observations[1])); err == nil {
		t.Fatal("execution admitted duplicate native observation authority")
	}
}
