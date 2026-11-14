package pathreferencecodec

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
)

func TestReferenceRecordPreservesIdentityAndIndependentLocator(t *testing.T) {
	root := t.TempDir()
	identity := pathtest.Exact(filepath.Join(root, "comparison.json"))
	locator := filepath.Join(root, "Selected.json")
	reference, err := pathauthority.NewReference(identity, locator)
	if err != nil {
		t.Fatal(err)
	}
	record, err := Encode(reference)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]string
	if err := json.Unmarshal(content, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 || fields["key"] != identity.Key() || fields["semantics_witness"] != identity.Witness() || fields["locator"] != locator {
		t.Fatal("serialized reference conflated or dropped its facts", fields)
	}
	var loaded Record
	if err := json.Unmarshal(content, &loaded); err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(loaded)
	if err != nil || !decoded.Equal(reference) {
		t.Fatal("reference did not survive metadata roundtrip", err)
	}
}

func TestReferenceRecordRejectsMissingOrInvalidFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "point.json")
	for _, record := range []Record{
		{},
		{Key: path, SemanticsWitness: "exact-v1:"},
		{Key: path, SemanticsWitness: "exact-v1:", Locator: "relative.json"},
		{Key: "relative.json", SemanticsWitness: "exact-v1:", Locator: path},
		{Key: path, SemanticsWitness: "unversioned", Locator: path},
		{Key: path, SemanticsWitness: "exact-v1:", Locator: path + "\x00"},
	} {
		if _, err := Decode(record); err == nil {
			t.Fatal("incomplete or malformed reference was admitted", record)
		}
	}
	if record, err := Encode(pathauthority.Reference{}); err == nil || record != (Record{}) {
		t.Fatal("missing reference produced metadata", record, err)
	}
}
