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

func TestReferenceTableConvergenceRetainsPrunesAndRequiresNewEvidence(t *testing.T) {
	root := t.TempDir()
	var identities []pathauthority.Exact
	var references []pathauthority.Reference
	for _, name := range []string{"old", "retained", "added"} {
		identity := pathtest.Exact(filepath.Join(root, name+".json"))
		reference, err := pathauthority.NewReference(identity, filepath.Join(root, "Selected", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		identities = append(identities, identity)
		references = append(references, reference)
	}
	current, err := pathauthority.NewReferenceTable(identities[:2], references[:2])
	if err != nil {
		t.Fatal(err)
	}
	nextIDs := []pathauthority.Exact{identities[2], identities[1], identities[1]}
	additions := []pathauthority.Reference{references[2]}
	next, err := current.Converge(nextIDs, additions)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := next.Reference(identities[0]); present || len(next.References()) != 2 {
		t.Fatal("convergence retained a departed identity")
	}
	for _, expected := range references[1:] {
		actual, present := next.Reference(expected.Identity())
		if !present || !actual.Equal(expected) {
			t.Fatal("convergence lost retained or added evidence")
		}
	}
	additions[0] = pathauthority.Reference{}
	repeated, err := next.Converge([]pathauthority.Exact{identities[1], identities[2]}, []pathauthority.Reference{references[1]})
	if err != nil || len(repeated.References()) != 2 {
		t.Fatal("same-table convergence was not idempotent", err)
	}
	for _, expected := range references[1:] {
		actual, present := repeated.Reference(expected.Identity())
		if !present || !actual.Equal(expected) {
			t.Fatal("repeat convergence changed recorded evidence")
		}
	}
	if actual, present := current.Reference(identities[0]); !present || !actual.Equal(references[0]) {
		t.Fatal("successor mutated its current table")
	}
	replacement, err := pathauthority.NewReference(identities[1], filepath.Join(root, "Replacement.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		ids       []pathauthority.Exact
		additions []pathauthority.Reference
	}{
		{name: "missing new evidence", ids: identities},
		{name: "unrelated addition", ids: identities[:2], additions: references[2:]},
		{name: "accidental replacement", ids: identities[:2], additions: []pathauthority.Reference{replacement}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := current.Converge(test.ids, test.additions); err == nil {
				t.Fatal("incomplete or replacing successor was admitted")
			}
		})
	}
	empty, err := current.Converge(nil, nil)
	if err != nil || empty.Validate() != nil || len(empty.References()) != 0 {
		t.Fatal("complete retirement failed", err)
	}
	if _, err := (pathauthority.ReferenceTable{}).Converge(nil, nil); err == nil {
		t.Fatal("uninitialized current table was treated as empty")
	}
}
