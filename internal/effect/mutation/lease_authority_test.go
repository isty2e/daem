package mutation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLeaseSetDetectsCanonicalPathRetargetAfterAcquisition(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, path := range []string{first, second} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}
	domain, err := NewLogicalPathDomain(LogicalPathRequest{
		Path: filepath.Join(alias, "value"), Access: AccessExclusive, Effect: PathEffectReferent,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := mutationTestStore(t)
	set, err := store.Acquire(context.Background(), domain)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Release()
	if matches, err := set.DomainsMatchCurrent(context.Background()); err != nil || !matches {
		t.Fatalf("DomainsMatchCurrent() = %t, %v; want true", matches, err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	if matches, err := set.DomainsMatchCurrent(context.Background()); err != nil || matches {
		t.Fatalf("DomainsMatchCurrent() = %t, %v; want false", matches, err)
	}
}

func TestLeaseSetCoversOnlyExactExclusivePhysicalAuthority(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	domains := mutationTestPhysicalDomains(t, path, "codex", "global")
	store := mutationTestStore(t)
	set, err := store.Acquire(context.Background(), domains...)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Release()

	cases := []struct {
		name      string
		requests  []PhysicalAuthorityRequest
		wantCover bool
	}{
		{
			name: "exact",
			requests: []PhysicalAuthorityRequest{{
				Path: path, Target: "codex", Scope: "global",
			}},
			wantCover: true,
		},
		{
			name: "other path",
			requests: []PhysicalAuthorityRequest{{
				Path: filepath.Join(root, "other.json"), Target: "codex", Scope: "global",
			}},
		},
		{
			name: "other target",
			requests: []PhysicalAuthorityRequest{{
				Path: path, Target: "claude-code", Scope: "global",
			}},
		},
		{
			name: "other scope",
			requests: []PhysicalAuthorityRequest{{
				Path: path, Target: "codex", Scope: "project",
			}},
		},
		{name: "no effect paths", wantCover: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			authority, err := NewPhysicalAuthoritySet(test.requests...)
			if err != nil {
				t.Fatal(err)
			}
			covered, err := set.CoversPhysicalAuthority(authority)
			if err != nil {
				t.Fatal(err)
			}
			if covered != test.wantCover {
				t.Fatalf("CoversPhysicalAuthority() = %t, want %t", covered, test.wantCover)
			}
		})
	}
}

func TestLeaseSetRejectsPhysicalAuthorityAliasABA(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, path := range []string{first, second} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}
	requestedPath := filepath.Join(alias, "config.json")
	domains := mutationTestPhysicalDomains(t, requestedPath, "codex", "global")
	store := mutationTestStore(t)
	set, err := store.Acquire(context.Background(), domains...)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Release()

	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	boundAuthority, err := NewPhysicalAuthoritySet(PhysicalAuthorityRequest{
		Path: requestedPath, Target: "codex", Scope: "global",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}

	if matches, err := set.DomainsMatchCurrent(context.Background()); err != nil || !matches {
		t.Fatalf("DomainsMatchCurrent() = %t, %v; want ABA lexical match", matches, err)
	}
	if covered, err := set.CoversPhysicalAuthority(boundAuthority); err != nil {
		t.Fatal(err)
	} else if covered {
		t.Fatal("CoversPhysicalAuthority accepted an effect bound during the alias ABA interval")
	}
}

func TestLeaseSetDoesNotTreatLogicalLeaseAsPhysicalAuthority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	logical := mutationTestLogicalDomain(t, path, AccessExclusive)
	store := mutationTestStore(t)
	set, err := store.Acquire(context.Background(), logical)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Release()
	authority, err := NewPhysicalAuthoritySet(PhysicalAuthorityRequest{
		Path: path, Target: "codex", Scope: "global",
	})
	if err != nil {
		t.Fatal(err)
	}
	if covered, err := set.CoversPhysicalAuthority(authority); err != nil {
		t.Fatal(err)
	} else if covered {
		t.Fatal("logical lease unexpectedly covered target-visible physical authority")
	}
}
