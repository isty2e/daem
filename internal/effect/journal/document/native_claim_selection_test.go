package document

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestNativeClaimSelectionIncludesWholeNativeAndGlobalOrdinaryPaths(t *testing.T) {
	id, err := entity.New(entity.KindInstructions, "ordinary")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(id, "native-claim-selection")
	if err != nil {
		t.Fatal(err)
	}
	ordinary := func(scope target.Scope, path string) durable.ManagedPathState {
		row, err := durable.NewManagedPathState(subject, []target.Target{target.TargetCodex}, scope, outputtest.Parse(t, path), testContentHash("ordinary"), realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	for _, test := range []struct {
		name           string
		row            durable.ManagedPathState
		legacy, native bool
	}{
		{"project native whole", nativeWholeFileJournalBaseline(t, target.TargetCodex, target.ScopeProject), false, true},
		{"global native whole", nativeWholeFileJournalBaseline(t, target.TargetCodex, target.ScopeGlobal), true, true},
		{"global ordinary", ordinary(target.ScopeGlobal, "~/.config/custom.json"), false, true},
		{"project ordinary", ordinary(target.ScopeProject, ".config/custom.json"), false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: []durable.ManagedPathState{test.row}})
			if err != nil {
				t.Fatal(err)
			}
			alias, err := aggregate.NewDocumentAddress(target.TargetCodex, test.row.Scope(), test.row.Destination())
			if err != nil {
				t.Fatal(err)
			}
			for _, selection := range []struct {
				policy   ClaimSelection
				selected bool
			}{{LegacyGlobalClaims, test.legacy}, {NativeClaims, test.native}} {
				index, err := IndexSnapshotForClaims(t.Context(), state, selection.policy)
				if err != nil {
					t.Fatal(err)
				}
				footprints, err := index.Footprints(t.Context(), []aggregate.DocumentAddress{alias})
				_, whole := footprints[output.WholePathFootprint()]
				if err != nil || whole != selection.selected || len(footprints) > 1 {
					t.Fatal("path membership does not follow its recorded claim-selection rule", selection.policy, footprints, err)
				}
			}
		})
	}
}
