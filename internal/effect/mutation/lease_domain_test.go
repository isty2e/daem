package mutation

import (
	"path/filepath"
	"sort"
	"testing"
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
