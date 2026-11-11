package pathauthority_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
)

func TestReferenceKeepsAccessSpellingSeparateFromIdentity(t *testing.T) {
	root := t.TempDir()
	identity := pathtest.Exact(filepath.Join(root, "canonical.json"))
	locator := filepath.Join(root, "selected.json")
	first, err := pathauthority.NewReference(identity, locator)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := pathauthority.NewReference(identity, locator)
	if err != nil {
		t.Fatal(err)
	}
	other, err := pathauthority.NewReference(identity, filepath.Join(root, "other.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Identity().Equal(identity) || first.Locator() != locator || !first.Equal(copy) {
		t.Fatal("recorded reference lost its independent facts")
	}
	if first.Equal(other) || !first.Identity().Equal(other.Identity()) {
		t.Fatal("reference equality and comparison identity were conflated")
	}
	if reflect.TypeFor[pathauthority.Reference]().Comparable() {
		t.Fatal("reference permits implicit equality or use as an identity map key")
	}
	if !reflect.TypeFor[pathauthority.Exact]().Comparable() {
		t.Fatal("pure path identity stopped being comparable")
	}
}

func TestReferenceRejectsPartialOrMalformedEvidence(t *testing.T) {
	root := t.TempDir()
	identity := pathtest.Exact(filepath.Join(root, "canonical.json"))
	for _, locator := range []string{"", "relative.json", root + "/child/../point.json", root + "/bad\x00", root + "/" + string([]byte{0xff})} {
		if _, err := pathauthority.NewReference(identity, locator); err == nil {
			t.Fatalf("invalid locator accepted: %q", locator)
		}
	}
	if _, err := pathauthority.NewReference(pathauthority.Exact{}, filepath.Join(root, "selected.json")); err == nil {
		t.Fatal("locator was admitted without comparison identity")
	}
	var zero pathauthority.Reference
	if !zero.IsZero() || zero.Validate() == nil || zero.Locator() != "" {
		t.Fatal("missing evidence was reconstructed or validated")
	}
}
