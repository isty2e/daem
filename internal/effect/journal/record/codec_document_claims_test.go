package record

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func documentClaimRoundtrip(t *testing.T, record recoveryDocumentClaimSet) recoveryDocumentClaimSet {
	t.Helper()
	content, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var decoded recoveryDocumentClaimSet
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestDocumentClaimSetReconstructsBothOwnershipModesAndReshaping(t *testing.T) {
	fixture := documentClaimsForTest(t)
	whole := []ownership.Claim{documentActiveClaim(t, fixture, output.WholePathFootprint())}
	keys := []ownership.Claim{
		documentActiveClaim(t, fixture, documentKeyFootprint(t, "")),
		documentActiveClaim(t, fixture, documentKeyFootprint(t, "a/b")),
	}
	parent := []ownership.Claim{documentActiveClaim(t, fixture, documentKeyFootprint(t, "settings"))}
	children := []ownership.Claim{documentActiveClaim(t, fixture, documentKeyFootprint(t, "settings", "model"))}
	for _, test := range []struct {
		name          string
		before, after []ownership.Claim
	}{
		{"narrow", whole, keys},
		{"widen", keys, whole},
		{"parent to children", parent, children},
		{"children to parent", children, parent},
		{"first acquire", nil, keys},
		{"last release", keys, nil},
		{"retain", keys, keys},
	} {
		t.Run(test.name, func(t *testing.T) {
			expected, err := ownershipmutation.NewDocumentClaimTransition(test.before, test.after, "document-change")
			if err != nil {
				t.Fatal(err)
			}
			record, err := persistedDocumentClaimTransition(expected, "document-change")
			if err != nil {
				t.Fatal(err)
			}
			actual, err := documentClaimRoundtrip(t, record).canonicalExact(fixture.owner, "document-change", fixture.exact)
			if err != nil {
				t.Fatal(err)
			}
			for _, phase := range [][2][]ownership.Claim{
				{expected.Before(), actual.Before()}, {expected.Prepared(), actual.Prepared()}, {expected.After(), actual.After()},
			} {
				if !slices.EqualFunc(phase[0], phase[1], ownership.Claim.Equal) {
					t.Fatal("claim wire lost an actual phase or changed prepared exclusion")
				}
			}
			for _, claim := range append(actual.Before(), actual.After()...) {
				covered := false
				for _, prepared := range actual.Prepared() {
					covered = covered || prepared.Address().Footprint().Contains(claim.Address().Footprint())
				}
				if !covered {
					t.Fatal("claim reshaping left an exclusion gap")
				}
			}
		})
	}
}

func TestDocumentProvisionalClaimsKeepTypedKeysAndPromoteWithoutEntries(t *testing.T) {
	fixture := documentClaimsForTest(t)
	var inputs []journaldocument.ProvisionalAcquire
	for _, parts := range [][]string{{""}, {"a/b"}, {"a", "b"}, {"line\n\x00key"}, {"~"}} {
		intent, err := ownership.NewProvisionalAcquireIntent(fixture.alias.AggregateRoot(), documentKeyFootprint(t, parts...), fixture.provisional, fixture.owner, "first-document")
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, journaldocument.NewProvisionalAcquire(fixture.alias, intent))
	}
	inputSnapshot := slices.Clone(inputs)
	record, err := persistedDocumentProvisionalClaims(inputs, journaldocument.LegacyGlobalClaims)
	if err != nil {
		t.Fatal(err)
	}
	decoded := documentClaimRoundtrip(t, record)
	actual, err := decoded.canonicalProvisional([]aggregate.DocumentAddress{fixture.alias}, fixture.owner, "first-document", fixture.provisional, journaldocument.LegacyGlobalClaims)
	if err != nil || len(actual) != len(inputs) {
		t.Fatalf("typed provisional evidence failed: %v", err)
	}
	for _, expected := range inputs {
		if !slices.ContainsFunc(actual, func(candidate journaldocument.ProvisionalAcquire) bool {
			return candidate.Alias() == expected.Alias() && candidate.Intent().Equal(expected.Intent())
		}) {
			t.Fatal("provisional wire interpreted a native key as a legacy content path")
		}
	}

	shuffled := slices.Clone(inputs)
	slices.Reverse(shuffled)
	reordered, err := persistedDocumentProvisionalClaims(shuffled, journaldocument.LegacyGlobalClaims)
	if err != nil {
		t.Fatal(err)
	}
	left, _ := json.Marshal(record)
	right, _ := json.Marshal(reordered)
	if !bytes.Equal(left, right) || !slices.EqualFunc(inputs, inputSnapshot, func(left, right journaldocument.ProvisionalAcquire) bool {
		return left.Alias() == right.Alias() && left.Intent().Equal(right.Intent())
	}) {
		t.Fatal("persisted identity depends on order or writer mutated its input")
	}
	promoted, err := decoded.promoted([]aggregate.DocumentAddress{fixture.alias}, fixture.owner, "first-document", fixture.provisional, fixture.exact, journaldocument.LegacyGlobalClaims)
	if err != nil {
		t.Fatal(err)
	}
	transition, err := documentClaimRoundtrip(t, promoted).canonicalExact(fixture.owner, "first-document", fixture.exact)
	if err != nil || len(transition.Before()) != 0 || len(transition.After()) != len(inputs) {
		t.Fatalf("group claim promotion required a legacy entry or lost acquires: %v", err)
	}
	for _, claim := range transition.Prepared() {
		if claim.State() != ownership.ClaimReserved || claim.OperationID() != "first-document" {
			t.Fatal("promoted claim did not derive an operation-scoped reservation")
		}
	}
}

