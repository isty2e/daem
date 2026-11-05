package record

import (
	"encoding/json"
	"testing"

	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestNativeBindingRejectsNestedAuthorityFieldAliases(t *testing.T) {
	record := bindingWireTestRecord(t, t.TempDir())
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range [][]string{{"statefile_authority"}, {"after", "authority"}} {
		content := nativeEvidenceObjectChange(t, encoded, path, func(fields map[string]json.RawMessage) {
			fields["Key"] = fields["key"]
		})
		if _, err := decodeBindingIntentWireForTest(content); err == nil {
			t.Errorf("nested authority alias was admitted at %v", path)
		}
	}
}

func TestNativeClaimsRejectNestedArrayRecordAliasesAndNulls(t *testing.T) {
	fixture := documentClaimsForTest(t)
	claims := []ownership.Claim{documentActiveClaim(t, fixture, output.WholePathFootprint())}
	transition, err := ownershipmutation.NewDocumentClaimTransition(claims, claims, "nested-record")
	if err != nil {
		t.Fatal(err)
	}
	record, err := persistedDocumentClaimTransition(transition, "nested-record")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		path []string
		edit func(map[string]json.RawMessage)
	}{
		{"authority alias", []string{"path_authority"}, func(fields map[string]json.RawMessage) { fields["Key"] = fields["key"] }},
		{"footprint alias", []string{"footprint"}, func(fields map[string]json.RawMessage) { fields["Kind"] = fields["kind"] }},
		{"whole footprint null key", []string{"footprint"}, func(fields map[string]json.RawMessage) { fields["key_path"] = json.RawMessage("null") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var outer map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &outer); err != nil {
				t.Fatal(err)
			}
			var rows []json.RawMessage
			if err := json.Unmarshal(outer["before"], &rows); err != nil {
				t.Fatal(err)
			}
			rows[0] = nativeEvidenceObjectChange(t, rows[0], test.path, test.edit)
			outer["before"], err = json.Marshal(rows)
			if err != nil {
				t.Fatal(err)
			}
			content, err := json.Marshal(outer)
			if err != nil {
				t.Fatal(err)
			}
			var actual recoveryDocumentClaimSet
			if err := json.Unmarshal(content, &actual); err == nil {
				if _, canonicalErr := actual.canonicalExact(fixture.owner, "nested-record", fixture.exact); canonicalErr == nil {
					t.Fatal("malformed nested evidence also reconstructed a complete claim transition")
				}
				t.Fatal("malformed nested claim record was admitted")
			}
		})
	}
}

func TestNativeEvidenceDoesNotInterpretKeysInsideStringValues(t *testing.T) {
	fixture := documentClaimsForTest(t)
	claim := documentActiveClaim(t, fixture, documentKeyFootprint(t, "CamelCase", "a/b", "日本語"))
	transition, err := ownershipmutation.NewDocumentClaimTransition(nil, []ownership.Claim{claim}, "literal-key")
	if err != nil {
		t.Fatal(err)
	}
	record, err := persistedDocumentClaimTransition(transition, "literal-key")
	if err != nil {
		t.Fatal(err)
	}
	actual, err := documentClaimRoundtrip(t, record).canonicalExact(fixture.owner, "literal-key", fixture.exact)
	if err != nil || len(actual.After()) != 1 || !actual.After()[0].Equal(claim) {
		t.Fatalf("canonical field admission changed a literal native key: %v", err)
	}
}

func nativeEvidenceObjectChange(t *testing.T, content []byte, path []string, edit func(map[string]json.RawMessage)) []byte {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		t.Fatal(err)
	}
	if len(path) == 0 {
		edit(fields)
	} else {
		child, present := fields[path[0]]
		if !present {
			t.Fatalf("fixture object path is missing: %v", path)
		}
		fields[path[0]] = nativeEvidenceObjectChange(t, child, path[1:], edit)
	}
	result, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
