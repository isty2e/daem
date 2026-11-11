package pathauthority_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
)

func TestReferenceTableOwnsCompleteImmutableEvidence(t *testing.T) {
	root := t.TempDir()
	firstID := pathtest.Exact(filepath.Join(root, "a.json"))
	secondID := pathtest.Exact(filepath.Join(root, "b.json"))
	first, err := pathauthority.NewReference(firstID, filepath.Join(root, "SelectedA.json"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := pathauthority.NewReference(secondID, filepath.Join(root, "SelectedB.json"))
	if err != nil {
		t.Fatal(err)
	}
	identities := []pathauthority.Exact{secondID, firstID, firstID}
	references := []pathauthority.Reference{second, first, first}
	table, err := pathauthority.NewReferenceTable(identities, references)
	if err != nil {
		t.Fatal(err)
	}
	values := table.References()
	if len(values) != 2 || !values[0].Equal(first) || !values[1].Equal(second) {
		t.Fatal("table did not normalize membership and identity order")
	}
	identities[0] = pathauthority.Exact{}
	references[0] = pathauthority.Reference{}
	values[0] = pathauthority.Reference{}
	for _, expected := range []pathauthority.Reference{first, second} {
		actual, present := table.Reference(expected.Identity())
		if !present || !actual.Equal(expected) {
			t.Fatal("caller mutation changed the table")
		}
	}
	conflicting, err := pathauthority.NewReference(firstID, second.Locator())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		identities []pathauthority.Exact
		references []pathauthority.Reference
	}{
		{name: "missing", identities: []pathauthority.Exact{firstID, secondID}, references: []pathauthority.Reference{first}},
		{name: "unused", identities: []pathauthority.Exact{firstID}, references: []pathauthority.Reference{first, second}},
		{name: "conflicting", identities: []pathauthority.Exact{firstID}, references: []pathauthority.Reference{first, conflicting}},
		{name: "invalid identity", identities: []pathauthority.Exact{{}}},
		{name: "invalid reference", identities: []pathauthority.Exact{firstID}, references: []pathauthority.Reference{{}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pathauthority.NewReferenceTable(test.identities, test.references); err == nil {
				t.Fatal("incomplete or contradictory evidence was admitted")
			}
		})
	}
	if (pathauthority.ReferenceTable{}).Validate() == nil {
		t.Fatal("zero table was admitted")
	}
	empty, err := pathauthority.NewReferenceTable(nil, nil)
	if err != nil || empty.Validate() != nil || len(empty.References()) != 0 {
		t.Fatal("explicit empty evidence was rejected", err)
	}
}

func TestReferenceTableSeparatesSemanticsWitnesses(t *testing.T) {
	key := strings.ToLower(filepath.Join(t.TempDir(), "point.json"))
	exact := pathtest.Exact(key)
	folded, err := pathauthority.NewExact(key, "windows-fold-v1:")
	if err != nil {
		t.Fatal(err)
	}
	var references []pathauthority.Reference
	for _, identity := range []pathauthority.Exact{folded, exact} {
		reference, err := pathauthority.NewReference(identity, key)
		if err != nil {
			t.Fatal(err)
		}
		references = append(references, reference)
	}
	table, err := pathauthority.NewReferenceTable([]pathauthority.Exact{exact, folded}, references)
	if err != nil {
		t.Fatal(err)
	}
	values := table.References()
	if len(values) != 2 || !values[0].Identity().Equal(exact) || !values[1].Identity().Equal(folded) {
		t.Fatal("path key collapsed distinct semantics witnesses")
	}
	for _, expected := range references {
		actual, present := table.Reference(expected.Identity())
		if !present || !actual.Equal(expected) {
			t.Fatal("lookup selected the wrong semantics witness")
		}
	}
}
