package record

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestNativeMissingHistoryDoesNotUseNewPromotionEnvelopeForStoredRecords(t *testing.T) {
	fixture := documentClaimsForTest(t)
	missing := fixture.provisional
	observed := recoveryDocumentObservation{
		Kind: "provisional", Authority: &pathAuthorityDTO{Key: missing.CandidateKey(), Witness: missing.CandidateWitness()},
		NamespaceAuthority: persistedPathAuthority(missing.Namespace()),
	}
	other := pathtest.DarwinCaseSensitive(filepath.Join(filepath.Dir(fixture.exact.Key()), "Other\u00e9.json"))
	if err := missing.AdmitsExact(other); err != nil {
		t.Fatalf("test requires a different exact key inside the new-publication envelope: %v", err)
	}
	for _, path := range []pathauthority.Exact{fixture.exact, other, pathtest.Exact(fixture.exact.Key())} {
		valid := path.Equal(fixture.exact)
		claim := documentActiveClaim(t, documentClaimFixture{owner: fixture.owner, exact: path}, documentKeyFootprint(t, "selected"))
		transition, err := ownershipmutation.NewDocumentClaimTransition([]ownership.Claim{claim}, nil, "retire")
		if err != nil {
			t.Fatal(err)
		}
		record, err := persistedDocumentClaimTransition(transition, "retire")
		if err != nil {
			t.Fatal(err)
		}
		rebuilt, err := record.canonicalObservedExact(fixture.owner, "retire", observed)
		if valid && (err != nil || len(rebuilt.Before()) != 1 || !rebuilt.Before()[0].Equal(claim)) {
			t.Fatalf("matching stored claim lost historical identity: %v", err)
		}
		if !valid && err == nil {
			t.Fatal("stored claim acquired a different key/witness through the promotion envelope")
		}

		binding, err := ownership.NewDocumentBinding(fixture.owner, fixture.alias.AggregateRoot(), path)
		if err != nil {
			t.Fatal(err)
		}
		value, err := ownership.PresentDocumentBinding(binding)
		if err != nil {
			t.Fatal(err)
		}
		intent, err := ownership.NewExactDocumentBindingIntent(fixture.owner, binding.Destination(), value, value)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := persistedDocumentBindingIntent(intent)
		if err != nil {
			t.Fatal(err)
		}
		group := recoveryDocumentGroup{
			CapturedTarget: observed,
			Aliases:        []recoveryDocumentAlias{{Address: persistedDocumentAlias(fixture.alias), ObservedBefore: observed}},
			BindingIntents: []recoveryDocumentBindingIntent{encoded},
		}
		intents, err := group.canonicalBindingIntents(t.Context(), fixture.owner)
		if valid && (err != nil || len(intents) != 1 || !intents[0].Equal(intent)) {
			t.Fatalf("matching retained binding became unpromoted: %v", err)
		}
		if !valid && (err == nil || intents != nil) {
			t.Fatal("stored binding acquired a different key/witness through the promotion envelope")
		}
	}
}
