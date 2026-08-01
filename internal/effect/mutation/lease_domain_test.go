package mutation

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestNormalizeDomainsCoalescesModesAndPreservesSharedDescendant(t *testing.T) {
	root := t.TempDir()
	parent := mutationTestLogicalDomain(t, root, AccessShared)
	child := mutationTestLogicalDomain(t, filepath.Join(root, "child"), AccessExclusive)
	duplicateChild := mutationTestLogicalDomain(t, filepath.Join(root, "child"), AccessShared)
	store := mutationTestStore(t)
	normalized, err := store.normalize([]Domain{child, parent, duplicateChild})
	if err != nil {
		t.Fatal(err)
	}
	assertMutationTestDomainMode(t, normalized, pathKey(parent.canonicalPath), AccessShared)
	assertMutationTestDomainMode(t, normalized, pathKey(child.canonicalPath), AccessExclusive)
}

func TestNormalizeDomainsExclusiveAncestorSubsumesPathButKeepsHostIntent(t *testing.T) {
	root := t.TempDir()
	parent := mutationTestLogicalDomain(t, root, AccessExclusive)
	child, err := NewPhysicalPathDomain(PhysicalPathRequest{
		Path: filepath.Join(root, "child"), Access: AccessExclusive, Effect: PathEffectDirectoryEntry,
		Target: "codex", Scope: "project",
	})
	if err != nil {
		t.Fatal(err)
	}
	store := mutationTestStore(t)
	normalized, err := store.normalize([]Domain{child, parent})
	if err != nil {
		t.Fatal(err)
	}
	assertMutationTestDomainMissing(t, normalized, pathKey(child.canonicalPath))
	assertMutationTestDomainMode(t, normalized, encodedKey("host-target", "codex"), AccessShared)
	assertMutationTestDomainMode(t, normalized, encodedKey("host-scope", "codex", "project"), AccessShared)
}

func TestNormalizeDomainsOrdersMixedKeysAndEscalatesRoutes(t *testing.T) {
	pathDomain := mutationTestLogicalDomain(t, filepath.Join(t.TempDir(), "path"), AccessExclusive)
	complete := mutationTestRouteDomain(t, "codex", "global", "plugin", RouteContainmentCompletePaths)
	scoped := mutationTestRouteDomain(t, "claude-code", "project", "plugin", RouteContainmentScope)
	unknown := mutationTestRouteDomain(t, "opencode", "project", "plugin", RouteContainmentUnknown)
	store := mutationTestStore(t)
	normalized, err := store.normalize([]Domain{unknown, pathDomain, complete, scoped})
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(normalized))
	for _, domain := range normalized {
		keys = append(keys, domain.key)
	}
	if !sort.StringsAreSorted(keys) {
		t.Fatalf("domain keys are not sorted: %q", keys)
	}
	assertMutationTestDomainMode(t, normalized, encodedKey("host-route", "codex", "global", "plugin"), AccessExclusive)
	assertMutationTestDomainMode(t, normalized, encodedKey("host-scope", "claude-code", "project"), AccessExclusive)
	assertMutationTestDomainMode(t, normalized, encodedKey("host-target", "opencode"), AccessExclusive)
}

func TestNormalizeDomainsCoalescesProvisionalAliasesAtExactNamespace(t *testing.T) {
	namespace := filepath.Join(t.TempDir(), "namespace")
	if err := mkdirMutationTest(namespace); err != nil {
		t.Fatal(err)
	}
	namespaceIdentity := mustMutationTestCanonicalPath(namespace)
	namespaceIdentity = darwinProvisionalMutationTestNamespace(namespaceIdentity)
	store := mutationTestStore(t)

	domains := make([]Domain, 0, 2)
	for _, name := range []string{"Caf\u00e9", "Cafe\u0301"} {
		candidate := canonicalPath{
			keyPath:    filepath.Join(namespaceIdentity.keyPath, name),
			accessPath: filepath.Join(namespaceIdentity.accessPath, name),
			witness:    namespaceIdentity.witness + "s",
		}
		provisional, err := newProvisionalPathIntent(candidate, namespaceIdentity)
		if err != nil {
			t.Fatal(err)
		}
		namespaceLease, err := newNamespaceLeaseIntent(namespaceIdentity)
		if err != nil {
			t.Fatal(err)
		}
		domains = append(domains, Domain{
			kind: domainLogicalPath, access: AccessExclusive,
			canonicalPath: candidate.keyPath, pathWitness: candidate.witness,
			initialCanonicalPath: candidate.keyPath, initialPathWitness: candidate.witness,
			provisional: provisional, namespaceLease: namespaceLease,
			requestedPath: candidate.accessPath, effect: PathEffectDirectoryEntry,
		})
	}

	normalized, err := store.normalize(domains)
	if err != nil {
		t.Fatal(err)
	}
	assertMutationTestDomainMode(t, normalized, pathKey(namespaceIdentity.keyPath), AccessExclusive)
	for _, domain := range domains {
		assertMutationTestDomainMissing(t, normalized, pathKey(domain.canonicalPath))
	}
}

func TestProvisionalAliasesContendOnOneNamespaceLease(t *testing.T) {
	namespace := mustMutationTestCanonicalPath(t.TempDir())
	first := provisionalMutationTestDomain(t, namespace, "Caf\u00e9")
	second := provisionalMutationTestDomain(t, namespace, "Cafe\u0301")
	store := mutationTestStore(t)
	store.maximum = 50 * time.Millisecond

	holder, err := store.Acquire(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	if _, err := store.Acquire(context.Background(), second); err == nil {
		t.Fatal("normalization aliases acquired independent namespace leases")
	} else {
		var contention ContentionError
		if !errors.As(err, &contention) {
			t.Fatalf("second acquire error = %v, want ContentionError", err)
		}
	}
}

func TestAdvancedAnchorCannotBypassHeldAncestorNamespaceLease(t *testing.T) {
	root := t.TempDir()
	rootIdentity := mustMutationTestCanonicalPath(root)
	first := provisionalMutationTestDomain(t, rootIdentity, filepath.Join("Future", "Caf\u00e9"))
	store := mutationTestStore(t)
	store.maximum = 50 * time.Millisecond
	holder, err := store.Acquire(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()

	advancedPath := filepath.Join(root, "Future")
	if err := mkdirMutationTest(advancedPath); err != nil {
		t.Fatal(err)
	}
	advancedIdentity := mustMutationTestCanonicalPath(advancedPath)
	second := provisionalMutationTestDomain(t, advancedIdentity, "Tea\u0301")
	if _, err := store.Acquire(context.Background(), second); err == nil {
		t.Fatal("advanced namespace anchor bypassed held ancestor lease")
	} else {
		var contention ContentionError
		if !errors.As(err, &contention) {
			t.Fatalf("advanced acquire error = %v, want ContentionError", err)
		}
	}
}
