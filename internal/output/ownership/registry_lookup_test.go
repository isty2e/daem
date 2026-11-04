package ownership

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
)

func TestRegistryIndexedConflictsKeepCanonicalPairwiseResults(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Caf\u00e9")
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	var claims []Claim
	for _, name := range []string{"a", "a-other", "b/child"} {
		for _, key := range []string{"/alpha", "/beta"} {
			claim, err := NewActiveClaim(mustAddress(t, filepath.Join(root, name), key), owner)
			if err != nil {
				t.Fatal(err)
			}
			claims = append(claims, claim)
		}
	}
	registry, err := NewRegistry(claims)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".", "a", "a/child", "a-other", "b", "b/child", "b/child/nested", "c"} {
		path := filepath.Join(root, name)
		for _, key := range []string{"", "/alpha", "/alpha/nested", "/alpha-other", "/beta", "/foreign"} {
			address := mustAddress(t, path, key)
			var want Claim
			found := false
			for _, claim := range registry.Claims() {
				if claim.Address().Overlaps(address) {
					want, found = claim, true
					break
				}
			}
			actual, present := registry.Conflict(address)
			if present != found || found && !actual.Equal(want) {
				t.Fatalf("indexed conflict changed first canonical result at %q %q", path, key)
			}
		}
		namespace := filepath.Dir(root)
		candidate, err := pathauthority.NewProvisional(path, pathtest.DarwinCaseSensitive(path).Witness(), namespace, pathtest.DarwinCaseSensitive(namespace).Witness())
		if err != nil {
			t.Fatal(err)
		}
		var want Claim
		found := false
		for _, claim := range registry.Claims() {
			if candidate.CandidateWithin(claim.Address().PathAuthority()) {
				want, found = claim, true
				break
			}
		}
		actual, present := registry.ProvisionalAncestorConflict(candidate)
		if present != found || found && !actual.Equal(want) {
			t.Fatalf("indexed candidate conflict changed conservative first result at %q", path)
		}
	}
	if _, found := registry.Conflict(ManagedAddress{}); found {
		t.Fatal("invalid address gained a conflict")
	}
	if _, found := registry.ProvisionalAncestorConflict(pathauthority.Provisional{}); found {
		t.Fatal("invalid provisional candidate gained a conflict")
	}
}
