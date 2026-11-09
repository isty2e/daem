package record

import (
	"testing"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestOrdinaryProvisionalEncodingRefusesProjectAcquisition(t *testing.T) {
	fixture := documentClaimsForTest(t)
	for _, spelling := range []string{"@pi-agent/settings.json", ".pi/settings.json"} {
		destination := outputtest.Parse(t, spelling)
		intent, err := ownership.NewProvisionalAcquireIntent(destination, documentKeyFootprint(t, "model"), fixture.provisional, fixture.owner, "first-document")
		if err != nil {
			t.Fatal(err)
		}
		records, err := recoveryProvisionalAcquireIntents([]ownership.ProvisionalAcquireIntent{intent})
		if spelling == ".pi/settings.json" {
			if err == nil || len(records) != 0 {
				t.Fatal("ordinary encoding admitted a project acquisition or partial record")
			}
		} else if err != nil || len(records) != 1 {
			t.Fatal("ordinary encoding refused its global control", err)
		}
	}
}

func TestDocumentProvisionalAliasAdmissionRetainsTheRecordedClaimPolicy(t *testing.T) {
	fixture := documentClaimsForTest(t)
	project, err := aggregate.NewDocumentAddress(target.TargetPi, target.ScopeProject, outputtest.Parse(t, ".pi/settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	intent, err := ownership.NewProvisionalAcquireIntent(project.AggregateRoot(), documentKeyFootprint(t, "model"), fixture.provisional, fixture.owner, "first-document")
	if err != nil {
		t.Fatal(err)
	}
	inputs := []journaldocument.ProvisionalAcquire{journaldocument.NewProvisionalAcquire(project, intent)}
	record, err := persistedDocumentProvisionalClaims(inputs, journaldocument.NativeClaims)
	if err != nil {
		t.Fatal(err)
	}
	record = documentClaimRoundtrip(t, record)
	aliases := []aggregate.DocumentAddress{fixture.alias, project}
	for _, selection := range []journaldocument.ClaimSelection{journaldocument.LegacyGlobalClaims, journaldocument.NativeClaims, 0} {
		acquires, err := record.canonicalProvisional(aliases, fixture.owner, "first-document", fixture.provisional, selection)
		if selection != journaldocument.NativeClaims {
			if err == nil || len(acquires) != 0 {
				t.Fatal("legacy or invalid claim policy admitted a project acquisition")
			}
			if encoded, err := persistedDocumentProvisionalClaims(inputs, selection); err == nil || encoded.Kind != "" {
				t.Fatal("legacy or invalid claim policy encoded a project acquisition")
			}
			continue
		}
		if err != nil || len(acquires) != 1 || acquires[0].Alias() != project || !acquires[0].Intent().Equal(intent) {
			t.Fatal("native17 wire lost project alias or acquisition evidence", err)
		}
		promoted, err := record.promoted(aliases, fixture.owner, "first-document", fixture.provisional, fixture.exact, selection)
		if err != nil {
			t.Fatal(err)
		}
		claims, err := promoted.canonicalExact(fixture.owner, "first-document", fixture.exact)
		if err != nil || len(claims.After()) != 1 || !claims.After()[0].Address().Footprint().Equal(intent.Footprint()) || !claims.After()[0].Address().PathAuthority().Equal(fixture.exact) {
			t.Fatal("project promotion changed exact namespace or selected footprint", err)
		}
	}
}
