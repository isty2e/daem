package projection

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/test/outputtest"
)

func TestOwnershipIndexUsesNativeIdentityAndAncestry(t *testing.T) {
	physical := pathtest.Exact(filepath.Join(t.TempDir(), "settings.json"))
	destination := outputtest.Parse(t, "@pi-agent/settings.json")
	row := func(scope output.Footprint) observe.OwnershipObservation {
		t.Helper()
		address, err := ownership.NewManagedAddress(physical, scope)
		if err != nil {
			t.Fatal(err)
		}
		observation, err := observe.NewExactOwnershipObservation(destination, scope, address, ownership.NoClaim())
		if err != nil {
			t.Fatal(err)
		}
		return observation
	}
	scopes := []output.Footprint{
		outputtest.KeyFootprint(t, ""), outputtest.KeyFootprint(t, "a/b"),
		outputtest.KeyFootprint(t, "a", "b"), outputtest.KeyFootprint(t, "a~1b"),
		outputtest.KeyFootprint(t, "."), outputtest.KeyFootprint(t, "line\n\x00key"),
	}
	rows := make([]observe.OwnershipObservation, 0, len(scopes))
	for _, scope := range scopes {
		rows = append(rows, row(scope))
	}
	indexed, conflicts, err := ownershipObservations(rows)
	if err != nil || len(indexed) != len(scopes) || len(conflicts) != 0 {
		t.Fatalf("native siblings collided or lost identity: %d, %d, %v", len(indexed), len(conflicts), err)
	}
	if _, _, err := ownershipObservations(append(rows, rows[1])); err == nil {
		t.Fatal("duplicate native scope was accepted")
	}
	_, conflicts, err = ownershipObservations([]observe.OwnershipObservation{rows[1], row(outputtest.KeyFootprint(t, "a/b", "child")), rows[2]})
	if err != nil || len(conflicts) != 2 {
		t.Fatalf("native ancestry was flattened or missed: %d, %v", len(conflicts), err)
	}
	_, conflicts, err = ownershipObservations([]observe.OwnershipObservation{row(output.WholePathFootprint()), rows[0]})
	if err != nil || len(conflicts) != 2 {
		t.Fatalf("empty property did not overlap whole-path authority: %d, %v", len(conflicts), err)
	}
}