func TestDocumentClaimEvidenceRefusesWrongBindingAndOverlaps(t *testing.T) {
	fixture := documentClaimsForTest(t)
	intent, err := ownership.NewProvisionalAcquireIntent(fixture.alias.AggregateRoot(), documentKeyFootprint(t, "parent"), fixture.provisional, fixture.owner, "first-document")
	if err != nil {
		t.Fatal(err)
	}
	record, err := persistedDocumentProvisionalClaims([]journaldocument.ProvisionalAcquire{journaldocument.NewProvisionalAcquire(fixture.alias, intent)}, journaldocument.LegacyGlobalClaims)
	if err != nil {
		t.Fatal(err)
	}
	other := documentClaimsForTest(t)
	for _, test := range []struct {
		name   string
		modify func(*recoveryDocumentClaimSet)
	}{
		{"wrong target", func(r *recoveryDocumentClaimSet) { (*r.Acquires)[0].Alias.Target = "codex" }},
		{"wrong scope", func(r *recoveryDocumentClaimSet) { (*r.Acquires)[0].Alias.Scope = "project" }},
		{"wrong destination", func(r *recoveryDocumentClaimSet) { (*r.Acquires)[0].Alias.Destination = "@pi-agent/other.json" }},
		{"wrong owner", func(r *recoveryDocumentClaimSet) {
			(*r.Acquires)[0].StatefileAuthority = *persistedPathAuthority(other.owner.StatefileAuthority())
		}},
		{"wrong operation", func(r *recoveryDocumentClaimSet) { (*r.Acquires)[0].OperationID = "different" }},
		{"wrong namespace", func(r *recoveryDocumentClaimSet) {
			(*r.Acquires)[0].NamespaceAuthority = *persistedPathAuthority(other.provisional.Namespace())
		}},
		{"duplicate acquire", func(r *recoveryDocumentClaimSet) { *r.Acquires = append(*r.Acquires, (*r.Acquires)[0]) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalid := documentClaimRoundtrip(t, record)
			test.modify(&invalid)
			if claims, err := invalid.canonicalProvisional([]aggregate.DocumentAddress{fixture.alias}, fixture.owner, "first-document", fixture.provisional, journaldocument.LegacyGlobalClaims); err == nil || len(claims) != 0 {
				t.Fatal("mismatched evidence returned usable provisional acquires")
			}
		})
	}
	if candidate, err := record.promoted([]aggregate.DocumentAddress{fixture.alias}, fixture.owner, "first-document", fixture.provisional, other.exact, journaldocument.LegacyGlobalClaims); err == nil || candidate.Kind != "" {
		t.Fatal("promotion admitted a different physical document")
	}
	child, err := ownership.NewProvisionalAcquireIntent(fixture.alias.AggregateRoot(), documentKeyFootprint(t, "parent", "child"), fixture.provisional, fixture.owner, "first-document")
	if err != nil {
		t.Fatal(err)
	}
	if candidate, err := persistedDocumentProvisionalClaims([]journaldocument.ProvisionalAcquire{journaldocument.NewProvisionalAcquire(fixture.alias, intent), journaldocument.NewProvisionalAcquire(fixture.alias, child)}, journaldocument.LegacyGlobalClaims); err == nil || candidate.Kind != "" {
		t.Fatal("overlapping native acquires returned a usable record")
	}
}

