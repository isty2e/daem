//go:build darwin || linux

package ownership

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

func TestLegacyNativeNamespaceConflictIsNotExactOwnership(t *testing.T) {
	root := t.TempDir()
	owner := mustAuthority(t, filepath.Join(root, "owner.json"), filepath.Join(root, "owner.toml"))
	foreign := mustAuthority(t, filepath.Join(root, "foreign.json"), filepath.Join(root, "foreign.toml"))
	legacy, err := pathauthority.NewExact("/root/i/doc", "darwin-case-v1:iii")
	if err != nil {
		t.Fatal(err)
	}
	native, err := pathauthority.NewExact("/root/İ/doc", "darwin-case-v2:iui")
	if err != nil {
		t.Fatal(err)
	}
	left, right := mustAddress(t, legacy.Key(), ""), mustAddress(t, native.Key(), "")
	left.path, right.path = legacy, native
	if left.Overlaps(right) || left.Equal(right) || !left.MayOverlap(right) {
		t.Fatal("legacy uncertainty was either discarded or promoted into proven identity")
	}
	for _, pair := range [][2]ManagedAddress{{left, right}, {right, left}} {
		claim, err := NewActiveClaim(pair[0], owner)
		if err != nil {
			t.Fatal(err)
		}
		registry, err := NewRegistry([]Claim{claim})
		if err != nil {
			t.Fatal(err)
		}
		if _, present := registry.Exact(pair[1]); present {
			t.Fatal("possible legacy conflict supplied exact ownership")
		}
		if conflict, present := registry.Conflict(pair[1]); !present || !conflict.Equal(claim) {
			t.Fatal("foreign legacy/native collision was invisible")
		}
		err = registry.RequireWriteExclusion(foreign.Key(), []ManagedAddress{pair[1]}, nil, nil, nil)
		var conflict *ForeignClaimError
		if !errors.As(err, &conflict) {
			t.Fatal("possible legacy/native collision did not exclude foreign writes", err)
		}
	}
}

func TestMissingNativeSpellingKeepsLegacyAncestorExclusion(t *testing.T) {
	root := t.TempDir()
	owner := mustAuthority(t, filepath.Join(root, "state.json"), filepath.Join(root, "manifest.toml"))
	ancestor, err := pathauthority.NewExact("/root/café", "darwin-case-v1:ii")
	if err != nil {
		t.Fatal(err)
	}
	address := mustAddress(t, ancestor.Key(), "")
	address.path = ancestor
	claim, err := NewActiveClaim(address, owner)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry([]Claim{claim})
	if err != nil {
		t.Fatal(err)
	}
	missing, err := pathauthority.NewProvisional("/root/Café/child", "darwin-case-v2:iui", "/root", "darwin-case-v1:i")
	if err != nil {
		t.Fatal(err)
	}
	if !missing.CandidateWithin(ancestor) {
		t.Fatal("uncertain native spelling bypassed its legacy ancestor")
	}
	if actual, found := registry.ProvisionalAncestorConflict(missing); !found || !actual.Equal(claim) {
		t.Fatal("missing native entry bypassed its legacy ancestor index")
	}
}
