package apply

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/test/outputtest"
)

func TestOwnershipFingerprintBindsNativeScopeWithoutDisplayParsing(t *testing.T) {
	exact := pathtest.Exact(filepath.Join(t.TempDir(), "settings.json"))
	destination := outputtest.Parse(t, "@pi-agent/settings.json")
	seen := make(map[string]bool)
	var rows []observe.OwnershipObservation
	for _, scope := range []output.Footprint{
		output.WholePathFootprint(), outputtest.KeyFootprint(t, ""),
		outputtest.KeyFootprint(t, "a/b"), outputtest.KeyFootprint(t, "a", "b"),
		outputtest.KeyFootprint(t, "a~1b"), outputtest.KeyFootprint(t, "."),
		outputtest.KeyFootprint(t, "line\n\x00key"),
	} {
		address, err := ownership.NewManagedAddress(exact, scope)
		if err != nil {
			t.Fatal(err)
		}
		row, err := observe.NewExactOwnershipObservation(destination, scope, address, ownership.NoClaim())
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(ownershipFingerprintFacts([]observe.OwnershipObservation{row}))
		if err != nil {
			t.Fatal(err)
		}
		if seen[string(encoded)] {
			t.Fatal("approval fingerprint facts conflate distinct native authority")
		}
		seen[string(encoded)] = true
		rows = append(rows, row)
	}
	before, err := json.Marshal(ownershipFingerprintFacts(rows))
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(rows)
	after, err := json.Marshal(ownershipFingerprintFacts(rows))
	if err != nil || string(before) != string(after) {
		t.Fatalf("observation order changed approval fingerprint facts: %v", err)
	}
}
