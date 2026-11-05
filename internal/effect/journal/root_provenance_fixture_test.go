package journal

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func captureFixtureProvenance(t *testing.T, path string) rootedpath.AuthorityProvenance {
	t.Helper()
	root, err := rootedpath.CaptureRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	authority, err := root.Authority()
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		t.Fatal(err)
	}
	return provenance
}
