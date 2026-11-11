package pathreferencecodec

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
)

func TestReferenceTableCodecKeepsWireIdentitiesUnique(t *testing.T) {
	root := t.TempDir()
	identities := []pathauthority.Exact{pathtest.Exact(filepath.Join(root, "b.json")), pathtest.Exact(filepath.Join(root, "a.json"))}
	var references []pathauthority.Reference
	for _, identity := range identities {
		reference, err := pathauthority.NewReference(identity, filepath.Join(root, "Selected", filepath.Base(identity.Key())))
		if err != nil {
			t.Fatal(err)
		}
		references = append(references, reference)
	}
	table, err := pathauthority.NewReferenceTable(identities, references)
	if err != nil {
		t.Fatal(err)
	}
	records, err := EncodeTable(table)
	if err != nil {
		t.Fatal(err)
	}
	reordered := slices.Clone(records)
	slices.Reverse(reordered)
	loaded, err := DecodeTable(reordered)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := pathauthority.NewReferenceTable(identities, loaded)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := EncodeTable(rebuilt)
	if err != nil || !slices.Equal(canonical, records) {
		t.Fatal("wire order changed canonical evidence", err)
	}
	conflicting := records[0]
	conflicting.Locator = filepath.Join(root, "other.json")
	for _, malformed := range [][]Record{nil, {{}}, {records[0], records[0]}, {records[0], conflicting}} {
		if _, err := DecodeTable(malformed); err == nil {
			t.Fatal("missing, invalid or repeated wire evidence was admitted")
		}
	}
	if records, err := EncodeTable(pathauthority.ReferenceTable{}); err == nil || records != nil {
		t.Fatal("uninitialized table produced wire data")
	}
	empty, err := pathauthority.NewReferenceTable(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeTable(empty)
	if err != nil || encoded == nil || len(encoded) != 0 {
		t.Fatal("empty table lost its explicit array", err)
	}
	if decoded, err := DecodeTable(encoded); err != nil || decoded == nil || len(decoded) != 0 {
		t.Fatal("explicit empty table did not round trip", err)
	}
}
