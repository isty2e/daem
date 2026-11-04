package observe

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/test/outputtest"
)

func TestOwnershipObservationPreservesNativeScopeAndAuthorityKind(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "Café.json")
	exact := pathtest.DarwinCaseSensitive(physical)
	provisional, err := pathauthority.NewProvisional(physical, exact.Witness(), root, pathtest.DarwinCaseSensitive(root).Witness())
	if err != nil {
		t.Fatal(err)
	}
	destination := outputtest.Parse(t, "@pi-agent/settings.json")
	scopes := []output.Footprint{
		output.WholePathFootprint(),
		outputtest.KeyFootprint(t, ""),
		outputtest.KeyFootprint(t, "a/b"),
		outputtest.KeyFootprint(t, "a", "b"),
		outputtest.KeyFootprint(t, "~1"),
		outputtest.KeyFootprint(t, ".."),
		outputtest.KeyFootprint(t, "line\n\x00key"),
	}
	seen := make(map[output.Footprint]struct{})
	var exactRows, provisionalRows []OwnershipObservation
	for _, scope := range scopes {
		address, err := ownership.NewManagedAddress(exact, scope)
		if err != nil {
			t.Fatal(err)
		}
		exactRow, err := NewExactOwnershipObservation(destination, scope, address, ownership.NoClaim())
		if err != nil {
			t.Fatal(err)
		}
		futureRow, err := NewProvisionalOwnershipObservation(destination, scope, provisional, ownership.NoClaim())
		if err != nil {
			t.Fatal(err)
		}
		if !exactRow.Footprint().Equal(scope) || !futureRow.Footprint().Equal(scope) {
			t.Fatal("ownership observation changed the selected native scope")
		}
		if exactRow.Overlaps(futureRow) || futureRow.Overlaps(exactRow) {
			t.Fatal("provisional evidence was treated as exact authority")
		}
		seen[exactRow.Footprint()] = struct{}{}
		exactRows = append(exactRows, exactRow)
		provisionalRows = append(provisionalRows, futureRow)
	}
	if len(seen) != len(scopes) {
		t.Fatal("distinct native scopes collapsed")
	}
	for _, rows := range [][]OwnershipObservation{exactRows, provisionalRows} {
		if rows[2].Overlaps(rows[3]) || rows[3].Overlaps(rows[2]) {
			t.Fatal("literal slash in a key became native ancestry")
		}
		for _, row := range rows {
			if !rows[0].Overlaps(row) || !row.Overlaps(rows[0]) || !row.Overlaps(row) {
				t.Fatal("whole-path or reflexive scope overlap was lost")
			}
		}
	}
	wholeAddress, _ := exactRows[0].ExactAddress()
	keyedAddress, _ := exactRows[3].ExactAddress()
	if _, err := exactRows[1].LegacyContentPath(); !errors.Is(err, output.ErrNotLegacyFootprint) {
		t.Fatalf("empty property was projected as legacy authority: %v", err)
	}
	if _, err := NewExactOwnershipObservation(destination, output.Footprint{}, wholeAddress, ownership.NoClaim()); !errors.Is(err, output.ErrInvalidFootprint) {
		t.Fatalf("zero footprint admission = %v", err)
	}
	if _, err := NewExactOwnershipObservation(destination, scopes[2], keyedAddress, ownership.NoClaim()); err == nil {
		t.Fatal("observation admitted an exact address for a different scope")
	}
}
