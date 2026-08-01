package mutation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestAcceptVisibilityDomainsRejectsCollapseOfDistinctProvisionalPaths(t *testing.T) {
	namespace := mustMutationTestCanonicalPath(t.TempDir())
	domains := []Domain{
		provisionalMutationTestDomain(t, namespace, "Caf\u00e9"),
		provisionalMutationTestDomain(t, namespace, "Cafe\u0301"),
	}
	collapsed := filepath.Join(namespace.keyPath, "Caf\u00e9")
	observed := []canonicalPath{
		{keyPath: collapsed, accessPath: collapsed, witness: namespace.witness + "s"},
		{keyPath: collapsed, accessPath: collapsed, witness: namespace.witness + "s"},
	}
	if _, accepted, err := acceptVisibilityDomains(domains, observed); err != nil || accepted {
		t.Fatalf("acceptVisibilityDomains() = %t, %v; want collapsed rejection", accepted, err)
	}
}

func TestAcceptVisibilityDomainsAllowsDistinctSiblingAnchorAdvance(t *testing.T) {
	namespace := mustMutationTestCanonicalPath(t.TempDir())
	domains := []Domain{
		provisionalMutationTestDomain(t, namespace, filepath.Join("Future", "Caf\u00e9")),
		provisionalMutationTestDomain(t, namespace, filepath.Join("Future", "Tea\u0301")),
	}
	advancedNamespace := canonicalPath{
		keyPath:    filepath.Join(namespace.keyPath, "Future"),
		accessPath: filepath.Join(namespace.accessPath, "Future"),
		witness:    namespace.witness + "s",
	}
	secondCandidate := canonicalPath{
		keyPath:    filepath.Join(advancedNamespace.keyPath, "Tea\u0301"),
		accessPath: filepath.Join(advancedNamespace.accessPath, "Tea\u0301"),
		witness:    advancedNamespace.witness + "s",
	}
	secondProvisional, err := newProvisionalPathIntent(secondCandidate, advancedNamespace)
	if err != nil {
		t.Fatal(err)
	}
	secondCandidate.provisional = secondProvisional
	observed := []canonicalPath{
		{
			keyPath:    filepath.Join(advancedNamespace.keyPath, "Caf\u00e9"),
			accessPath: filepath.Join(advancedNamespace.accessPath, "Caf\u00e9"),
			witness:    advancedNamespace.witness + "s",
		},
		secondCandidate,
	}
	accepted, ok, err := acceptVisibilityDomains(domains, observed)
	if err != nil || !ok {
		t.Fatalf("acceptVisibilityDomains() = %t, %v", ok, err)
	}
	if !accepted[0].provisional.IsZero() {
		t.Fatal("visible first sibling remained provisional")
	}
	if !accepted[1].provisional.Equal(secondProvisional) {
		t.Fatalf("second sibling provisional = %#v, want %#v", accepted[1].provisional, secondProvisional)
	}
	if accepted[1].namespaceLease.key != namespace.keyPath {
		t.Fatalf("held namespace advanced to %q", accepted[1].namespaceLease.key)
	}
}

func TestLeaseSetAcceptsProvisionalToExactVisibilityChange(t *testing.T) {
	namespacePath := t.TempDir()
	namespace := mustMutationTestCanonicalPath(namespacePath)
	domain := provisionalMutationTestDomain(t, namespace, "Caf\u00e9")
	store := mutationTestStore(t)
	set, err := store.Acquire(context.Background(), domain)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Release()

	if err := os.WriteFile(domain.requestedPath, []byte("visible"), 0o600); err != nil {
		t.Fatal(err)
	}
	if matches, err := set.DomainsMatchCurrent(context.Background()); err != nil || matches {
		t.Fatalf("strict DomainsMatchCurrent() = %t, %v; want false", matches, err)
	}
	if matches, err := set.VisibilityAuthorityMatchesCurrent(context.Background()); err != nil || !matches {
		t.Fatalf("VisibilityAuthorityMatchesCurrent() = %t, %v; want true", matches, err)
	}
	if accepted, err := set.AcceptVisibilityChanges(context.Background()); err != nil || !accepted {
		t.Fatalf("AcceptVisibilityChanges() = %t, %v; want true", accepted, err)
	}
	if matches, err := set.DomainsMatchCurrent(context.Background()); err != nil || !matches {
		t.Fatalf("rebound DomainsMatchCurrent() = %t, %v; want true", matches, err)
	}
}

func TestAcceptVisibilityDomainsRejectsChangeWithoutExclusiveNamespaceLease(t *testing.T) {
	namespace := mustMutationTestCanonicalPath(t.TempDir())
	domain := provisionalMutationTestDomain(t, namespace, "Caf\u00e9")
	domain.access = AccessShared
	visible := canonicalPath{
		keyPath: domain.canonicalPath, accessPath: domain.requestedPath, witness: domain.pathWitness,
	}
	if _, accepted, err := acceptVisibilityDomains(
		[]Domain{domain},
		[]canonicalPath{visible},
	); err != nil || accepted {
		t.Fatalf("acceptVisibilityDomains() = %t, %v; want shared-domain rejection", accepted, err)
	}
}

func provisionalMutationTestDomain(t *testing.T, namespace canonicalPath, relative string) Domain {
	t.Helper()
	depth := len(strings.Split(filepath.Clean(relative), string(filepath.Separator)))
	candidate := canonicalPath{
		keyPath:    filepath.Join(namespace.keyPath, relative),
		accessPath: filepath.Join(namespace.accessPath, relative),
		witness:    namespace.witness + pathSemanticsWitness(strings.Repeat("s", depth)),
	}
	provisional, err := newProvisionalPathIntent(candidate, namespace)
	if err != nil {
		t.Fatal(err)
	}
	namespaceLease, err := newNamespaceLeaseIntent(namespace)
	if err != nil {
		t.Fatal(err)
	}
	return Domain{
		kind: domainLogicalPath, access: AccessExclusive,
		canonicalPath: candidate.keyPath, pathWitness: candidate.witness,
		initialCanonicalPath: candidate.keyPath, initialPathWitness: candidate.witness,
		initialProvisional: provisional, provisional: provisional,
		namespaceLease: namespaceLease, requestedPath: candidate.accessPath,
		effect: PathEffectDirectoryEntry,
	}
}
