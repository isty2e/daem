package publication

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func native17FixtureClaims(t *testing.T, owner stateauthority.Authority, namespace journalnamespace.DocumentNamespace, snapshot durable.Snapshot) []ownership.Claim {
	t.Helper()
	exact, known := namespace.Observation().Exact()
	if !known {
		t.Fatal("claim fixture requires its actual exact namespace")
	}
	index, err := journaldocument.IndexSnapshotForClaims(t.Context(), snapshot, journaldocument.NativeClaims)
	if err != nil {
		t.Fatal(err)
	}
	footprints, err := index.Footprints(t.Context(), namespace.Addresses())
	if err != nil {
		t.Fatal(err)
	}
	var claims []ownership.Claim
	for footprint := range footprints {
		claims = append(claims, documentActiveClaim(t, documentClaimFixture{owner: owner, exact: exact}, footprint))
	}
	return claims
}

func native17FixtureRoots(t *testing.T, roots journalnamespace.Roots, budget *physicalwork.Budget) (output.RootReference, []journalrecord.RootCapture) {
	t.Helper()
	qualified, err := roots.Qualify(t.Context(), budget)
	if err != nil {
		t.Fatal(err)
	}
	manifest, present := qualified.Manifest()
	if !present {
		t.Fatal("capture fixture has no qualified manifest")
	}
	references := []output.RootReference{manifest.RootReference()}
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		reference, err := configuration.RootReferenceForScope(selected, target.ScopeGlobal)
		if err != nil {
			t.Fatal(err)
		}
		references = append(references, reference)
	}
	var result []journalrecord.RootCapture
	for _, reference := range references {
		grant, present := qualified.Selection(reference)
		if !present {
			continue
		}
		alternative, _ := grant.PointAlternative()
		captured, err := journalrecord.NewRootCapture(reference, grant.Provenance(), alternative)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, captured)
	}
	return manifest.RootReference(), result
}