func TestDocumentClaimWireRejectsInvalidClosedVariants(t *testing.T) {
	for _, content := range []string{
		`{}`, `null`, `{"kind":null}`, `{"kind":"other"}`,
		`{"kind":"absent","before":[]}`, `{"kind":"exact","before":[],"after":[]}`,
		`{"kind":"exact","before":null,"after":[]}`, `{"kind":"provisional","acquires":[]}`,
		`{"kind":"provisional","acquires":[{}]}`, `{"kind":"absent","prepared":[]}`,
		`{"kind":"absent","kind":"exact"}`, `{"kind":"absent","Kind":"exact"}`,
	} {
		var record recoveryDocumentClaimSet
		if err := json.Unmarshal([]byte(content), &record); err == nil || record.Kind != "" {
			t.Fatalf("invalid closed document-claim variant decoded: %s", content)
		}
	}
}

func TestDocumentExactClaimsBindOwnerPathAndOperation(t *testing.T) {
	fixture := documentClaimsForTest(t)
	other := documentClaimsForTest(t)
	claim := documentActiveClaim(t, fixture, documentKeyFootprint(t, "a/b"))
	transition, err := ownershipmutation.NewDocumentClaimTransition(nil, []ownership.Claim{claim}, "acquire-document")
	if err != nil {
		t.Fatal(err)
	}
	if record, err := persistedDocumentClaimTransition(transition, "different-operation"); err == nil || record.Kind != "" {
		t.Fatal("writer relabeled the actual prepared reservation")
	}
	record, err := persistedDocumentClaimTransition(transition, "acquire-document")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		modify func(*recoveryDocumentClaimSet)
	}{
		{"wrong owner", func(r *recoveryDocumentClaimSet) {
			(*r.After)[0].StatefileAuthority = persistedPathAuthority(other.owner.StatefileAuthority())
		}},
		{"wrong document", func(r *recoveryDocumentClaimSet) { (*r.After)[0].PathAuthority = persistedPathAuthority(other.exact) }},
		{"non-active phase", func(r *recoveryDocumentClaimSet) {
			(*r.After)[0].State = string(ownership.ClaimReserved)
			(*r.After)[0].OperationID = "acquire-document"
		}},
		{"absent claim", func(r *recoveryDocumentClaimSet) { (*r.After)[0] = recoveryClaimValue{} }},
		{"overlapping phase", func(r *recoveryDocumentClaimSet) { *r.After = append(*r.After, (*r.After)[0]) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalid := documentClaimRoundtrip(t, record)
			test.modify(&invalid)
			candidate, err := invalid.canonicalExact(fixture.owner, "acquire-document", fixture.exact)
			if err == nil || len(candidate.Before()) != 0 || len(candidate.After()) != 0 {
				t.Fatal("invalid active phase returned a usable document transition")
			}
		})
	}
	if _, err := record.canonicalExact(fixture.owner, "", fixture.exact); err == nil {
		t.Fatal("empty operation was accepted for exact document claims")
	}
}
